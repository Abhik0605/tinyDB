package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"

	"golang.org/x/sys/unix"

	"tinydb-go/internal/index"
)

// Constants
const BTREE_PAGE_SIZE = index.BTREE_PAGE_SIZE
const DB_SIG = "BuildYourOwnDB07" // Updated version for free list support

// KV is the key-value store backed by a B-tree with page recycling
type KV struct {
	Path string // file path
	// internals
	fd     int         // file descriptor
	tree   index.BTree // B-tree index
	free   FreeList    // free list for page recycling
	failed bool        // Did the last update fail?
	mmap   struct {
		total  int      // mmap size, can be larger than the file size
		chunks [][]byte // multiple mmaps, can be non-continuous
	}
	page struct {
		flushed uint64 // database size in number of pages
		nappend int    // number of pages to be appended
		// newly allocated or updated pages keyed by page pointer
		updates map[uint64][]byte
	}
}

// opens or creates the database file
func (db *KV) Open() error {
	// open or create the file
	fd, err := createFileSync(db.Path)
	if err != nil {
		return err
	}
	db.fd = fd

	// get file size
	finfo, err := os.Stat(db.Path)
	if err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("stat file: %w", err)
	}
	fileSize := finfo.Size()

	// create the initial mmap
	if fileSize > 0 {
		if err := extendMmap(db, int(fileSize)); err != nil {
			_ = unix.Close(fd)
			return err
		}
	}

	// read the meta page
	if err := readRoot(db, fileSize); err != nil {
		_ = unix.Close(fd)
		return err
	}

	// initialize page updates map
	db.page.updates = make(map[uint64][]byte)

	// set up the B-tree callbacks
	db.tree.SetCallbacks(db.pageRead, db.pageNew, db.pageDel)

	// set up the free list callbacks
	db.free.SetCallbacks(db.pageRead, db.pageAppend, db.pageSet)

	return nil
}

// Close closes the database file by closing the file descriptor
func (db *KV) Close() error {
	// unmap all chunks
	for _, chunk := range db.mmap.chunks {
		if err := unix.Munmap(chunk); err != nil {
			return fmt.Errorf("munmap: %w", err)
		}
	}
	return unix.Close(db.fd)
}

// retrieves a value by searching the key in the btree
func (db *KV) Get(key []byte) ([]byte, bool) {
	return db.tree.Get(key)
}

// inserts or updates a key-value pair
func (db *KV) Set(key []byte, val []byte) error {
	meta := saveMeta(db)
	db.tree.Insert(key, val)
	return updateOrRevert(db, meta)
}

// deletes a key from the database
func (db *KV) Del(key []byte) (bool, error) {
	meta := saveMeta(db)
	deleted := db.tree.Delete(key)
	if !deleted {
		return false, nil
	}
	return true, updateOrRevert(db, meta)
}

// Update modes constants
const (
	ModeUpsert     = 0 // insert or replace
	ModeUpdateOnly = 1 // update existing keys
	ModeInsertOnly = 2 // only add new keys
)

// Update inserts or updates a key-value pair based on mode
func (db *KV) Update(key []byte, val []byte, mode int) (bool, error) {
	switch mode {
	case ModeUpsert:
		// Insert or replace
		err := db.Set(key, val)
		return true, err
	case ModeUpdateOnly:
		// Only update if key exists
		if _, exists := db.Get(key); !exists {
			return false, nil
		}
		err := db.Set(key, val)
		return true, err
	case ModeInsertOnly:
		// Only insert if key doesn't exist
		if _, exists := db.Get(key); exists {
			return false, nil
		}
		err := db.Set(key, val)
		return true, err
	default:
		return false, fmt.Errorf("unknown update mode: %d", mode)
	}
}

// ForEach iterates over all key-value pairs in the database
func (db *KV) ForEach(cb func(key, val []byte) bool) {
	db.tree.ForEach(cb)
}

// ForEachWithPrefix iterates over all key-value pairs with the given prefix
func (db *KV) ForEachWithPrefix(prefix []byte, cb func(key, val []byte) bool) {
	db.tree.ForEachWithPrefix(prefix, cb)
}

// createFileSync creates or opens a file with fsync on the directory
func createFileSync(file string) (int, error) {
	// obtain the directory fd
	flags := os.O_RDONLY | unix.O_DIRECTORY
	dirfd, err := unix.Open(path.Dir(file), flags, 0o644)
	if err != nil {
		return -1, fmt.Errorf("open directory: %w", err)
	}
	defer unix.Close(dirfd)

	// open or create the file
	flags = os.O_RDWR | os.O_CREATE
	fd, err := unix.Openat(dirfd, path.Base(file), flags, 0o644)
	if err != nil {
		return -1, fmt.Errorf("open file: %w", err)
	}

	// fsync the directory
	if err = unix.Fsync(dirfd); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("fsync directory: %w", err)
	}

	return fd, nil
}

// pageRead reads a page from the mmap'ed file or from pending updates
func (db *KV) pageRead(ptr uint64) []byte {
	// Check pending updates first
	if page, ok := db.page.updates[ptr]; ok {
		return page
	}
	// Read from mmap
	start := uint64(0)
	for _, chunk := range db.mmap.chunks {
		end := start + uint64(len(chunk))/BTREE_PAGE_SIZE
		if ptr < end {
			offset := BTREE_PAGE_SIZE * (ptr - start)
			return chunk[offset : offset+BTREE_PAGE_SIZE]
		}
		start = end
	}
	panic("bad ptr")
}

// pageNew allocates a page - tries free list first, then appends (BTree.new callback)
func (db *KV) pageNew(node []byte) uint64 {
	// Try to reuse a page from the free list
	ptr := db.free.Get()
	if ptr == 0 {
		// No free pages available, append a new one
		ptr = db.pageAppend(node)
	} else {
		// Reusing a free page
		db.page.updates[ptr] = node
	}
	return ptr
}

// pageAppend appends a new page to the file (for free list use)
func (db *KV) pageAppend(node []byte) uint64 {
	ptr := db.page.flushed + uint64(db.page.nappend)
	db.page.nappend++
	db.page.updates[ptr] = node
	return ptr
}

// pageSet updates an existing page in place (for free list node updates)
func (db *KV) pageSet(ptr uint64, node []byte) {
	db.page.updates[ptr] = node
}

// pageDel marks a page as freed (BTree.del callback)
func (db *KV) pageDel(ptr uint64) {
	db.free.Free(ptr)
}

// extendMmap extends the memory mapping
func extendMmap(db *KV, size int) error {
	if size <= db.mmap.total {
		return nil // enough range
	}

	alloc := max(db.mmap.total, 64<<20) // double the current address space, min 64MB
	for db.mmap.total+alloc < size {
		alloc *= 2 // still not enough?
	}

	chunk, err := unix.Mmap(
		db.fd, int64(db.mmap.total), alloc,
		unix.PROT_READ, unix.MAP_SHARED,
	)
	if err != nil {
		return fmt.Errorf("mmap: %w", err)
	}

	db.mmap.total += alloc
	db.mmap.chunks = append(db.mmap.chunks, chunk)
	return nil
}

// writePages writes pending pages to the file
func writePages(db *KV) error {
	if len(db.page.updates) == 0 {
		return nil
	}

	// extend the mmap if needed for new pages
	size := (int(db.page.flushed) + db.page.nappend) * BTREE_PAGE_SIZE
	if err := extendMmap(db, size); err != nil {
		return err
	}

	// write all pending pages to the file
	for ptr, page := range db.page.updates {
		offset := int64(ptr * BTREE_PAGE_SIZE)
		if _, err := unix.Pwrite(db.fd, page, offset); err != nil {
			return err
		}
	}

	// update flushed page count
	db.page.flushed += uint64(db.page.nappend)
	// clear pending state
	db.page.updates = make(map[uint64][]byte)
	db.page.nappend = 0
	return nil
}

// saveMeta serializes the database metadata
// Format: | sig (16B) | root_ptr (8B) | page_used (8B) | fl_head (8B) | fl_tail (8B) | fl_total (8B) |
func saveMeta(db *KV) []byte {
	var data [56]byte
	copy(data[:16], []byte(DB_SIG))
	binary.LittleEndian.PutUint64(data[16:], db.tree.Root())
	binary.LittleEndian.PutUint64(data[24:], db.page.flushed+uint64(db.page.nappend))
	binary.LittleEndian.PutUint64(data[32:], db.free.Head())
	binary.LittleEndian.PutUint64(data[40:], db.free.Tail())
	binary.LittleEndian.PutUint64(data[48:], uint64(db.free.Total()))
	return data[:]
}

// loadMeta deserializes database metadata
func loadMeta(db *KV, data []byte) error {
	if len(data) < 56 {
		return errors.New("meta page too small")
	}
	sig := string(data[:16])
	if sig != DB_SIG {
		return fmt.Errorf("invalid signature: %s", sig)
	}
	db.tree.SetRoot(binary.LittleEndian.Uint64(data[16:]))
	db.page.flushed = binary.LittleEndian.Uint64(data[24:])
	db.free.SetHead(binary.LittleEndian.Uint64(data[32:]))
	db.free.SetTail(binary.LittleEndian.Uint64(data[40:]))
	db.free.SetTotal(int(binary.LittleEndian.Uint64(data[48:])))
	return nil
}

// readRoot reads the meta page from the file
func readRoot(db *KV, fileSize int64) error {
	if fileSize == 0 {
		// empty file, the meta page is initialized on the 1st write
		db.page.flushed = 1
		return nil
	}

	// read the page
	data := db.mmap.chunks[0]
	if err := loadMeta(db, data); err != nil {
		return err
	}

	// verify the page count makes sense
	if int64(db.page.flushed)*BTREE_PAGE_SIZE > fileSize {
		return errors.New("file size mismatch with meta page")
	}

	return nil
}

// updateRoot writes the meta page atomically
func updateRoot(db *KV) error {
	if _, err := unix.Pwrite(db.fd, saveMeta(db), 0); err != nil {
		return fmt.Errorf("write meta page: %w", err)
	}
	return nil
}

// updateFile persists all changes to disk
func updateFile(db *KV) error {
	// 1. Write new nodes
	if err := writePages(db); err != nil {
		return err
	}
	// 2. fsync to enforce the order between 1 and 3
	if err := unix.Fsync(db.fd); err != nil {
		return err
	}
	// 3. Update the root pointer atomically
	if err := updateRoot(db); err != nil {
		return err
	}
	// 4. fsync to make everything persistent
	return unix.Fsync(db.fd)
}

// updateOrRevert performs update with rollback on error
func updateOrRevert(db *KV, meta []byte) error {
	// ensure the on-disk meta page matches the in-memory one after an error
	if db.failed {
		if err := updateRoot(db); err != nil {
			return err
		}
		if err := unix.Fsync(db.fd); err != nil {
			return err
		}
		db.failed = false
	}

	// flush pending freed pages to the free list
	db.free.Update()

	// 2-phase update
	err := updateFile(db)
	if err != nil {
		// the on-disk meta page is in an unknown state
		db.failed = true
		// revert the in-memory states to allow reads
		_ = loadMeta(db, meta)
		// discard temporaries
		db.page.updates = make(map[uint64][]byte)
		db.page.nappend = 0
		db.free.ClearPending()
	}
	return err
}

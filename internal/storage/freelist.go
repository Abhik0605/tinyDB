package storage

import (
	"encoding/binary"
)

/*
FREE LIST STRUCTURE OVERVIEW:
The free list is a linked list of disk pages that store pointers to freed pages.
Each free list node can store multiple page pointers plus a "next" pointer to the next node.

FREE LIST NODE LAYOUT (stored on a single page):
| size (2B) | next (8B) | pointers (8B each) |

- size: number of page pointers stored in this node
- next: pointer to the next free list node (0 if this is the last node)
- pointers: array of freed page pointers

The free list is managed in a copy-on-write manner during transactions:
- freed pages are added to a pending list during the transaction
- at commit time, pending freed pages are written to new free list nodes
- old free list nodes are themselves added to the free list

To handle this chicken-and-egg problem (freeing old free list nodes):
- We maintain two separate lists: head/tail for reading, updates go to head only
- Pages popped from the free list go to a "reuse" list
- At commit, pages from both pending and reuse lists may be added back
*/

const (
	// Free list node header: | size (2B) | next (8B) |
	FREE_LIST_HEADER = 2 + 8
	// Maximum pointers per free list node
	FREE_LIST_CAP = (BTREE_PAGE_SIZE - FREE_LIST_HEADER) / 8
)

// FreeList manages a list of free pages that can be reused.
// It uses a linked list of nodes stored on disk, where each node
// contains pointers to multiple freed pages.
type FreeList struct {
	// callbacks for reading/writing pages
	get func(uint64) []byte  // read a page
	new func([]byte) uint64  // append a new page
	set func(uint64, []byte) // update an existing page

	// persisted data - stored in meta page
	head uint64 // pointer to first free list node (for popping)
	tail uint64 // pointer to last free list node (for pushing)
	// number of pages in the free list (used to decide when to reuse)
	total int

	// in-memory pending state during a transaction
	// pages to be added to the free list at the end of a transaction
	updates []uint64
}

// Head returns the head pointer of the free list
func (fl *FreeList) Head() uint64 {
	return fl.head
}

// Tail returns the tail pointer of the free list
func (fl *FreeList) Tail() uint64 {
	return fl.tail
}

// Total returns the total number of items in the free list
func (fl *FreeList) Total() int {
	return fl.total
}

// SetHead sets the head pointer
func (fl *FreeList) SetHead(head uint64) {
	fl.head = head
}

// SetTail sets the tail pointer
func (fl *FreeList) SetTail(tail uint64) {
	fl.tail = tail
}

// SetTotal sets the total count
func (fl *FreeList) SetTotal(total int) {
	fl.total = total
}

// SetCallbacks sets the callback functions for the free list
func (fl *FreeList) SetCallbacks(
	get func(uint64) []byte,
	new func([]byte) uint64,
	set func(uint64, []byte),
) {
	fl.get = get
	fl.new = new
	fl.set = set
}

// Get pops a page pointer from the free list for reuse.
// Returns 0 if the free list is empty.
func (fl *FreeList) Get() uint64 {
	if fl.head == 0 {
		return 0 // empty list
	}

	node := fl.get(fl.head)
	size := flNodeGetSize(node)
	if size == 0 {
		return 0 // no pointers in this node
	}

	// Pop the last pointer from the current head node
	ptr := flNodeGetPtr(node, int(size-1))

	// Update the node or move to next node
	if size == 1 {
		// This node becomes empty, add it to updates for recycling
		// and move head to the next node
		next := flNodeGetNext(node)
		fl.updates = append(fl.updates, fl.head)
		fl.head = next
		if fl.head == 0 {
			fl.tail = 0 // list is now empty
		}
	} else {
		// Just decrement the size - need to copy since node might be from mmap
		updatedNode := make([]byte, BTREE_PAGE_SIZE)
		copy(updatedNode, node)
		flNodeSetSize(updatedNode, size-1)
		fl.set(fl.head, updatedNode)
	}

	fl.total--
	return ptr
}

// Free adds a page pointer to the pending updates list.
// The page will be added to the free list when Update is called.
func (fl *FreeList) Free(ptr uint64) {
	fl.updates = append(fl.updates, ptr)
}

// PendingCount returns the number of pages pending to be added to the free list
func (fl *FreeList) PendingCount() int {
	return len(fl.updates)
}

// ClearPending clears the pending updates (used for rollback)
func (fl *FreeList) ClearPending() {
	fl.updates = fl.updates[:0]
}

// Update flushes pending freed pages to the free list on disk.
// It creates new nodes as needed and links them to the list.
func (fl *FreeList) Update() {
	if len(fl.updates) == 0 {
		return
	}

	// Process updates in batches that fit in a single node
	for len(fl.updates) > 0 {
		// Create a new node
		node := make([]byte, BTREE_PAGE_SIZE)

		// How many can we fit?
		n := min(len(fl.updates), FREE_LIST_CAP)

		// Fill the node with pointers
		flNodeSetSize(node, uint16(n))
		flNodeSetNext(node, 0) // will be updated if we have more
		for i := 0; i < n; i++ {
			flNodeSetPtr(node, i, fl.updates[i])
		}
		fl.updates = fl.updates[n:]

		// Append the new node to the list
		ptr := fl.new(node)
		fl.total += n

		if fl.tail == 0 {
			// First node in the list
			fl.head = ptr
			fl.tail = ptr
		} else {
			// Link from the old tail - need to copy since it might be from mmap
			tailNode := make([]byte, BTREE_PAGE_SIZE)
			copy(tailNode, fl.get(fl.tail))
			flNodeSetNext(tailNode, ptr)
			fl.set(fl.tail, tailNode)
			fl.tail = ptr
		}
	}
}

// Free list node helpers - encode/decode node data

// flNodeGetSize returns the number of pointers in this node
func flNodeGetSize(node []byte) uint16 {
	return binary.LittleEndian.Uint16(node[0:2])
}

// flNodeSetSize sets the number of pointers in this node
func flNodeSetSize(node []byte, size uint16) {
	binary.LittleEndian.PutUint16(node[0:2], size)
}

// flNodeGetNext returns the pointer to the next node
func flNodeGetNext(node []byte) uint64 {
	return binary.LittleEndian.Uint64(node[2:10])
}

// flNodeSetNext sets the pointer to the next node
func flNodeSetNext(node []byte, next uint64) {
	binary.LittleEndian.PutUint64(node[2:10], next)
}

// flNodeGetPtr returns the page pointer at the given index
func flNodeGetPtr(node []byte, idx int) uint64 {
	offset := FREE_LIST_HEADER + 8*idx
	return binary.LittleEndian.Uint64(node[offset : offset+8])
}

// flNodeSetPtr sets the page pointer at the given index
func flNodeSetPtr(node []byte, idx int, ptr uint64) {
	offset := FREE_LIST_HEADER + 8*idx
	binary.LittleEndian.PutUint64(node[offset:offset+8], ptr)
}

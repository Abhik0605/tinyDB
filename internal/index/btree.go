/*
Package index implements a B-tree data structure for efficient key-value storage.

B-TREE STRUCTURE OVERVIEW:
The B-tree starts as a single leaf node (root) that stores actual key-value pairs.
When the root becomes full, it splits into multiple child nodes, and a new internal
root is created above them. This process continues as the tree grows.

NODE LAYOUT:
Each node is stored as a byte slice with the following structure:

Byte Position:

0         4         8         8*n       8*n+2          8*n+2+2*n

┌─────────┬─────────┬─────────┬─────────┬─────────────┬─────────────┐
│ HEADER  │  PTRS   │ OFFSETS │ (unused)│   KV DATA   │             │
│  (4B)   │ (8*n)   │ (2*n)   │         │   SECTION   │
└─────────┴─────────┴─────────┴─────────┴─────────────┴─────────────┘

HEADER (4 bytes): [type(2B) | nkeys(2B)]

PTRS (8*n bytes): Pointers to child nodes (for internal nodes) or 0 (for leaves)

OFFSETS (2*n bytes): Byte offsets to locate each key-value pair
KV DATA: Variable-length key-value pairs [klen( ) | vlen(2B) | key | value]

TREE GROWTH PROCESS:

 1. Initial State: Root is a leaf node containing key-value data
    Root (LEAF): ["apple":"fruit", "banana":"yellow", ...]

 2. When Root Gets Full: The root splits into child nodes
    Left Child:  ["apple":"fruit", "banana":"yellow"]
    Right Child: ["cherry":"red", "date":"sweet"]

 3. New Root Created: A new internal root is created above the children
    New Root (INTERNAL): [boundary_key:ptr1, "cherry":ptr2]
    ↓                ↓
    Left Child (LEAF):   ["apple":"fruit", "banana":"yellow"]
    Right Child (LEAF):  ["cherry":"red", "date":"sweet"]

KEY ROUTING:
- Internal nodes store boundary keys copied from their children's first keys
- During lookup, the boundary keys determine which child to route to
- Each pointer corresponds to a child that handles a specific key range

POINTER MANAGEMENT:
- Pointers are uint64 values managed through callback functions:
  - get(ptr): Retrieves node data from a pointer
  - new(data): Stores node data and returns a pointer
  - del(ptr): Deallocates a node
*/
package index

import (
	"bytes"
	"encoding/binary"
)

const HEADER = 4
const BTREE_PAGE_SIZE = 4096
const BTREE_MAX_KEY_SIZE = 1000
const BTREE_MAX_VAL_SIZE = 3000

// This is just an indicator
const (
	BNODE_NODE = 1 // internal nodes without values
	BNODE_LEAF = 2 // leaf nodes with values
)

func assert(condition bool) {
	if !condition {
		panic("assertion failed")
	}
}

func init() {
	node1max := HEADER + 8 + 2 + 4 + BTREE_MAX_KEY_SIZE + BTREE_MAX_VAL_SIZE
	assert(node1max <= BTREE_PAGE_SIZE)
}

type BTree struct {
	root uint64
	get  func(uint64) []byte // dereference a pointer
	new  func([]byte) uint64 // allocate a new page
	del  func(uint64)        // deallocate a page
}

type BNode []byte

func (node BNode) btype() uint16 {
	return binary.LittleEndian.Uint16(node[0:2])
}
func (node BNode) nkeys() uint16 {
	return binary.LittleEndian.Uint16(node[2:4])
}

func (node BNode) setHeader(btype uint16, nkeys uint16) {
	binary.LittleEndian.PutUint16(node[0:2], btype)
	// btype can be either BNODE_NODE or BNODE_LEAF indicating the node type
	binary.LittleEndian.PutUint16(node[2:4], nkeys)
}

// pointers
func (node BNode) getPtr(idx uint16) uint64 {
	assert(idx < node.nkeys())
	pos := HEADER + 8*idx
	return binary.LittleEndian.Uint64(node[pos:])
}
func (node BNode) setPtr(idx uint16, val uint64) {
	assert(idx < node.nkeys())
	pos := HEADER + 8*idx
	binary.LittleEndian.PutUint64(node[pos:], val)
}

// offset list
func offsetPos(node BNode, idx uint16) uint16 {
	assert(1 <= idx && idx <= node.nkeys())
	return HEADER + 8*node.nkeys() + 2*(idx-1)
}

func (node BNode) getOffset(idx uint16) uint16 {
	if idx == 0 {
		return 0
	}
	return binary.LittleEndian.Uint16(node[offsetPos(node, idx):])
}

// An offset is the byte offset from the start of the node to the start of the KV pair.
// It indicates the start of the KV pair, not the end.
func (node BNode) setOffset(idx uint16, offset uint16) {
	// idx indicates the index of the KV pair not the byte position
	// offset represents the byte offset from the start of the KV DATA section
	//  to the start of the KV pair
	assert(idx <= node.nkeys())
	binary.LittleEndian.PutUint16(node[offsetPos(node, idx):], offset)
}

// key-values
func (node BNode) kvPos(idx uint16) uint16 {
	assert(idx <= node.nkeys())
	return HEADER + 8*node.nkeys() + 2*node.nkeys() + node.getOffset(idx)
}
func (node BNode) getKey(idx uint16) []byte {
	assert(idx < node.nkeys())
	pos := node.kvPos(idx)
	klen := binary.LittleEndian.Uint16(node[pos:])
	return node[pos+4:][:klen]
}
func (node BNode) getVal(idx uint16) []byte {
	assert(idx < node.nkeys())
	pos := node.kvPos(idx)
	klen := binary.LittleEndian.Uint16(node[pos:])
	vlen := binary.LittleEndian.Uint16(node[pos+2:])
	return node[pos+4+klen:][:vlen]
}

// node size in bytes
func (node BNode) nbytes() uint16 {
	return node.kvPos(node.nkeys())
}

// returns the first kid node whose range intersects the key. (kid[i] <= key)
// TODO: binary search
func nodeLookupLE(node BNode, key []byte) uint16 {
	nkeys := node.nkeys()
	found := uint16(0)
	// the first key is a copy from the parent node,
	// thus it's always less than or equal to the key.
	for i := uint16(1); i < nkeys; i++ {
		cmp := bytes.Compare(node.getKey(i), key)
		if cmp <= 0 {
			found = i
		}
		if cmp >= 0 {
			break
		}
	}
	return found
}

// copy a KV into the position
// this funcion is used for 2 purposes:
// 1. copy a key from a kid node to the parent node
// 2. copy a key from a leaf node to the parent node
func nodeAppendKV(new BNode, idx uint16, ptr uint64, key []byte, val []byte) {
	// ptrs
	new.setPtr(idx, ptr)
	// KVs
	pos := new.kvPos(idx)
	binary.LittleEndian.PutUint16(new[pos+0:], uint16(len(key))) // encode klen
	binary.LittleEndian.PutUint16(new[pos+2:], uint16(len(val))) // encode vlen
	copy(new[pos+4:], key)
	copy(new[pos+4+uint16(len(key)):], val)
	// the offset of the next key
	new.setOffset(idx+1, new.getOffset(idx)+4+uint16((len(key)+len(val))))
}

// copy multiple KVs into the position from the old node
func nodeAppendRange(
	new BNode, old BNode,
	dstNew uint16, srcOld uint16, n uint16,
) {
	for i := uint16(0); i < n; i++ {
		nodeAppendKV(new, dstNew+i, old.getPtr(srcOld+i), old.getKey(srcOld+i), old.getVal(srcOld+i))
	}
}

// When a child node splits, the parent node needs to be updated with new metadata.
func nodeReplaceKidN(
	tree *BTree, new BNode, old BNode, idx uint16,
	kids ...BNode,
) {
	inc := uint16(len(kids))
	// update the header
	new.setHeader(BNODE_NODE, old.nkeys()+inc-1)
	// copy the old KVs
	nodeAppendRange(new, old, 0, 0, idx)
	// copy the new KVs
	for i, node := range kids {
		nodeAppendKV(new, idx+uint16(i), tree.new(node), node.getKey(0), nil)
		// 				  ^position      ^pointer        ^key            ^val
	}
	// copy the remaining old KVs
	nodeAppendRange(new, old, idx+inc, idx+1, old.nkeys()-(idx+1))
}

// split a oversized node into 2 so that the 2nd node always fits on a page
func nodeSplit2(left BNode, right BNode, old BNode) {
	n := old.nkeys()
	left.setHeader(old.btype(), n/2)
	right.setHeader(old.btype(), n-n/2)
	nodeAppendRange(left, old, 0, 0, n/2)
	nodeAppendRange(right, old, 0, n/2, n-n/2)
}

// split a node if it's too big. the results are 1~3 nodes.
func nodeSplit3(old BNode) (uint16, [3]BNode) {
	if old.nbytes() <= BTREE_PAGE_SIZE {
		old = old[:BTREE_PAGE_SIZE]
		return 1, [3]BNode{old} // not split
	}
	left := BNode(make([]byte, 2*BTREE_PAGE_SIZE)) // might be split later
	right := BNode(make([]byte, BTREE_PAGE_SIZE))
	nodeSplit2(left, right, old)
	if left.nbytes() <= BTREE_PAGE_SIZE {
		left = left[:BTREE_PAGE_SIZE]
		return 2, [3]BNode{left, right} // 2 nodes
	}
	leftleft := BNode(make([]byte, BTREE_PAGE_SIZE))
	middle := BNode(make([]byte, BTREE_PAGE_SIZE))
	nodeSplit2(leftleft, middle, left)
	assert(leftleft.nbytes() <= BTREE_PAGE_SIZE)
	return 3, [3]BNode{leftleft, middle, right} // 3 nodes
}

// add a new key to a leaf node
func leafInsert(
	new BNode, old BNode, idx uint16,
	key []byte, val []byte,
) {
	new.setHeader(BNODE_LEAF, old.nkeys()+1) // setup the header
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKV(new, idx, 0, key, val)
	nodeAppendRange(new, old, idx+1, idx, old.nkeys()-idx)
}

// updates an existing key instead of inserting a duplicate key
func leafUpdate(
	new BNode, old BNode, idx uint16,
	key []byte, val []byte,
) {
	new.setHeader(BNODE_LEAF, old.nkeys()) // setup the header
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendKV(new, idx, 0, key, val)
	nodeAppendRange(new, old, idx+1, idx+1, old.nkeys()-(idx+1))
}

func treeInsert(tree *BTree, node BNode, key []byte, val []byte) BNode {
	// the result node.
	// it's allowed to be bigger than 1 page and will be split if so
	new := BNode(make([]byte, 2*BTREE_PAGE_SIZE))
	// where to insert the key?
	idx := nodeLookupLE(node, key)
	// act depending on the node type
	switch node.btype() {
	case BNODE_LEAF:
		// leaf, node.getKey(idx) <= key
		if bytes.Equal(key, node.getKey(idx)) {
			// found the key, update it.
			leafUpdate(new, node, idx, key, val)
		} else {
			// insert it after the position.
			leafInsert(new, node, idx+1, key, val)
		}
	case BNODE_NODE:
		// internal node, insert it to a kid node.
		nodeInsert(tree, new, node, idx, key, val)
	default:
		panic("bad node!")
	}
	return new
}

// part of the treeInsert(): KV insertion to an internal node
func nodeInsert(
	tree *BTree, new BNode, node BNode, idx uint16,
	key []byte, val []byte,
) {
	kptr := node.getPtr(idx)
	// recursive insertion to the kid node
	knode := treeInsert(tree, tree.get(kptr), key, val)
	// split the result
	nsplit, split := nodeSplit3(knode)
	// deallocate the kid node
	tree.del(kptr)
	// update the kid links
	nodeReplaceKidN(tree, new, node, idx, split[:nsplit]...)
}

func (tree *BTree) Insert(key []byte, val []byte) {
	if tree.root == 0 {
		// create the first node
		root := BNode(make([]byte, BTREE_PAGE_SIZE))
		root.setHeader(BNODE_LEAF, 2)
		// a dummy key, this makes the tree cover the whole key space.
		// thus a lookup can always find a containing node.
		nodeAppendKV(root, 0, 0, nil, nil)
		nodeAppendKV(root, 1, 0, key, val)
		tree.root = tree.new(root)
		return
	}
	node := treeInsert(tree, tree.get(tree.root), key, val)
	nsplit, split := nodeSplit3(node)
	tree.del(tree.root)
	if nsplit > 1 {
		// the root was split, add a new level.
		root := BNode(make([]byte, BTREE_PAGE_SIZE))
		root.setHeader(BNODE_NODE, nsplit)
		for i, knode := range split[:nsplit] {
			ptr, key := tree.new(knode), knode.getKey(0)
			nodeAppendKV(root, uint16(i), ptr, key, nil)
		}
		tree.root = tree.new(root)
	} else {
		tree.root = tree.new(split[0])
	}
}

// remove a key from a leaf node
func leafDelete(
	new BNode, old BNode, idx uint16, key []byte,
) {
	if !bytes.Equal(old.getKey(idx), key) {
		panic("key not found")
	}
	new.setHeader(BNODE_LEAF, old.nkeys()-1)
	nodeAppendRange(new, old, 0, 0, idx)
	nodeAppendRange(new, old, idx, idx+1, old.nkeys()-(idx+1))
}

// merge 2 nodes into 1
func nodeMerge(new BNode, left BNode, right BNode) {
	new.setHeader(left.btype(), left.nkeys()+right.nkeys())
	nodeAppendRange(new, left, 0, 0, left.nkeys())
	nodeAppendRange(new, right, left.nkeys(), 0, right.nkeys())
}

// replace 2 adjacent links with 1
// Replaces children at positions idx and idx+1 with a single merged child
func nodeReplace2Kid(
	new BNode, old BNode, idx uint16, ptr uint64, key []byte,
) {
	new.setHeader(BNODE_NODE, old.nkeys()-1)
	nodeAppendRange(new, old, 0, 0, idx)                         // copy old[0:idx] to new[0:idx]
	nodeAppendKV(new, idx, ptr, key, nil)                        // set new[idx] to merged child
	nodeAppendRange(new, old, idx+1, idx+2, old.nkeys()-(idx+2)) // copy old[idx+2:] to new[idx+1:]
}

// should the updated kid be merged with a sibling?
func shouldMerge(
	tree *BTree, node BNode,
	idx uint16, updated BNode,
) (int, BNode) {
	if updated.nbytes() > BTREE_PAGE_SIZE/4 {
		return 0, BNode{}
	}
	if idx > 0 {
		sibling := BNode(tree.get(node.getPtr(idx - 1)))
		merged := sibling.nbytes() + updated.nbytes() - HEADER
		if merged <= BTREE_PAGE_SIZE {
			return -1, sibling // left
		}
	}
	if idx+1 < node.nkeys() {
		sibling := BNode(tree.get(node.getPtr(idx + 1)))
		merged := sibling.nbytes() + updated.nbytes() - HEADER
		if merged <= BTREE_PAGE_SIZE {
			return +1, sibling // right
		}
	}
	return 0, BNode{}
}

// delete a key from the tree
func treeDelete(tree *BTree, node BNode, key []byte) BNode {
	// where to find the key?
	idx := nodeLookupLE(node, key)
	// act depending on the node type
	switch node.btype() {
	case BNODE_LEAF:
		// leaf, check if key exists
		if !bytes.Equal(key, node.getKey(idx)) {
			return BNode{} // key not found
		}
		// delete the key
		new := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafDelete(new, node, idx, key)
		return new
	case BNODE_NODE:
		// internal node, delete from kid node
		return nodeDelete(tree, node, idx, key)
	default:
		panic("bad node!")
	}
}

// delete a key from an internal node; part of the treeDelete()
func nodeDelete(tree *BTree, node BNode, idx uint16, key []byte) BNode {
	// recurse into the kid
	kptr := node.getPtr(idx)
	updated := treeDelete(tree, tree.get(kptr), key)
	if len(updated) == 0 {
		return BNode{} // not found
	}
	tree.del(kptr)
	new := BNode(make([]byte, BTREE_PAGE_SIZE))
	// check for merging
	mergeDir, sibling := shouldMerge(tree, node, idx, updated)
	switch {
	case mergeDir < 0: // left
		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, sibling, updated)
		tree.del(node.getPtr(idx - 1))
		nodeReplace2Kid(new, node, idx-1, tree.new(merged), merged.getKey(0))
	case mergeDir > 0: // right
		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, updated, sibling)
		tree.del(node.getPtr(idx + 1))
		nodeReplace2Kid(new, node, idx, tree.new(merged), merged.getKey(0))
	case mergeDir == 0 && updated.nkeys() == 0:
		assert(node.nkeys() == 1 && idx == 0) // 1 empty child but no sibling
		new.setHeader(BNODE_NODE, 0)          // the parent becomes empty too
	case mergeDir == 0 && updated.nkeys() > 0: // no merge
		nodeReplaceKidN(tree, new, node, idx, updated)
	}
	return new
}

// SetCallbacks sets the callback functions for the B-tree
func (tree *BTree) SetCallbacks(get func(uint64) []byte, new func([]byte) uint64, del func(uint64)) {
	tree.get = get
	tree.new = new
	tree.del = del
}

// Root returns the root pointer
func (tree *BTree) Root() uint64 {
	return tree.root
}

// SetRoot sets the root pointer
func (tree *BTree) SetRoot(root uint64) {
	tree.root = root
}

// Get retrieves a value by key from the B-tree
func (tree *BTree) Get(key []byte) ([]byte, bool) {
	if tree.root == 0 {
		return nil, false
	}
	return treeGet(tree, tree.get(tree.root), key)
}

// treeGet searches for a key in the B-tree and returns its value
func treeGet(tree *BTree, node BNode, key []byte) ([]byte, bool) {
	idx := nodeLookupLE(node, key)
	switch node.btype() {
	case BNODE_LEAF:
		if bytes.Equal(key, node.getKey(idx)) {
			return node.getVal(idx), true
		}
		return nil, false
	case BNODE_NODE:
		return treeGet(tree, tree.get(node.getPtr(idx)), key)
	default:
		panic("bad node!")
	}
}

// Delete removes a key from the B-tree and returns whether it was found
func (tree *BTree) Delete(key []byte) bool {
	if tree.root == 0 {
		return false
	}
	updated := treeDelete(tree, tree.get(tree.root), key)
	if len(updated) == 0 {
		return false // key not found
	}
	tree.del(tree.root)
	if updated.nkeys() == 0 {
		tree.root = 0 // tree is empty
	} else if updated.btype() == BNODE_NODE && updated.nkeys() == 1 {
		// remove a level
		tree.root = updated.getPtr(0)
	} else {
		tree.root = tree.new(updated)
	}
	return true
}

// ForEach iterates over all key-value pairs in the B-tree.
// The callback function receives the key and value for each pair.
// If the callback returns false, iteration stops.
func (tree *BTree) ForEach(cb func(key, val []byte) bool) {
	if tree.root == 0 {
		return
	}
	treeForEach(tree, tree.get(tree.root), cb)
}

func treeForEach(tree *BTree, node BNode, cb func(key, val []byte) bool) bool {
	nkeys := node.nkeys()
	switch node.btype() {
	case BNODE_LEAF:
		for i := uint16(0); i < nkeys; i++ {
			key := node.getKey(i)
			val := node.getVal(i)
			// Skip the dummy key (empty key at index 0)
			if len(key) == 0 && i == 0 {
				continue
			}
			if !cb(key, val) {
				return false
			}
		}
	case BNODE_NODE:
		for i := uint16(0); i < nkeys; i++ {
			if !treeForEach(tree, tree.get(node.getPtr(i)), cb) {
				return false
			}
		}
	}
	return true
}

// ForEachWithPrefix iterates over all key-value pairs that have the given prefix.
func (tree *BTree) ForEachWithPrefix(prefix []byte, cb func(key, val []byte) bool) {
	if tree.root == 0 {
		return
	}
	tree.ForEach(func(key, val []byte) bool {
		if len(key) >= len(prefix) && bytes.Equal(key[:len(prefix)], prefix) {
			return cb(key, val)
		}
		return true // continue iteration
	})
}

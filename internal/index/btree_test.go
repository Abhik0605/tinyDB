package index

import (
	"bytes"
	"strings"
	"testing"
	"unsafe"
)

type C struct {
	tree  BTree
	ref   map[string]string // the reference data
	pages map[uint64]BNode  // in-memory pages
}

func newC() *C {
	pages := map[uint64]BNode{}
	return &C{
		tree: BTree{
			get: func(ptr uint64) []byte {
				node, ok := pages[ptr]
				assert(ok)
				return node
			},
			new: func(node []byte) uint64 {
				assert(BNode(node).nbytes() <= BTREE_PAGE_SIZE)
				ptr := uint64(uintptr(unsafe.Pointer(&node[0])))
				assert(pages[ptr] == nil)
				pages[ptr] = node
				return ptr
			},
			del: func(ptr uint64) {
				assert(pages[ptr] != nil)
				delete(pages, ptr)
			},
		},
		ref:   map[string]string{},
		pages: pages,
	}
}

func (c *C) add(key string, val string) {
	c.tree.Insert([]byte(key), []byte(val))
	c.ref[key] = val // reference data
}

func TestNodeReplaceKidN(t *testing.T) {
	c := newC()

	// Create old internal node with 3 children
	oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
	oldNode.setHeader(BNODE_NODE, 3)
	nodeAppendKV(oldNode, 0, 100, []byte("apple"), nil)
	nodeAppendKV(oldNode, 1, 200, []byte("banana"), nil)
	nodeAppendKV(oldNode, 2, 300, []byte("cherry"), nil)

	// Create 2 replacement child nodes (simulating a split)
	kid1 := BNode(make([]byte, BTREE_PAGE_SIZE))
	kid1.setHeader(BNODE_LEAF, 1)
	nodeAppendKV(kid1, 0, 0, []byte("banana"), []byte("yellow"))

	kid2 := BNode(make([]byte, BTREE_PAGE_SIZE))
	kid2.setHeader(BNODE_LEAF, 1)
	nodeAppendKV(kid2, 0, 0, []byte("blueberry"), []byte("blue"))

	// Replace child at index 1 with 2 children
	newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
	nodeReplaceKidN(&c.tree, newNode, oldNode, 1, kid1, kid2)

	// Test new node has correct number of keys (3 - 1 + 2 = 4)
	if newNode.nkeys() != 4 {
		t.Errorf("Expected 4 keys in new node, got %d", newNode.nkeys())
	}

	// Test keys are in correct order
	expectedKeys := []string{"apple", "banana", "blueberry", "cherry"}
	for i, expected := range expectedKeys {
		if string(newNode.getKey(uint16(i))) != expected {
			t.Errorf("Expected key[%d] = %s, got %s", i, expected, string(newNode.getKey(uint16(i))))
		}
	}

	// Test that pointers were updated (should be new pointers for replaced children)
	if newNode.getPtr(0) != 100 { // Original first child
		t.Errorf("Expected ptr[0] = 100, got %d", newNode.getPtr(0))
	}
	if newNode.getPtr(3) != 300 { // Original third child (now at index 3)
		t.Errorf("Expected ptr[3] = 300, got %d", newNode.getPtr(3))
	}

	// Test that middle pointers are new (from tree.new())
	if newNode.getPtr(1) == 200 || newNode.getPtr(2) == 200 {
		t.Error("Middle pointers should be new, not the old pointer 200")
	}
}
func TestNodeSplit2(t *testing.T) {
	// create a test node
	oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
	oldNode.setHeader(BNODE_LEAF, 3)
	nodeAppendKV(oldNode, 0, 10, []byte("apple"), []byte("fruit"))
	nodeAppendKV(oldNode, 1, 20, []byte("banana"), []byte("yellow"))
	nodeAppendKV(oldNode, 2, 30, []byte("cherry"), []byte("red"))

	left := BNode(make([]byte, BTREE_PAGE_SIZE))
	right := BNode(make([]byte, BTREE_PAGE_SIZE))
	nodeSplit2(left, right, oldNode)

	// Test left node
	if left.nkeys() != 1 {
		t.Errorf("Expected 1 key in left node, got %d", left.nkeys())
	}
	if string(left.getKey(0)) != "apple" {
		t.Errorf("Expected apple, got %s", string(left.getKey(0)))
	}
	if string(left.getVal(0)) != "fruit" {
		t.Errorf("Expected fruit, got %s", string(left.getVal(0)))
	}
	if left.getPtr(0) != 10 {
		t.Errorf("Expected ptr[0] = 10, got %d", left.getPtr(0))
	}

	// Test right node
	if right.nkeys() != 2 {
		t.Errorf("Expected 2 keys in right node, got %d", right.nkeys())
	}
	if string(right.getKey(0)) != "banana" {
		t.Errorf("Expected banana, got %s", string(right.getKey(0)))
	}
	if string(right.getVal(0)) != "yellow" {
		t.Errorf("Expected yellow, got %s", string(right.getVal(0)))
	}

}

func TestNodeSplit3(t *testing.T) {
	t.Run("no split - small node", func(t *testing.T) {
		// Create a small node that fits in one page
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 3)
		nodeAppendKV(oldNode, 0, 10, []byte("apple"), []byte("fruit"))
		nodeAppendKV(oldNode, 1, 20, []byte("banana"), []byte("yellow"))
		nodeAppendKV(oldNode, 2, 30, []byte("cherry"), []byte("red"))

		nsplit, split := nodeSplit3(oldNode)

		// Should return 1 node (no split)
		if nsplit != 1 {
			t.Errorf("Expected 1 node, got %d", nsplit)
		}

		// The returned node should have the same content as the original
		// (though it may be a different slice after resizing)
		if split[0].nkeys() != oldNode.nkeys() {
			t.Errorf("Expected %d keys, got %d", oldNode.nkeys(), split[0].nkeys())
		}
		if split[0].btype() != oldNode.btype() {
			t.Errorf("Expected type %d, got %d", oldNode.btype(), split[0].btype())
		}
		// Verify the data is preserved
		for i := uint16(0); i < oldNode.nkeys(); i++ {
			if !bytes.Equal(split[0].getKey(i), oldNode.getKey(i)) {
				t.Errorf("Key %d mismatch: expected %s, got %s",
					i, string(oldNode.getKey(i)), string(split[0].getKey(i)))
			}
			if !bytes.Equal(split[0].getVal(i), oldNode.getVal(i)) {
				t.Errorf("Val %d mismatch: expected %s, got %s",
					i, string(oldNode.getVal(i)), string(split[0].getVal(i)))
			}
		}
	})

	t.Run("split into 2 nodes", func(t *testing.T) {
		// Create a node that's too large and needs to be split into 2
		oldNode := BNode(make([]byte, 2*BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 20)

		// Fill with enough data to require a split
		for i := uint16(0); i < 20; i++ {
			key := []byte(strings.Repeat("k", 100))
			val := []byte(strings.Repeat("v", 100))
			nodeAppendKV(oldNode, i, uint64(i), key, val)
		}

		nsplit, split := nodeSplit3(oldNode)

		// Should return 2 nodes
		if nsplit != 2 {
			t.Errorf("Expected 2 nodes, got %d", nsplit)
		}

		// Both nodes should fit in a page
		if split[0].nbytes() > BTREE_PAGE_SIZE {
			t.Errorf("First node too large: %d bytes", split[0].nbytes())
		}
		if split[1].nbytes() > BTREE_PAGE_SIZE {
			t.Errorf("Second node too large: %d bytes", split[1].nbytes())
		}

		// Total keys should be preserved
		totalKeys := split[0].nkeys() + split[1].nkeys()
		if totalKeys != oldNode.nkeys() {
			t.Errorf("Expected %d total keys, got %d", oldNode.nkeys(), totalKeys)
		}

		// Both nodes should have the same type
		if split[0].btype() != oldNode.btype() {
			t.Errorf("First node type mismatch")
		}
		if split[1].btype() != oldNode.btype() {
			t.Errorf("Second node type mismatch")
		}
	})

	t.Run("split into 3 nodes", func(t *testing.T) {
		// To trigger a 3-way split, we need a node where:
		// 1. The node is > BTREE_PAGE_SIZE (so it splits into left and right)
		// 2. The left node after split is still > BTREE_PAGE_SIZE (so it splits again)

		// Create a large buffer for the oversized node
		oldNode := BNode(make([]byte, 3*BTREE_PAGE_SIZE))

		// We'll add entries until we have a node that's large enough
		// Each entry: header overhead + key + val
		// Approximate: 10 bytes overhead + 150 key + 150 val = 310 bytes per entry
		// To get ~6KB (1.5 pages), we need about 20 entries
		nkeys := uint16(30)
		oldNode.setHeader(BNODE_LEAF, nkeys)

		// Fill with data
		for i := uint16(0); i < nkeys; i++ {
			// Use medium-sized keys and values
			key := []byte(strings.Repeat("k", 120))
			val := []byte(strings.Repeat("v", 120))
			nodeAppendKV(oldNode, i, uint64(i), key, val)
		}

		// Verify the node is actually large enough to require splitting
		if oldNode.nbytes() <= BTREE_PAGE_SIZE {
			t.Skip("Node not large enough to test 3-way split")
		}

		nsplit, split := nodeSplit3(oldNode)

		// Should return either 2 or 3 nodes depending on the data
		if nsplit < 2 || nsplit > 3 {
			t.Errorf("Expected 2 or 3 nodes, got %d", nsplit)
		}

		// All nodes should fit in a page
		for i := uint16(0); i < nsplit; i++ {
			if split[i].nbytes() > BTREE_PAGE_SIZE {
				t.Errorf("Node %d too large: %d bytes", i, split[i].nbytes())
			}
		}

		// Total keys should be preserved
		var totalKeys uint16
		for i := uint16(0); i < nsplit; i++ {
			totalKeys += split[i].nkeys()
		}
		if totalKeys != oldNode.nkeys() {
			t.Errorf("Expected %d total keys, got %d", oldNode.nkeys(), totalKeys)
		}

		// All nodes should have the same type
		for i := uint16(0); i < nsplit; i++ {
			if split[i].btype() != oldNode.btype() {
				t.Errorf("Node %d type mismatch", i)
			}
		}
	})
}

// Unit tests for B-tree node operations
func TestNodeAppendRange(t *testing.T) {
	// Create source node with 3 KV pairs
	oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
	oldNode.setHeader(BNODE_LEAF, 3)
	nodeAppendKV(oldNode, 0, 10, []byte("apple"), []byte("fruit"))
	nodeAppendKV(oldNode, 1, 20, []byte("banana"), []byte("yellow"))
	nodeAppendKV(oldNode, 2, 30, []byte("cherry"), []byte("red"))

	// Create destination node
	newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
	newNode.setHeader(BNODE_LEAF, 2)

	// Copy 2 KV pairs from oldNode[1:3] to newNode[0:2]
	nodeAppendRange(newNode, oldNode, 0, 1, 2)

	// Test copied data
	if string(newNode.getKey(0)) != "banana" {
		t.Errorf("Expected banana, got %s", string(newNode.getKey(0)))
	}
	if string(newNode.getVal(0)) != "yellow" {
		t.Errorf("Expected yellow, got %s", string(newNode.getVal(0)))
	}
	if string(newNode.getKey(1)) != "cherry" {
		t.Errorf("Expected cherry, got %s", string(newNode.getKey(1)))
	}
	if string(newNode.getVal(1)) != "red" {
		t.Errorf("Expected red, got %s", string(newNode.getVal(1)))
	}

	// Test pointers were copied
	if newNode.getPtr(0) != 20 {
		t.Errorf("Expected ptr[0] = 20, got %d", newNode.getPtr(0))
	}
	if newNode.getPtr(1) != 30 {
		t.Errorf("Expected ptr[1] = 30, got %d", newNode.getPtr(1))
	}
}

func TestNodeAppendKV(t *testing.T) {
	// Create a test node
	node := BNode(make([]byte, BTREE_PAGE_SIZE))
	// create a leaf node with 2 keys
	node.setHeader(BNODE_LEAF, 2)

	// set key-values
	nodeAppendKV(node, 0, 1, []byte("key1"), []byte("val1"))
	nodeAppendKV(node, 1, 2, []byte("key2"), []byte("val2"))

	// Test basic node operations
	if node.btype() != BNODE_LEAF {
		t.Errorf("Expected BNODE_LEAF, got %d", node.btype())
	}

	// Test key count
	if node.nkeys() != 2 {
		t.Errorf("Expected 2 keys, got %d", node.nkeys())
	}

	// Test pointer operations
	if node.getPtr(0) != 1 {
		t.Errorf("Expected ptr[0] = 1, got %d", node.getPtr(0))
	}
	if node.getPtr(1) != 2 {
		t.Errorf("Expected ptr[1] = 2, got %d", node.getPtr(1))
	}

	// Test offset operations
	if node.getOffset(0) != 0 {
		t.Errorf("Expected offset[0] = 0, got %d", node.getOffset(0))
	}

	// should be 12 because len of KV pair 0 is 8 plus 4 bytes of header
	if node.getOffset(1) != 12 {
		t.Errorf("Expected offset[1] = 12, got %d", node.getOffset(1))
	}

	// Test key/value retrieval
	key1 := node.getKey(0)
	if string(key1) != "key1" {
		t.Errorf("Expected key1, got %s", string(key1))
	}

	// Test node size
	if node.nbytes() == 0 {
		t.Error("Node size should be greater than 0")
	}

	// Test kvPos calculation
	pos := node.kvPos(0)
	if pos < HEADER+8*2+2*2 {
		t.Errorf("kvPos too small: %d", pos)
	}
}

// Test BNode basic methods
func TestBNodeBasicMethods(t *testing.T) {
	t.Run("btype and nkeys", func(t *testing.T) {
		node := BNode(make([]byte, BTREE_PAGE_SIZE))
		node.setHeader(BNODE_LEAF, 5)

		if node.btype() != BNODE_LEAF {
			t.Errorf("Expected btype = %d, got %d", BNODE_LEAF, node.btype())
		}
		if node.nkeys() != 5 {
			t.Errorf("Expected nkeys = 5, got %d", node.nkeys())
		}

		// Test internal node
		node.setHeader(BNODE_NODE, 10)
		if node.btype() != BNODE_NODE {
			t.Errorf("Expected btype = %d, got %d", BNODE_NODE, node.btype())
		}
		if node.nkeys() != 10 {
			t.Errorf("Expected nkeys = 10, got %d", node.nkeys())
		}
	})

	t.Run("getPtr and setPtr", func(t *testing.T) {
		node := BNode(make([]byte, BTREE_PAGE_SIZE))
		node.setHeader(BNODE_NODE, 3)

		node.setPtr(0, 100)
		node.setPtr(1, 200)
		node.setPtr(2, 300)

		if node.getPtr(0) != 100 {
			t.Errorf("Expected ptr[0] = 100, got %d", node.getPtr(0))
		}
		if node.getPtr(1) != 200 {
			t.Errorf("Expected ptr[1] = 200, got %d", node.getPtr(1))
		}
		if node.getPtr(2) != 300 {
			t.Errorf("Expected ptr[2] = 300, got %d", node.getPtr(2))
		}
	})

	t.Run("getOffset and setOffset", func(t *testing.T) {
		node := BNode(make([]byte, BTREE_PAGE_SIZE))
		node.setHeader(BNODE_LEAF, 3)

		node.setOffset(1, 10)
		node.setOffset(2, 25)
		node.setOffset(3, 40)

		if node.getOffset(0) != 0 {
			t.Errorf("Expected offset[0] = 0, got %d", node.getOffset(0))
		}
		if node.getOffset(1) != 10 {
			t.Errorf("Expected offset[1] = 10, got %d", node.getOffset(1))
		}
		if node.getOffset(2) != 25 {
			t.Errorf("Expected offset[2] = 25, got %d", node.getOffset(2))
		}
		if node.getOffset(3) != 40 {
			t.Errorf("Expected offset[3] = 40, got %d", node.getOffset(3))
		}
	})

	t.Run("getKey and getVal", func(t *testing.T) {
		node := BNode(make([]byte, BTREE_PAGE_SIZE))
		node.setHeader(BNODE_LEAF, 2)

		nodeAppendKV(node, 0, 0, []byte("apple"), []byte("red"))
		nodeAppendKV(node, 1, 0, []byte("banana"), []byte("yellow"))

		if string(node.getKey(0)) != "apple" {
			t.Errorf("Expected key[0] = apple, got %s", string(node.getKey(0)))
		}
		if string(node.getVal(0)) != "red" {
			t.Errorf("Expected val[0] = red, got %s", string(node.getVal(0)))
		}
		if string(node.getKey(1)) != "banana" {
			t.Errorf("Expected key[1] = banana, got %s", string(node.getKey(1)))
		}
		if string(node.getVal(1)) != "yellow" {
			t.Errorf("Expected val[1] = yellow, got %s", string(node.getVal(1)))
		}
	})

	t.Run("nbytes", func(t *testing.T) {
		node := BNode(make([]byte, BTREE_PAGE_SIZE))
		node.setHeader(BNODE_LEAF, 2)

		nodeAppendKV(node, 0, 0, []byte("key1"), []byte("val1"))
		nodeAppendKV(node, 1, 0, []byte("key2"), []byte("val2"))

		size := node.nbytes()
		// Size should be: HEADER + 8*nkeys + 2*nkeys + total KV data
		// HEADER=4, 8*2=16, 2*2=4, KV data = 2*(4+4+4) = 24
		// Total = 4 + 16 + 4 + 24 = 48
		expectedSize := uint16(48)
		if size != expectedSize {
			t.Errorf("Expected nbytes = %d, got %d", expectedSize, size)
		}
	})
}

// Test nodeLookupLE
func TestNodeLookupLE(t *testing.T) {
	t.Run("find exact match", func(t *testing.T) {
		node := BNode(make([]byte, BTREE_PAGE_SIZE))
		node.setHeader(BNODE_LEAF, 4)
		nodeAppendKV(node, 0, 0, []byte(""), []byte("")) // dummy key
		nodeAppendKV(node, 1, 0, []byte("apple"), []byte("red"))
		nodeAppendKV(node, 2, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(node, 3, 0, []byte("cherry"), []byte("red"))

		idx := nodeLookupLE(node, []byte("banana"))
		if idx != 2 {
			t.Errorf("Expected index 2, got %d", idx)
		}
	})

	t.Run("find less than", func(t *testing.T) {
		node := BNode(make([]byte, BTREE_PAGE_SIZE))
		node.setHeader(BNODE_LEAF, 4)
		nodeAppendKV(node, 0, 0, []byte(""), []byte(""))
		nodeAppendKV(node, 1, 0, []byte("apple"), []byte("red"))
		nodeAppendKV(node, 2, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(node, 3, 0, []byte("cherry"), []byte("red"))

		// Search for "blueberry" should return index of "banana"
		idx := nodeLookupLE(node, []byte("blueberry"))
		if idx != 2 {
			t.Errorf("Expected index 2, got %d", idx)
		}
	})

	t.Run("find first key", func(t *testing.T) {
		node := BNode(make([]byte, BTREE_PAGE_SIZE))
		node.setHeader(BNODE_LEAF, 3)
		nodeAppendKV(node, 0, 0, []byte(""), []byte(""))
		nodeAppendKV(node, 1, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(node, 2, 0, []byte("cherry"), []byte("red"))

		// Search for "apple" should return index 0 (dummy key)
		idx := nodeLookupLE(node, []byte("apple"))
		if idx != 0 {
			t.Errorf("Expected index 0, got %d", idx)
		}
	})

	t.Run("find last key", func(t *testing.T) {
		node := BNode(make([]byte, BTREE_PAGE_SIZE))
		node.setHeader(BNODE_LEAF, 4)
		nodeAppendKV(node, 0, 0, []byte(""), []byte(""))
		nodeAppendKV(node, 1, 0, []byte("apple"), []byte("red"))
		nodeAppendKV(node, 2, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(node, 3, 0, []byte("cherry"), []byte("red"))

		// Search for "date" should return index 3 (cherry)
		idx := nodeLookupLE(node, []byte("date"))
		if idx != 3 {
			t.Errorf("Expected index 3, got %d", idx)
		}
	})
}

// Test leafInsert
func TestLeafInsert(t *testing.T) {
	t.Run("insert at beginning", func(t *testing.T) {
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(oldNode, 0, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(oldNode, 1, 0, []byte("cherry"), []byte("red"))

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafInsert(newNode, oldNode, 0, []byte("apple"), []byte("green"))

		if newNode.nkeys() != 3 {
			t.Errorf("Expected 3 keys, got %d", newNode.nkeys())
		}
		if string(newNode.getKey(0)) != "apple" {
			t.Errorf("Expected apple at index 0, got %s", string(newNode.getKey(0)))
		}
		if string(newNode.getKey(1)) != "banana" {
			t.Errorf("Expected banana at index 1, got %s", string(newNode.getKey(1)))
		}
	})

	t.Run("insert in middle", func(t *testing.T) {
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(oldNode, 0, 0, []byte("apple"), []byte("green"))
		nodeAppendKV(oldNode, 1, 0, []byte("cherry"), []byte("red"))

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafInsert(newNode, oldNode, 1, []byte("banana"), []byte("yellow"))

		if newNode.nkeys() != 3 {
			t.Errorf("Expected 3 keys, got %d", newNode.nkeys())
		}
		if string(newNode.getKey(1)) != "banana" {
			t.Errorf("Expected banana at index 1, got %s", string(newNode.getKey(1)))
		}
	})

	t.Run("insert at end", func(t *testing.T) {
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(oldNode, 0, 0, []byte("apple"), []byte("green"))
		nodeAppendKV(oldNode, 1, 0, []byte("banana"), []byte("yellow"))

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafInsert(newNode, oldNode, 2, []byte("cherry"), []byte("red"))

		if newNode.nkeys() != 3 {
			t.Errorf("Expected 3 keys, got %d", newNode.nkeys())
		}
		if string(newNode.getKey(2)) != "cherry" {
			t.Errorf("Expected cherry at index 2, got %s", string(newNode.getKey(2)))
		}
	})
}

// Test leafUpdate
func TestLeafUpdate(t *testing.T) {
	t.Run("update first key", func(t *testing.T) {
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 3)
		nodeAppendKV(oldNode, 0, 0, []byte("apple"), []byte("green"))
		nodeAppendKV(oldNode, 1, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(oldNode, 2, 0, []byte("cherry"), []byte("red"))

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafUpdate(newNode, oldNode, 0, []byte("apple"), []byte("red"))

		if newNode.nkeys() != 3 {
			t.Errorf("Expected 3 keys, got %d", newNode.nkeys())
		}
		if string(newNode.getVal(0)) != "red" {
			t.Errorf("Expected red, got %s", string(newNode.getVal(0)))
		}
		// Other keys should remain unchanged
		if string(newNode.getKey(1)) != "banana" {
			t.Errorf("Expected banana at index 1, got %s", string(newNode.getKey(1)))
		}
	})

	t.Run("update middle key", func(t *testing.T) {
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 3)
		nodeAppendKV(oldNode, 0, 0, []byte("apple"), []byte("green"))
		nodeAppendKV(oldNode, 1, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(oldNode, 2, 0, []byte("cherry"), []byte("red"))

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafUpdate(newNode, oldNode, 1, []byte("banana"), []byte("green"))

		if string(newNode.getVal(1)) != "green" {
			t.Errorf("Expected green, got %s", string(newNode.getVal(1)))
		}
	})
}

// Test leafDelete
func TestLeafDelete(t *testing.T) {
	t.Run("delete first key", func(t *testing.T) {
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 3)
		nodeAppendKV(oldNode, 0, 0, []byte("apple"), []byte("green"))
		nodeAppendKV(oldNode, 1, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(oldNode, 2, 0, []byte("cherry"), []byte("red"))

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafDelete(newNode, oldNode, 0, []byte("apple"))

		if newNode.nkeys() != 2 {
			t.Errorf("Expected 2 keys, got %d", newNode.nkeys())
		}
		if string(newNode.getKey(0)) != "banana" {
			t.Errorf("Expected banana at index 0, got %s", string(newNode.getKey(0)))
		}
		if string(newNode.getKey(1)) != "cherry" {
			t.Errorf("Expected cherry at index 1, got %s", string(newNode.getKey(1)))
		}
	})

	t.Run("delete middle key", func(t *testing.T) {
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 3)
		nodeAppendKV(oldNode, 0, 0, []byte("apple"), []byte("green"))
		nodeAppendKV(oldNode, 1, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(oldNode, 2, 0, []byte("cherry"), []byte("red"))

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafDelete(newNode, oldNode, 1, []byte("banana"))

		if newNode.nkeys() != 2 {
			t.Errorf("Expected 2 keys, got %d", newNode.nkeys())
		}
		if string(newNode.getKey(0)) != "apple" {
			t.Errorf("Expected apple at index 0, got %s", string(newNode.getKey(0)))
		}
		if string(newNode.getKey(1)) != "cherry" {
			t.Errorf("Expected cherry at index 1, got %s", string(newNode.getKey(1)))
		}
	})

	t.Run("delete last key", func(t *testing.T) {
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 3)
		nodeAppendKV(oldNode, 0, 0, []byte("apple"), []byte("green"))
		nodeAppendKV(oldNode, 1, 0, []byte("banana"), []byte("yellow"))
		nodeAppendKV(oldNode, 2, 0, []byte("cherry"), []byte("red"))

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		leafDelete(newNode, oldNode, 2, []byte("cherry"))

		if newNode.nkeys() != 2 {
			t.Errorf("Expected 2 keys, got %d", newNode.nkeys())
		}
		if string(newNode.getKey(0)) != "apple" {
			t.Errorf("Expected apple at index 0, got %s", string(newNode.getKey(0)))
		}
		if string(newNode.getKey(1)) != "banana" {
			t.Errorf("Expected banana at index 1, got %s", string(newNode.getKey(1)))
		}
	})
}

// Test nodeMerge
func TestNodeMerge(t *testing.T) {
	t.Run("merge two leaf nodes", func(t *testing.T) {
		left := BNode(make([]byte, BTREE_PAGE_SIZE))
		left.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(left, 0, 0, []byte("apple"), []byte("green"))
		nodeAppendKV(left, 1, 0, []byte("banana"), []byte("yellow"))

		right := BNode(make([]byte, BTREE_PAGE_SIZE))
		right.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(right, 0, 0, []byte("cherry"), []byte("red"))
		nodeAppendKV(right, 1, 0, []byte("date"), []byte("brown"))

		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, left, right)

		if merged.nkeys() != 4 {
			t.Errorf("Expected 4 keys, got %d", merged.nkeys())
		}
		if merged.btype() != BNODE_LEAF {
			t.Errorf("Expected BNODE_LEAF, got %d", merged.btype())
		}

		expectedKeys := []string{"apple", "banana", "cherry", "date"}
		for i, expected := range expectedKeys {
			if string(merged.getKey(uint16(i))) != expected {
				t.Errorf("Expected key[%d] = %s, got %s", i, expected, string(merged.getKey(uint16(i))))
			}
		}
	})

	t.Run("merge two internal nodes", func(t *testing.T) {
		left := BNode(make([]byte, BTREE_PAGE_SIZE))
		left.setHeader(BNODE_NODE, 2)
		nodeAppendKV(left, 0, 100, []byte("apple"), nil)
		nodeAppendKV(left, 1, 200, []byte("banana"), nil)

		right := BNode(make([]byte, BTREE_PAGE_SIZE))
		right.setHeader(BNODE_NODE, 2)
		nodeAppendKV(right, 0, 300, []byte("cherry"), nil)
		nodeAppendKV(right, 1, 400, []byte("date"), nil)

		merged := BNode(make([]byte, BTREE_PAGE_SIZE))
		nodeMerge(merged, left, right)

		if merged.nkeys() != 4 {
			t.Errorf("Expected 4 keys, got %d", merged.nkeys())
		}
		if merged.btype() != BNODE_NODE {
			t.Errorf("Expected BNODE_NODE, got %d", merged.btype())
		}

		// Check pointers are preserved
		if merged.getPtr(0) != 100 {
			t.Errorf("Expected ptr[0] = 100, got %d", merged.getPtr(0))
		}
		if merged.getPtr(2) != 300 {
			t.Errorf("Expected ptr[2] = 300, got %d", merged.getPtr(2))
		}
	})
}

// Test nodeReplace2Kid
func TestNodeReplace2Kid(t *testing.T) {
	t.Run("replace first two children with one", func(t *testing.T) {
		// Create a parent with 3 children
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_NODE, 3)
		nodeAppendKV(oldNode, 0, 100, []byte("apple"), nil)
		nodeAppendKV(oldNode, 1, 200, []byte("banana"), nil)
		nodeAppendKV(oldNode, 2, 300, []byte("cherry"), nil)

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		// Replace children at idx 0 and 1 with a merged one
		nodeReplace2Kid(newNode, oldNode, 0, 150, []byte("avocado"))

		// Should have 2 keys now (3 - 1 = 2)
		if newNode.nkeys() != 2 {
			t.Errorf("Expected 2 keys, got %d", newNode.nkeys())
		}

		// Check keys
		if string(newNode.getKey(0)) != "avocado" {
			t.Errorf("Expected avocado at index 0, got %s", string(newNode.getKey(0)))
		}
		if string(newNode.getKey(1)) != "cherry" {
			t.Errorf("Expected cherry at index 1, got %s", string(newNode.getKey(1)))
		}

		// Check pointers
		if newNode.getPtr(0) != 150 {
			t.Errorf("Expected ptr[0] = 150, got %d", newNode.getPtr(0))
		}
		if newNode.getPtr(1) != 300 {
			t.Errorf("Expected ptr[1] = 300, got %d", newNode.getPtr(1))
		}
	})

	t.Run("replace middle two children", func(t *testing.T) {
		// Create a parent with 4 children
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_NODE, 4)
		nodeAppendKV(oldNode, 0, 100, []byte("apple"), nil)
		nodeAppendKV(oldNode, 1, 200, []byte("banana"), nil)
		nodeAppendKV(oldNode, 2, 300, []byte("cherry"), nil)
		nodeAppendKV(oldNode, 3, 400, []byte("date"), nil)

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		// Replace children at idx 1 and 2 with a merged one
		nodeReplace2Kid(newNode, oldNode, 1, 250, []byte("blueberry"))

		// Should have 3 keys now (4 - 1 = 3)
		if newNode.nkeys() != 3 {
			t.Errorf("Expected 3 keys, got %d", newNode.nkeys())
		}

		// Check keys
		if string(newNode.getKey(0)) != "apple" {
			t.Errorf("Expected apple at index 0, got %s", string(newNode.getKey(0)))
		}
		if string(newNode.getKey(1)) != "blueberry" {
			t.Errorf("Expected blueberry at index 1, got %s", string(newNode.getKey(1)))
		}
		if string(newNode.getKey(2)) != "date" {
			t.Errorf("Expected date at index 2, got %s", string(newNode.getKey(2)))
		}

		// Check pointers
		if newNode.getPtr(0) != 100 {
			t.Errorf("Expected ptr[0] = 100, got %d", newNode.getPtr(0))
		}
		if newNode.getPtr(1) != 250 {
			t.Errorf("Expected ptr[1] = 250, got %d", newNode.getPtr(1))
		}
		if newNode.getPtr(2) != 400 {
			t.Errorf("Expected ptr[2] = 400, got %d", newNode.getPtr(2))
		}
	})

	t.Run("replace last two children", func(t *testing.T) {
		// Create a parent with 4 children
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_NODE, 4)
		nodeAppendKV(oldNode, 0, 100, []byte("apple"), nil)
		nodeAppendKV(oldNode, 1, 200, []byte("banana"), nil)
		nodeAppendKV(oldNode, 2, 300, []byte("cherry"), nil)
		nodeAppendKV(oldNode, 3, 400, []byte("date"), nil)

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		// Replace children at idx 2 and 3 with a merged one
		nodeReplace2Kid(newNode, oldNode, 2, 350, []byte("cranberry"))

		// Should have 3 keys now (4 - 1 = 3)
		if newNode.nkeys() != 3 {
			t.Errorf("Expected 3 keys, got %d", newNode.nkeys())
		}

		// Check keys
		if string(newNode.getKey(0)) != "apple" {
			t.Errorf("Expected apple at index 0, got %s", string(newNode.getKey(0)))
		}
		if string(newNode.getKey(1)) != "banana" {
			t.Errorf("Expected banana at index 1, got %s", string(newNode.getKey(1)))
		}
		if string(newNode.getKey(2)) != "cranberry" {
			t.Errorf("Expected cranberry at index 2, got %s", string(newNode.getKey(2)))
		}

		// Check pointers
		if newNode.getPtr(0) != 100 {
			t.Errorf("Expected ptr[0] = 100, got %d", newNode.getPtr(0))
		}
		if newNode.getPtr(1) != 200 {
			t.Errorf("Expected ptr[1] = 200, got %d", newNode.getPtr(1))
		}
		if newNode.getPtr(2) != 350 {
			t.Errorf("Expected ptr[2] = 350, got %d", newNode.getPtr(2))
		}
	})

	t.Run("replace only two children in node", func(t *testing.T) {
		// Create a parent with exactly 2 children
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_NODE, 2)
		nodeAppendKV(oldNode, 0, 100, []byte("apple"), nil)
		nodeAppendKV(oldNode, 1, 200, []byte("banana"), nil)

		newNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		// Replace children at idx 0 and 1 with a merged one
		nodeReplace2Kid(newNode, oldNode, 0, 150, []byte("avocado"))

		// Should have 1 key now (2 - 1 = 1)
		if newNode.nkeys() != 1 {
			t.Errorf("Expected 1 key, got %d", newNode.nkeys())
		}

		// Check key
		if string(newNode.getKey(0)) != "avocado" {
			t.Errorf("Expected avocado at index 0, got %s", string(newNode.getKey(0)))
		}

		// Check pointer
		if newNode.getPtr(0) != 150 {
			t.Errorf("Expected ptr[0] = 150, got %d", newNode.getPtr(0))
		}
	})
}

// Test shouldMerge
func TestShouldMerge(t *testing.T) {
	c := newC()

	t.Run("no merge - node too large", func(t *testing.T) {
		// Create a node that's larger than BTREE_PAGE_SIZE/4
		updated := BNode(make([]byte, BTREE_PAGE_SIZE))
		updated.setHeader(BNODE_LEAF, 10)
		for i := uint16(0); i < 10; i++ {
			nodeAppendKV(updated, i, 0, []byte(strings.Repeat("k", 50)), []byte(strings.Repeat("v", 50)))
		}

		parent := BNode(make([]byte, BTREE_PAGE_SIZE))
		parent.setHeader(BNODE_NODE, 3)
		nodeAppendKV(parent, 0, c.tree.new(BNode(make([]byte, BTREE_PAGE_SIZE))), []byte("a"), nil)
		nodeAppendKV(parent, 1, c.tree.new(updated), []byte("b"), nil)
		nodeAppendKV(parent, 2, c.tree.new(BNode(make([]byte, BTREE_PAGE_SIZE))), []byte("c"), nil)

		mergeDir, _ := shouldMerge(&c.tree, parent, 1, updated)
		if mergeDir != 0 {
			t.Errorf("Expected no merge (0), got %d", mergeDir)
		}
	})

	t.Run("merge with left sibling", func(t *testing.T) {
		// Create a small updated node
		updated := BNode(make([]byte, BTREE_PAGE_SIZE))
		updated.setHeader(BNODE_LEAF, 1)
		nodeAppendKV(updated, 0, 0, []byte("b"), []byte("val"))

		// Create a small left sibling
		leftSibling := BNode(make([]byte, BTREE_PAGE_SIZE))
		leftSibling.setHeader(BNODE_LEAF, 1)
		nodeAppendKV(leftSibling, 0, 0, []byte("a"), []byte("val"))

		parent := BNode(make([]byte, BTREE_PAGE_SIZE))
		parent.setHeader(BNODE_NODE, 2)
		nodeAppendKV(parent, 0, c.tree.new(leftSibling), []byte("a"), nil)
		nodeAppendKV(parent, 1, c.tree.new(updated), []byte("b"), nil)

		mergeDir, sibling := shouldMerge(&c.tree, parent, 1, updated)
		if mergeDir != -1 {
			t.Errorf("Expected merge with left (-1), got %d", mergeDir)
		}
		if sibling.nkeys() != 1 {
			t.Errorf("Expected sibling with 1 key, got %d", sibling.nkeys())
		}
	})

	t.Run("merge with right sibling", func(t *testing.T) {
		// Create a small updated node
		updated := BNode(make([]byte, BTREE_PAGE_SIZE))
		updated.setHeader(BNODE_LEAF, 1)
		nodeAppendKV(updated, 0, 0, []byte("a"), []byte("val"))

		// Create a small right sibling
		rightSibling := BNode(make([]byte, BTREE_PAGE_SIZE))
		rightSibling.setHeader(BNODE_LEAF, 1)
		nodeAppendKV(rightSibling, 0, 0, []byte("b"), []byte("val"))

		parent := BNode(make([]byte, BTREE_PAGE_SIZE))
		parent.setHeader(BNODE_NODE, 2)
		nodeAppendKV(parent, 0, c.tree.new(updated), []byte("a"), nil)
		nodeAppendKV(parent, 1, c.tree.new(rightSibling), []byte("b"), nil)

		mergeDir, sibling := shouldMerge(&c.tree, parent, 0, updated)
		if mergeDir != 1 {
			t.Errorf("Expected merge with right (1), got %d", mergeDir)
		}
		if sibling.nkeys() != 1 {
			t.Errorf("Expected sibling with 1 key, got %d", sibling.nkeys())
		}
	})
}

// Test BTree Insert
func TestBTreeInsert(t *testing.T) {
	t.Run("insert into empty tree", func(t *testing.T) {
		c := newC()

		c.tree.Insert([]byte("apple"), []byte("red"))

		if c.tree.root == 0 {
			t.Error("Expected root to be set")
		}

		root := BNode(c.tree.get(c.tree.root))
		if root.nkeys() != 2 { // dummy key + actual key
			t.Errorf("Expected 2 keys, got %d", root.nkeys())
		}
		if string(root.getKey(1)) != "apple" {
			t.Errorf("Expected apple, got %s", string(root.getKey(1)))
		}
		if string(root.getVal(1)) != "red" {
			t.Errorf("Expected red, got %s", string(root.getVal(1)))
		}
	})

	t.Run("insert multiple keys", func(t *testing.T) {
		c := newC()

		c.add("banana", "yellow")
		c.add("apple", "red")
		c.add("cherry", "red")

		root := BNode(c.tree.get(c.tree.root))
		// Should have at least 3 keys + dummy
		if root.nkeys() < 3 {
			t.Errorf("Expected at least 3 keys, got %d", root.nkeys())
		}
	})

	t.Run("update existing key", func(t *testing.T) {
		c := newC()

		c.tree.Insert([]byte("apple"), []byte("green"))
		c.tree.Insert([]byte("apple"), []byte("red"))

		root := BNode(c.tree.get(c.tree.root))
		// Find the apple key
		found := false
		for i := uint16(0); i < root.nkeys(); i++ {
			if string(root.getKey(i)) == "apple" {
				if string(root.getVal(i)) != "red" {
					t.Errorf("Expected red, got %s", string(root.getVal(i)))
				}
				found = true
				break
			}
		}
		if !found {
			t.Error("Key 'apple' not found")
		}
	})

	t.Run("insert many keys to trigger split", func(t *testing.T) {
		c := newC()

		// Insert enough keys to trigger a split
		for i := 0; i < 100; i++ {
			key := []byte(strings.Repeat("k", 10) + string(rune(i)))
			val := []byte(strings.Repeat("v", 10))
			c.tree.Insert(key, val)
		}

		// Tree should still be valid
		if c.tree.root == 0 {
			t.Error("Expected root to be set")
		}
	})
}

// Test treeInsert
func TestTreeInsert(t *testing.T) {
	t.Run("insert into leaf node", func(t *testing.T) {
		c := newC()

		// Create a simple leaf node
		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(oldNode, 0, 0, []byte(""), []byte(""))
		nodeAppendKV(oldNode, 1, 0, []byte("apple"), []byte("red"))

		newNode := treeInsert(&c.tree, oldNode, []byte("banana"), []byte("yellow"))

		if newNode.nkeys() != 3 {
			t.Errorf("Expected 3 keys, got %d", newNode.nkeys())
		}
	})

	t.Run("update existing key in leaf", func(t *testing.T) {
		c := newC()

		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(oldNode, 0, 0, []byte(""), []byte(""))
		nodeAppendKV(oldNode, 1, 0, []byte("apple"), []byte("green"))

		newNode := treeInsert(&c.tree, oldNode, []byte("apple"), []byte("red"))

		if newNode.nkeys() != 2 {
			t.Errorf("Expected 2 keys, got %d", newNode.nkeys())
		}
		if string(newNode.getVal(1)) != "red" {
			t.Errorf("Expected red, got %s", string(newNode.getVal(1)))
		}
	})
}

// Test treeDelete
func TestTreeDelete(t *testing.T) {
	t.Run("delete from leaf node", func(t *testing.T) {
		c := newC()

		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 3)
		nodeAppendKV(oldNode, 0, 0, []byte(""), []byte(""))
		nodeAppendKV(oldNode, 1, 0, []byte("apple"), []byte("red"))
		nodeAppendKV(oldNode, 2, 0, []byte("banana"), []byte("yellow"))

		newNode := treeDelete(&c.tree, oldNode, []byte("apple"))

		if newNode.nkeys() != 2 {
			t.Errorf("Expected 2 keys, got %d", newNode.nkeys())
		}
		if string(newNode.getKey(1)) != "banana" {
			t.Errorf("Expected banana, got %s", string(newNode.getKey(1)))
		}
	})

	t.Run("delete non-existent key", func(t *testing.T) {
		c := newC()

		oldNode := BNode(make([]byte, BTREE_PAGE_SIZE))
		oldNode.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(oldNode, 0, 0, []byte(""), []byte(""))
		nodeAppendKV(oldNode, 1, 0, []byte("apple"), []byte("red"))

		newNode := treeDelete(&c.tree, oldNode, []byte("banana"))

		if len(newNode) != 0 {
			t.Error("Expected empty node for non-existent key")
		}
	})
}

// Test nodeInsert
func TestNodeInsert(t *testing.T) {
	t.Run("insert into internal node", func(t *testing.T) {
		c := newC()

		// Create a child leaf node
		child := BNode(make([]byte, BTREE_PAGE_SIZE))
		child.setHeader(BNODE_LEAF, 2)
		nodeAppendKV(child, 0, 0, []byte(""), []byte(""))
		nodeAppendKV(child, 1, 0, []byte("apple"), []byte("red"))
		childPtr := c.tree.new(child)

		// Create parent internal node
		parent := BNode(make([]byte, BTREE_PAGE_SIZE))
		parent.setHeader(BNODE_NODE, 1)
		nodeAppendKV(parent, 0, childPtr, []byte(""), nil)

		// Insert into the internal node
		newParent := BNode(make([]byte, 2*BTREE_PAGE_SIZE))
		nodeInsert(&c.tree, newParent, parent, 0, []byte("banana"), []byte("yellow"))

		// The parent should be updated
		if newParent.nkeys() == 0 {
			t.Error("Expected parent to have keys")
		}
	})
}

// Integration tests
func TestBTreeIntegration(t *testing.T) {
	t.Run("insert and verify order", func(t *testing.T) {
		c := newC()

		keys := []string{"dog", "cat", "bird", "ant", "elephant"}
		for _, key := range keys {
			c.add(key, "value_"+key)
		}

		// Verify all keys are in the reference map
		for _, key := range keys {
			if val, ok := c.ref[key]; !ok {
				t.Errorf("Key %s not found in reference", key)
			} else if val != "value_"+key {
				t.Errorf("Expected value_%s, got %s", key, val)
			}
		}
	})

	t.Run("insert duplicate keys", func(t *testing.T) {
		c := newC()

		c.add("apple", "green")
		c.add("apple", "red")
		c.add("apple", "yellow")

		if c.ref["apple"] != "yellow" {
			t.Errorf("Expected yellow, got %s", c.ref["apple"])
		}
	})

	t.Run("insert with empty values", func(t *testing.T) {
		c := newC()

		c.tree.Insert([]byte("key1"), []byte(""))
		c.tree.Insert([]byte("key2"), []byte("value"))

		root := BNode(c.tree.get(c.tree.root))
		if root.nkeys() < 2 {
			t.Error("Expected at least 2 keys")
		}
	})

	t.Run("stress test - many insertions", func(t *testing.T) {
		c := newC()

		// Insert 1000 keys
		for i := 0; i < 1000; i++ {
			key := []byte(strings.Repeat("k", 5) + string(rune(i%256)) + string(rune(i/256)))
			val := []byte(strings.Repeat("v", 5))
			c.tree.Insert(key, val)
		}

		// Tree should still be valid
		if c.tree.root == 0 {
			t.Error("Expected root to be set")
		}

		// All pages should be valid size
		for _, page := range c.pages {
			if page.nbytes() > BTREE_PAGE_SIZE {
				t.Errorf("Page too large: %d bytes", page.nbytes())
			}
		}
	})
}

// Test edge cases
func TestBTreeEdgeCases(t *testing.T) {
	t.Run("insert max size key and value", func(t *testing.T) {
		c := newC()

		key := []byte(strings.Repeat("k", BTREE_MAX_KEY_SIZE))
		val := []byte(strings.Repeat("v", BTREE_MAX_VAL_SIZE))

		c.tree.Insert(key, val)

		if c.tree.root == 0 {
			t.Error("Expected root to be set")
		}
	})

	t.Run("empty key", func(t *testing.T) {
		c := newC()

		c.tree.Insert([]byte(""), []byte("empty_key_value"))

		if c.tree.root == 0 {
			t.Error("Expected root to be set")
		}
	})

	t.Run("single character keys", func(t *testing.T) {
		c := newC()

		for i := 'a'; i <= 'z'; i++ {
			c.add(string(i), "val_"+string(i))
		}

		if len(c.ref) != 26 {
			t.Errorf("Expected 26 keys, got %d", len(c.ref))
		}
	})
}

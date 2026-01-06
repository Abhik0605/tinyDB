package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestKVBasicOperations(t *testing.T) {
	// Create a temporary directory for the test database
	tmpDir, err := os.MkdirTemp("", "kv_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create and open the database
	db := &KV{Path: dbPath}
	if err := db.Open(); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Test Set and Get
	key1 := []byte("hello")
	val1 := []byte("world")

	if err := db.Set(key1, val1); err != nil {
		t.Fatalf("Failed to set key: %v", err)
	}

	gotVal, found := db.Get(key1)
	if !found {
		t.Fatalf("Key not found after Set")
	}
	if string(gotVal) != string(val1) {
		t.Fatalf("Expected value %q, got %q", val1, gotVal)
	}

	// Test updating an existing key
	val2 := []byte("updated")
	if err := db.Set(key1, val2); err != nil {
		t.Fatalf("Failed to update key: %v", err)
	}

	gotVal, found = db.Get(key1)
	if !found {
		t.Fatalf("Key not found after update")
	}
	if string(gotVal) != string(val2) {
		t.Fatalf("Expected value %q, got %q", val2, gotVal)
	}

	// Test Delete
	deleted, err := db.Del(key1)
	if err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}
	if !deleted {
		t.Fatalf("Delete returned false for existing key")
	}

	_, found = db.Get(key1)
	if found {
		t.Fatalf("Key still found after delete")
	}

	// Test deleting non-existent key
	deleted, err = db.Del([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("Failed to delete non-existent key: %v", err)
	}
	if deleted {
		t.Fatalf("Delete returned true for non-existent key")
	}

	// Close the database
	if err := db.Close(); err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}
}

func TestKVPersistence(t *testing.T) {
	// Create a temporary directory for the test database
	tmpDir, err := os.MkdirTemp("", "kv_test_persistence")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// First session: write data
	db := &KV{Path: dbPath}
	if err := db.Open(); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		val := []byte(fmt.Sprintf("value%d", i))
		if err := db.Set(key, val); err != nil {
			t.Fatalf("Failed to set key %d: %v", i, err)
		}
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}

	// Second session: read data back
	db2 := &KV{Path: dbPath}
	if err := db2.Open(); err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}

	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		expectedVal := []byte(fmt.Sprintf("value%d", i))
		gotVal, found := db2.Get(key)
		if !found {
			t.Fatalf("Key %d not found after reopening", i)
		}
		if string(gotVal) != string(expectedVal) {
			t.Fatalf("Expected value %q for key %d, got %q", expectedVal, i, gotVal)
		}
	}

	if err := db2.Close(); err != nil {
		t.Fatalf("Failed to close database: %v", err)
	}
}

func TestKVMultipleOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kv_test_multi")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	db := &KV{Path: dbPath}
	if err := db.Open(); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Insert 1000 keys to trigger B-tree splits
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key%05d", i))
		val := []byte(fmt.Sprintf("value%05d", i))
		if err := db.Set(key, val); err != nil {
			t.Fatalf("Failed to set key %d: %v", i, err)
		}
	}
}

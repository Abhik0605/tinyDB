package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

const (
	TYPE_BYTES = 1 // string (of arbitrary bytes)
	TYPE_INT64 = 2 // integer; 64-bit signed
)

// table cell
type Value struct {
	Type uint32 // tagged union
	I64  int64
	Str  []byte
}

// table row
type Record struct {
	Cols []string
	Vals []Value
}

func (rec *Record) AddStr(col string, val []byte) *Record {
	rec.Cols = append(rec.Cols, col)
	rec.Vals = append(rec.Vals, Value{Type: TYPE_BYTES, Str: val})
	return rec
}

func (rec *Record) AddInt64(col string, val int64) *Record {
	rec.Cols = append(rec.Cols, col)
	rec.Vals = append(rec.Vals, Value{Type: TYPE_INT64, I64: val})
	return rec
}

func (rec *Record) Get(col string) *Value {
	for i, c := range rec.Cols {
		if c == col {
			return &rec.Vals[i]
		}
	}
	return nil
}

type TableDef struct {
	// user defined
	Name  string
	Types []uint32 // column types
	Cols  []string // column names
	PKeys int      // the first `PKeys` columns are the primary key
	// auto-assigned B-tree key prefixes for different tables
	Prefix uint32
}

var TDEF_TABLE = &TableDef{
	Prefix: 2,
	Name:   "@table",
	Types:  []uint32{TYPE_BYTES, TYPE_BYTES},
	Cols:   []string{"name", "def"},
	PKeys:  1,
}

var TDEF_META = &TableDef{
	Prefix: 1,
	Name:   "@meta",
	Types:  []uint32{TYPE_BYTES, TYPE_BYTES},
	Cols:   []string{"key", "val"},
	PKeys:  1,
}

type DB struct {
	Path string
	kv   KV
}

func assert(cond bool) {
	if !cond {
		panic("assertion failed")
	}
}

// Open opens the database
func (db *DB) Open() error {
	db.kv.Path = db.Path
	return db.kv.Open()
}

// Close closes the database
func (db *DB) Close() error {
	return db.kv.Close()
}

// get a single row by the primary key
func dbGet(db *DB, tdef *TableDef, rec *Record) (bool, error) {
	// 1. reorder the input columns according to the schema
	values, err := checkRecord(tdef, *rec, tdef.PKeys)
	if err != nil {
		return false, err
	}
	// 2. encode the primary key
	key := encodeKey(nil, tdef.Prefix, values[:tdef.PKeys])
	// 3. query the KV store
	val, ok := db.kv.Get(key)
	if !ok {
		return false, nil
	}
	// 4. decode the value into columns
	for i := tdef.PKeys; i < len(tdef.Cols); i++ {
		values[i].Type = tdef.Types[i]
	}
	decodeValues(val, values[tdef.PKeys:])
	rec.Cols = tdef.Cols
	rec.Vals = values
	return true, nil
}

// reorder a record and check for missing columns.
// n == tdef.PKeys: record is exactly a primary key
// n == len(tdef.Cols): record contains all columns
func checkRecord(tdef *TableDef, rec Record, n int) ([]Value, error) {
	values := make([]Value, len(tdef.Cols))
	for i, col := range rec.Cols {
		idx := -1
		for j, c := range tdef.Cols {
			if c == col {
				idx = j
				break
			}
		}
		if idx == -1 {
			return nil, fmt.Errorf("no such column: %s", col)
		}
		if idx >= n {
			return nil, fmt.Errorf("extra column: %s", col)
		}
		values[idx] = rec.Vals[i]
	}
	for i := 0; i < n; i++ {
		if values[i].Type == 0 {
			return nil, fmt.Errorf("missing column: %s", tdef.Cols[i])
		}
	}
	return values, nil
}

// encode a list of values into a key
// encode columns for the "key" of the KV
func encodeKey(out []byte, prefix uint32, vals []Value) []byte {
	out = append(out, byte(prefix))
	for _, v := range vals {
		out = append(out, byte(v.Type))
		switch v.Type {
		case TYPE_BYTES:
			// length-prefixed string for key encoding
			var buf [2]byte
			binary.LittleEndian.PutUint16(buf[:], uint16(len(v.Str)))
			out = append(out, buf[:]...)
			out = append(out, v.Str...)
		case TYPE_INT64:
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], uint64(v.I64))
			out = append(out, buf[:]...)
		}
	}
	return out
}

// decode a list of values from a key
func decodeKey(out []Value, key []byte) []Value {
	_ = key[0] // skip prefix
	for i := 1; i < len(key); {
		t := uint32(key[i])
		i++
		switch t {
		case TYPE_BYTES:
			klen := binary.LittleEndian.Uint16(key[i:])
			i += 2
			out = append(out, Value{Type: t, Str: key[i : i+int(klen)]})
			i += int(klen)
		case TYPE_INT64:
			out = append(out, Value{Type: t, I64: int64(binary.LittleEndian.Uint64(key[i:]))})
			i += 8
		}
	}
	return out
}

// encode values for storage (non-key columns)
func encodeValues(out []byte, vals []Value) []byte {
	for _, v := range vals {
		switch v.Type {
		case TYPE_BYTES:
			var buf [2]byte
			binary.LittleEndian.PutUint16(buf[:], uint16(len(v.Str)))
			out = append(out, buf[:]...)
			out = append(out, v.Str...)
		case TYPE_INT64:
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], uint64(v.I64))
			out = append(out, buf[:]...)
		}
	}
	return out
}

// decode values from storage (non-key columns)
func decodeValues(data []byte, vals []Value) {
	pos := 0
	for i := range vals {
		switch vals[i].Type {
		case TYPE_BYTES:
			slen := binary.LittleEndian.Uint16(data[pos:])
			pos += 2
			vals[i].Str = data[pos : pos+int(slen)]
			pos += int(slen)
		case TYPE_INT64:
			vals[i].I64 = int64(binary.LittleEndian.Uint64(data[pos:]))
			pos += 8
		}
	}
}

// get a single row by the primary key
func (db *DB) Get(table string, rec *Record) (bool, error) {
	tdef := getTableDef(db, table)
	if tdef == nil {
		return false, fmt.Errorf("table not found: %s", table)
	}
	return dbGet(db, tdef, rec)
}
func getTableDef(db *DB, name string) *TableDef {
	rec := (&Record{}).AddStr("name", []byte(name))
	ok, err := dbGet(db, TDEF_TABLE, rec)
	assert(err == nil)
	if !ok {
		return nil
	}
	tdef := &TableDef{}
	err = json.Unmarshal(rec.Get("def").Str, tdef)
	assert(err == nil)
	return tdef
}

// update modes
const (
	MODE_UPSERT      = 0 // insert or replace
	MODE_UPDATE_ONLY = 1 // update existing keys
	MODE_INSERT_ONLY = 2 // only add new keys
)

func dbUpdate(db *DB, tdef *TableDef, rec Record, mode int) (bool, error) {
	values, err := checkRecord(tdef, rec, len(tdef.Cols))
	if err != nil {
		return false, err
	}
	key := encodeKey(nil, tdef.Prefix, values[:tdef.PKeys])
	val := encodeValues(nil, values[tdef.PKeys:])
	return db.kv.Update(key, val, mode)
}

// TableNew creates a new table definition
func (db *DB) TableNew(tdef *TableDef) error {
	// Assign a new prefix for the table
	tdef.Prefix = db.getNextPrefix()

	defBytes, err := json.Marshal(tdef)
	if err != nil {
		return fmt.Errorf("failed to marshal table def: %w", err)
	}

	_, err = dbUpdate(db, TDEF_TABLE, Record{
		Cols: []string{"name", "def"},
		Vals: []Value{
			{Type: TYPE_BYTES, Str: []byte(tdef.Name)},
			{Type: TYPE_BYTES, Str: defBytes},
		},
	}, MODE_INSERT_ONLY)
	return err
}

// getNextPrefix returns the next available table prefix
func (db *DB) getNextPrefix() uint32 {
	// Start from 3 since 1 and 2 are reserved for meta tables
	// In a real implementation, we would track this in the meta table
	return 3
}

// Insert inserts a new row into the table
func (db *DB) Insert(table string, rec Record) (bool, error) {
	tdef := getTableDef(db, table)
	if tdef == nil {
		return false, fmt.Errorf("table not found: %s", table)
	}
	return dbUpdate(db, tdef, rec, MODE_INSERT_ONLY)
}

// Update updates an existing row in the table
func (db *DB) Update(table string, rec Record) (bool, error) {
	tdef := getTableDef(db, table)
	if tdef == nil {
		return false, fmt.Errorf("table not found: %s", table)
	}
	return dbUpdate(db, tdef, rec, MODE_UPDATE_ONLY)
}

// Upsert inserts or updates a row in the table
func (db *DB) Upsert(table string, rec Record) (bool, error) {
	tdef := getTableDef(db, table)
	if tdef == nil {
		return false, fmt.Errorf("table not found: %s", table)
	}
	return dbUpdate(db, tdef, rec, MODE_UPSERT)
}

// Delete deletes a row from the table by primary key
func (db *DB) Delete(table string, rec Record) (bool, error) {
	tdef := getTableDef(db, table)
	if tdef == nil {
		return false, fmt.Errorf("table not found: %s", table)
	}
	values, err := checkRecord(tdef, rec, tdef.PKeys)
	if err != nil {
		return false, err
	}
	key := encodeKey(nil, tdef.Prefix, values[:tdef.PKeys])
	return db.kv.Del(key)
}

// GetTableDef returns the table definition for the given table name (exported)
func (db *DB) GetTableDef(name string) *TableDef {
	return getTableDef(db, name)
}

// Scanner provides iteration over table rows
type Scanner struct {
	db    *DB
	tdef  *TableDef
	rows  []Record
	index int
}

// NewScanner creates a new scanner for the given table
func (db *DB) NewScanner(table string) (*Scanner, error) {
	tdef := getTableDef(db, table)
	if tdef == nil {
		return nil, fmt.Errorf("table not found: %s", table)
	}

	sc := &Scanner{
		db:    db,
		tdef:  tdef,
		rows:  make([]Record, 0),
		index: 0,
	}

	// Collect all rows with this table's prefix
	prefix := []byte{byte(tdef.Prefix)}
	db.kv.ForEachWithPrefix(prefix, func(key, val []byte) bool {
		rec := sc.decodeRow(key, val)
		sc.rows = append(sc.rows, rec)
		return true
	})

	return sc, nil
}

// decodeRow decodes a key-value pair into a Record
func (sc *Scanner) decodeRow(key, val []byte) Record {
	rec := Record{
		Cols: make([]string, len(sc.tdef.Cols)),
		Vals: make([]Value, len(sc.tdef.Cols)),
	}
	copy(rec.Cols, sc.tdef.Cols)

	// Set up types
	for i := range rec.Vals {
		rec.Vals[i].Type = sc.tdef.Types[i]
	}

	// Decode primary key columns from key
	pos := 1 // skip prefix byte
	for i := 0; i < sc.tdef.PKeys; i++ {
		t := uint32(key[pos])
		pos++
		switch t {
		case TYPE_BYTES:
			slen := binary.LittleEndian.Uint16(key[pos:])
			pos += 2
			rec.Vals[i].Str = key[pos : pos+int(slen)]
			pos += int(slen)
		case TYPE_INT64:
			rec.Vals[i].I64 = int64(binary.LittleEndian.Uint64(key[pos:]))
			pos += 8
		}
	}

	// Decode non-key columns from value
	decodeValues(val, rec.Vals[sc.tdef.PKeys:])

	return rec
}

// Valid returns true if the scanner is pointing to a valid row
func (sc *Scanner) Valid() bool {
	return sc.index < len(sc.rows)
}

// Next advances the scanner to the next row
func (sc *Scanner) Next() {
	sc.index++
}

// Row returns the current row
func (sc *Scanner) Row() Record {
	if sc.index < len(sc.rows) {
		return sc.rows[sc.index]
	}
	return Record{}
}

// Reset resets the scanner to the beginning
func (sc *Scanner) Reset() {
	sc.index = 0
}

// Count returns the total number of rows
func (sc *Scanner) Count() int {
	return len(sc.rows)
}

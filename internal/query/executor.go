package query

import (
	"bytes"
	"fmt"

	"tinydb-go/internal/storage"
)

// Result represents the result of a query execution
type Result struct {
	Columns      []string
	Rows         [][]interface{}
	RowsAffected int
	Message      string
}

// Executor executes SQL statements against the database
type Executor struct {
	db *storage.DB
}

// NewExecutor creates a new query executor
func NewExecutor(db *storage.DB) *Executor {
	return &Executor{db: db}
}

// Execute parses and executes a SQL statement
func (e *Executor) Execute(sql string) (*Result, error) {
	stmt, err := ParseSQL(sql)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	switch s := stmt.(type) {
	case *SelectStmt:
		return e.executeSelect(s)
	case *InsertStmt:
		return e.executeInsert(s)
	case *UpdateStmt:
		return e.executeUpdate(s)
	case *DeleteStmt:
		return e.executeDelete(s)
	case *CreateTableStmt:
		return e.executeCreateTable(s)
	default:
		return nil, fmt.Errorf("unknown statement type")
	}
}

func (e *Executor) executeSelect(stmt *SelectStmt) (*Result, error) {
	tdef := e.db.GetTableDef(stmt.Table)
	if tdef == nil {
		return nil, fmt.Errorf("table not found: %s", stmt.Table)
	}

	scanner, err := e.db.NewScanner(stmt.Table)
	if err != nil {
		return nil, err
	}

	// Determine columns to return
	var columns []string
	if len(stmt.Columns) == 1 && stmt.Columns[0] == "*" {
		columns = tdef.Cols
	} else {
		columns = stmt.Columns
	}

	result := &Result{Columns: columns, Rows: [][]interface{}{}}

	count := 0
	for scanner.Valid() {
		rec := scanner.Row()

		// Apply WHERE filter
		if stmt.Where != nil {
			match, err := e.evaluateWhere(stmt.Where, rec, tdef)
			if err != nil {
				return nil, err
			}
			if !match {
				scanner.Next()
				continue
			}
		}

		// Apply LIMIT
		if stmt.Limit > 0 && count >= stmt.Limit {
			break
		}

		// Build row
		row := make([]interface{}, len(columns))
		for i, col := range columns {
			val := rec.Get(col)
			if val != nil {
				if val.Type == storage.TYPE_BYTES {
					row[i] = string(val.Str)
				} else {
					row[i] = val.I64
				}
			}
		}
		result.Rows = append(result.Rows, row)
		count++
		scanner.Next()
	}

	return result, nil
}

func (e *Executor) executeInsert(stmt *InsertStmt) (*Result, error) {
	tdef := e.db.GetTableDef(stmt.Table)
	if tdef == nil {
		return nil, fmt.Errorf("table not found: %s", stmt.Table)
	}

	if len(stmt.Columns) != len(stmt.Values) {
		return nil, fmt.Errorf("column count doesn't match value count")
	}

	rec := storage.Record{Cols: stmt.Columns, Vals: make([]storage.Value, len(stmt.Values))}
	for i, v := range stmt.Values {
		switch val := v.(type) {
		case string:
			rec.Vals[i] = storage.Value{Type: storage.TYPE_BYTES, Str: []byte(val)}
		case int64:
			rec.Vals[i] = storage.Value{Type: storage.TYPE_INT64, I64: val}
		default:
			return nil, fmt.Errorf("unsupported value type")
		}
	}

	added, err := e.db.Insert(stmt.Table, rec)
	if err != nil {
		return nil, err
	}

	if !added {
		return &Result{Message: "Row already exists", RowsAffected: 0}, nil
	}
	return &Result{Message: "1 row inserted", RowsAffected: 1}, nil
}

func (e *Executor) executeUpdate(stmt *UpdateStmt) (*Result, error) {
	tdef := e.db.GetTableDef(stmt.Table)
	if tdef == nil {
		return nil, fmt.Errorf("table not found: %s", stmt.Table)
	}

	scanner, err := e.db.NewScanner(stmt.Table)
	if err != nil {
		return nil, err
	}

	rowsAffected := 0
	for scanner.Valid() {
		rec := scanner.Row()

		// Apply WHERE filter
		if stmt.Where != nil {
			match, err := e.evaluateWhere(stmt.Where, rec, tdef)
			if err != nil {
				return nil, err
			}
			if !match {
				scanner.Next()
				continue
			}
		}

		// Apply updates
		for col, val := range stmt.Sets {
			for i, c := range rec.Cols {
				if c == col {
					switch v := val.(type) {
					case string:
						rec.Vals[i] = storage.Value{Type: storage.TYPE_BYTES, Str: []byte(v)}
					case int64:
						rec.Vals[i] = storage.Value{Type: storage.TYPE_INT64, I64: v}
					}
					break
				}
			}
		}

		// Upsert the updated record
		_, err := e.db.Upsert(stmt.Table, rec)
		if err != nil {
			return nil, err
		}
		rowsAffected++
		scanner.Next()
	}

	return &Result{
		Message:      fmt.Sprintf("%d row(s) updated", rowsAffected),
		RowsAffected: rowsAffected,
	}, nil
}

func (e *Executor) executeDelete(stmt *DeleteStmt) (*Result, error) {
	tdef := e.db.GetTableDef(stmt.Table)
	if tdef == nil {
		return nil, fmt.Errorf("table not found: %s", stmt.Table)
	}

	scanner, err := e.db.NewScanner(stmt.Table)
	if err != nil {
		return nil, err
	}

	// Collect rows to delete (can't delete while iterating)
	var toDelete []storage.Record
	for scanner.Valid() {
		rec := scanner.Row()

		// Apply WHERE filter
		if stmt.Where != nil {
			match, err := e.evaluateWhere(stmt.Where, rec, tdef)
			if err != nil {
				return nil, err
			}
			if !match {
				scanner.Next()
				continue
			}
		}

		toDelete = append(toDelete, rec)
		scanner.Next()
	}

	// Delete collected rows
	rowsAffected := 0
	for _, rec := range toDelete {
		// Build a record with just the primary key columns
		pkRec := storage.Record{
			Cols: tdef.Cols[:tdef.PKeys],
			Vals: rec.Vals[:tdef.PKeys],
		}
		deleted, err := e.db.Delete(stmt.Table, pkRec)
		if err != nil {
			return nil, err
		}
		if deleted {
			rowsAffected++
		}
	}

	return &Result{
		Message:      fmt.Sprintf("%d row(s) deleted", rowsAffected),
		RowsAffected: rowsAffected,
	}, nil
}

func (e *Executor) executeCreateTable(stmt *CreateTableStmt) (*Result, error) {
	// Build TableDef
	tdef := &storage.TableDef{
		Name:  stmt.Table,
		Cols:  make([]string, len(stmt.Columns)),
		Types: make([]uint32, len(stmt.Columns)),
		PKeys: 0,
	}

	for i, col := range stmt.Columns {
		tdef.Cols[i] = col.Name
		switch col.Type {
		case "TEXT", "VARCHAR", "STRING":
			tdef.Types[i] = storage.TYPE_BYTES
		case "INT", "INTEGER", "BIGINT":
			tdef.Types[i] = storage.TYPE_INT64
		default:
			return nil, fmt.Errorf("unknown column type: %s", col.Type)
		}
	}

	// Set primary key count
	for _, pk := range stmt.PKeys {
		for i, col := range tdef.Cols {
			if col == pk {
				// Move primary key column to front if needed
				if i >= tdef.PKeys {
					// Swap with the next primary key position
					tdef.Cols[i], tdef.Cols[tdef.PKeys] = tdef.Cols[tdef.PKeys], tdef.Cols[i]
					tdef.Types[i], tdef.Types[tdef.PKeys] = tdef.Types[tdef.PKeys], tdef.Types[i]
				}
				tdef.PKeys++
				break
			}
		}
	}

	if tdef.PKeys == 0 {
		tdef.PKeys = 1 // Default to first column as primary key
	}

	err := e.db.TableNew(tdef)
	if err != nil {
		return nil, err
	}

	return &Result{Message: fmt.Sprintf("Table '%s' created", stmt.Table)}, nil
}

// evaluateWhere evaluates a WHERE expression against a record
func (e *Executor) evaluateWhere(expr *WhereExpr, rec storage.Record, tdef *storage.TableDef) (bool, error) {
	switch expr.Op {
	case "AND":
		left, err := e.evaluateWhere(expr.Left, rec, tdef)
		if err != nil {
			return false, err
		}
		if !left {
			return false, nil
		}
		return e.evaluateWhere(expr.Right, rec, tdef)
	case "OR":
		left, err := e.evaluateWhere(expr.Left, rec, tdef)
		if err != nil {
			return false, err
		}
		if left {
			return true, nil
		}
		return e.evaluateWhere(expr.Right, rec, tdef)
	case "=", "!=", "<", "<=", ">", ">=":
		return e.evaluateComparison(expr, rec, tdef)
	default:
		return false, fmt.Errorf("unknown operator: %s", expr.Op)
	}
}

func (e *Executor) evaluateComparison(expr *WhereExpr, rec storage.Record, tdef *storage.TableDef) (bool, error) {
	val := rec.Get(expr.Column)
	if val == nil {
		return false, nil // Column not found, no match
	}

	switch v := expr.Value.(type) {
	case string:
		if val.Type != storage.TYPE_BYTES {
			return false, nil
		}
		cmp := bytes.Compare(val.Str, []byte(v))
		return compareResult(cmp, expr.Op), nil
	case int64:
		if val.Type != storage.TYPE_INT64 {
			return false, nil
		}
		var cmp int
		if val.I64 < v {
			cmp = -1
		} else if val.I64 > v {
			cmp = 1
		} else {
			cmp = 0
		}
		return compareResult(cmp, expr.Op), nil
	default:
		return false, fmt.Errorf("unsupported value type in WHERE clause")
	}
}

func compareResult(cmp int, op string) bool {
	switch op {
	case "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	default:
		return false
	}
}

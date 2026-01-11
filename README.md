# TinyDB

A lightweight SQL database written in Go, featuring a custom B-tree index, memory-mapped storage, and an interactive REPL.

## Features

- **SQL Support**: CREATE TABLE, INSERT, SELECT, UPDATE, DELETE
- **Data Types**: TEXT, INT
- **Query Operators**: `=`, `!=`, `<`, `<=`, `>`, `>=`, `AND`, `OR`
- **Clauses**: WHERE, LIMIT
- **B-Tree Indexing**: O(log n) key-value operations
- **Memory-Mapped I/O**: Efficient disk access using mmap
- **Page Recycling**: Free list for reusing deleted pages
- **Interactive REPL**: Command-line interface with syntax help

## Architecture

```
User Input → SQL Parser → AST → Executor → Table → KV Store → B-Tree → Disk
```

| Layer   | Package            | Description                                  |
| ------- | ------------------ | -------------------------------------------- |
| CLI     | `cmd/main.go`      | REPL and single-command modes                |
| Query   | `internal/query`   | SQL parser and executor                      |
| Storage | `internal/storage` | Table abstraction, KV store, page management |
| Index   | `internal/index`   | B-tree implementation                        |

## Quick Start

### Build

```bash
go build -o tinydb ./cmd
```

### Run REPL

```bash
./tinydb
```

### Run with Custom Database File

```bash
./tinydb -db mydata.db
```

### Execute Single Command

```bash
./tinydb -e "SELECT * FROM users"
```

## Usage Examples

```sql
-- Create a table
CREATE TABLE users (id INT, name TEXT, PRIMARY KEY (id))

-- Insert data
INSERT INTO users (id, name) VALUES (1, 'Alice')
INSERT INTO users (id, name) VALUES (2, 'Bob')

-- Query data
SELECT * FROM users
SELECT name FROM users WHERE id = 1
SELECT * FROM users WHERE id > 0 LIMIT 10

-- Update data
UPDATE users SET name = 'Charlie' WHERE id = 2

-- Delete data
DELETE FROM users WHERE id = 1
```

## Command-Line Flags

| Flag  | Default     | Description                         |
| ----- | ----------- | ----------------------------------- |
| `-db` | `tinydb.db` | Path to database file               |
| `-e`  |             | Execute single SQL command and exit |

## REPL Commands

| Command         | Description          |
| --------------- | -------------------- |
| `help`          | Show SQL syntax help |
| `exit` / `quit` | Exit the REPL        |

## Project Structure

```
tinydb/
├── cmd/
│   └── main.go           # CLI entry point
├── internal/
│   ├── index/
│   │   └── btree.go      # B-tree implementation
│   ├── query/
│   │   ├── sql_parser.go # SQL tokenizer and parser
│   │   └── executor.go   # Query execution engine
│   └── storage/
│       ├── kv.go         # Memory-mapped KV store
│       ├── table.go      # Table abstraction layer
│       └── freelist.go   # Page recycling
├── go.mod
└── README.md
```

## Resources

- [Build Your Own Database From Scratch in Go](https://build-your-own.org/database/)

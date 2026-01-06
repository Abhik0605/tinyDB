package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"tinydb-go/internal/query"
	"tinydb-go/internal/storage"
)

func main() {
	// Parse command line flags
	dbPath := flag.String("db", "tinydb.db", "Path to database file")
	execCmd := flag.String("e", "", "Execute a single SQL command and exit")
	flag.Parse()

	// Open database
	db := &storage.DB{Path: *dbPath}
	if err := db.Open(); err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	executor := query.NewExecutor(db)

	// Single command mode
	if *execCmd != "" {
		result, err := executor.Execute(*execCmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		printResult(result)
		return
	}

	// REPL mode
	fmt.Println("TinyDB - A simple SQL database")
	fmt.Println("Type 'exit' or 'quit' to exit, 'help' for help")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("tinydb> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Handle special commands
		lower := strings.ToLower(input)
		if lower == "exit" || lower == "quit" {
			fmt.Println("Goodbye!")
			break
		}
		if lower == "help" {
			printHelp()
			continue
		}

		// Execute SQL
		result, err := executor.Execute(input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}
		printResult(result)
	}
}

func printResult(result *query.Result) {
	if result == nil {
		return
	}

	// If there are rows, print as table
	if len(result.Rows) > 0 {
		printTable(result.Columns, result.Rows)
		fmt.Printf("(%d rows)\n", len(result.Rows))
	} else if result.Message != "" {
		fmt.Println(result.Message)
	} else if result.RowsAffected > 0 {
		fmt.Printf("%d row(s) affected\n", result.RowsAffected)
	} else {
		fmt.Println("OK")
	}
	fmt.Println()
}

func printTable(columns []string, rows [][]interface{}) {
	if len(columns) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = len(col)
	}
	for _, row := range rows {
		for i, val := range row {
			s := fmt.Sprintf("%v", val)
			if len(s) > widths[i] {
				widths[i] = len(s)
			}
		}
	}

	// Print header
	printRow(columns, widths)
	printSeparator(widths)

	// Print rows
	for _, row := range rows {
		strs := make([]string, len(row))
		for i, val := range row {
			strs[i] = fmt.Sprintf("%v", val)
		}
		printRow(strs, widths)
	}
}

func printRow(values []string, widths []int) {
	for i, val := range values {
		fmt.Printf("| %-*s ", widths[i], val)
	}
	fmt.Println("|")
}

func printSeparator(widths []int) {
	for _, w := range widths {
		fmt.Print("+")
		for j := 0; j < w+2; j++ {
			fmt.Print("-")
		}
	}
	fmt.Println("+")
}

func printHelp() {
	fmt.Println("TinyDB SQL Commands:")
	fmt.Println("  CREATE TABLE name (col1 TYPE, col2 TYPE, PRIMARY KEY (col1))")
	fmt.Println("  INSERT INTO table (col1, col2) VALUES (val1, val2)")
	fmt.Println("  SELECT * FROM table [WHERE condition] [LIMIT n]")
	fmt.Println("  UPDATE table SET col = val [WHERE condition]")
	fmt.Println("  DELETE FROM table [WHERE condition]")
	fmt.Println()
	fmt.Println("Types: TEXT, INT")
	fmt.Println("Operators: =, !=, <, <=, >, >=, AND, OR")
	fmt.Println()
}

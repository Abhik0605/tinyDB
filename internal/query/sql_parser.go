package query

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Statement types
type StmtType int

const (
	STMT_SELECT StmtType = iota
	STMT_INSERT
	STMT_UPDATE
	STMT_DELETE
	STMT_CREATE_TABLE
)

// Statement is the interface for all SQL statements
type Statement interface {
	Type() StmtType
}

// SelectStmt represents a SELECT statement
type SelectStmt struct {
	Table   string
	Columns []string // column names to select, "*" means all
	Where   *WhereExpr
	Limit   int
}

func (s *SelectStmt) Type() StmtType { return STMT_SELECT }

// InsertStmt represents an INSERT statement
type InsertStmt struct {
	Table   string
	Columns []string
	Values  []interface{} // can be string or int64
}

func (s *InsertStmt) Type() StmtType { return STMT_INSERT }

// UpdateStmt represents an UPDATE statement
type UpdateStmt struct {
	Table string
	Sets  map[string]interface{} // column -> value
	Where *WhereExpr
}

func (s *UpdateStmt) Type() StmtType { return STMT_UPDATE }

// DeleteStmt represents a DELETE statement
type DeleteStmt struct {
	Table string
	Where *WhereExpr
}

func (s *DeleteStmt) Type() StmtType { return STMT_DELETE }

// CreateTableStmt represents a CREATE TABLE statement
type CreateTableStmt struct {
	Table   string
	Columns []ColumnDef
	PKeys   []string // primary key column names
}

func (s *CreateTableStmt) Type() StmtType { return STMT_CREATE_TABLE }

// ColumnDef defines a column in a CREATE TABLE statement
type ColumnDef struct {
	Name string
	Type string // "TEXT" or "INT"
}

// WhereExpr represents a WHERE clause expression
type WhereExpr struct {
	Op     string      // "AND", "OR", "NOT", "=", "!=", "<", "<=", ">", ">="
	Left   *WhereExpr  // for binary ops
	Right  *WhereExpr  // for binary ops
	Column string      // for comparison ops
	Value  interface{} // for comparison ops (string or int64)
}

// Token types
type TokenType int

const (
	TOK_EOF TokenType = iota
	TOK_IDENT
	TOK_NUMBER
	TOK_STRING
	TOK_LPAREN
	TOK_RPAREN
	TOK_COMMA
	TOK_STAR
	TOK_EQ
	TOK_NE
	TOK_LT
	TOK_LE
	TOK_GT
	TOK_GE
	TOK_SEMICOLON
)

type Token struct {
	Type  TokenType
	Value string
}

// Lexer tokenizes SQL input
type Lexer struct {
	input string
	pos   int
}

func NewLexer(input string) *Lexer {
	return &Lexer{input: input, pos: 0}
}

func (l *Lexer) peek() byte {
	if l.pos >= len(l.input) {
		return 0
	}
	return l.input[l.pos]
}

func (l *Lexer) advance() byte {
	ch := l.peek()
	l.pos++
	return ch
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(rune(l.input[l.pos])) {
		l.pos++
	}
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{TOK_EOF, ""}
	}

	ch := l.peek()

	// Single character tokens
	switch ch {
	case '(':
		l.advance()
		return Token{TOK_LPAREN, "("}
	case ')':
		l.advance()
		return Token{TOK_RPAREN, ")"}
	case ',':
		l.advance()
		return Token{TOK_COMMA, ","}
	case '*':
		l.advance()
		return Token{TOK_STAR, "*"}
	case ';':
		l.advance()
		return Token{TOK_SEMICOLON, ";"}
	case '=':
		l.advance()
		return Token{TOK_EQ, "="}
	case '<':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{TOK_LE, "<="}
		}
		if l.peek() == '>' {
			l.advance()
			return Token{TOK_NE, "<>"}
		}
		return Token{TOK_LT, "<"}
	case '>':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{TOK_GE, ">="}
		}
		return Token{TOK_GT, ">"}
	case '!':
		l.advance()
		if l.peek() == '=' {
			l.advance()
			return Token{TOK_NE, "!="}
		}
		return Token{TOK_EOF, ""} // error
	case '\'':
		return l.readString()
	}

	// Numbers
	if unicode.IsDigit(rune(ch)) || (ch == '-' && l.pos+1 < len(l.input) && unicode.IsDigit(rune(l.input[l.pos+1]))) {
		return l.readNumber()
	}

	// Identifiers and keywords
	if unicode.IsLetter(rune(ch)) || ch == '_' {
		return l.readIdent()
	}

	l.advance()
	return Token{TOK_EOF, ""}
}

func (l *Lexer) readString() Token {
	l.advance() // skip opening quote
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != '\'' {
		l.pos++
	}
	value := l.input[start:l.pos]
	if l.pos < len(l.input) {
		l.advance() // skip closing quote
	}
	return Token{TOK_STRING, value}
}

func (l *Lexer) readNumber() Token {
	start := l.pos
	if l.peek() == '-' {
		l.advance()
	}
	for l.pos < len(l.input) && unicode.IsDigit(rune(l.input[l.pos])) {
		l.pos++
	}
	return Token{TOK_NUMBER, l.input[start:l.pos]}
}

func (l *Lexer) readIdent() Token {
	start := l.pos
	for l.pos < len(l.input) && (unicode.IsLetter(rune(l.input[l.pos])) || unicode.IsDigit(rune(l.input[l.pos])) || l.input[l.pos] == '_') {
		l.pos++
	}
	return Token{TOK_IDENT, l.input[start:l.pos]}
}

// Parser parses SQL statements
type Parser struct {
	lexer   *Lexer
	current Token
}

func NewParser(input string) *Parser {
	p := &Parser{lexer: NewLexer(input)}
	p.current = p.lexer.NextToken()
	return p
}

func (p *Parser) advance() {
	p.current = p.lexer.NextToken()
}

func (p *Parser) expect(typ TokenType) error {
	if p.current.Type != typ {
		return fmt.Errorf("unexpected token: %v, expected type %v", p.current.Value, typ)
	}
	p.advance()
	return nil
}

func (p *Parser) expectKeyword(keyword string) error {
	if p.current.Type != TOK_IDENT || !strings.EqualFold(p.current.Value, keyword) {
		return fmt.Errorf("expected keyword '%s', got '%s'", keyword, p.current.Value)
	}
	p.advance()
	return nil
}

func (p *Parser) isKeyword(keyword string) bool {
	return p.current.Type == TOK_IDENT && strings.EqualFold(p.current.Value, keyword)
}

// ParseSQL parses a SQL statement and returns a Statement
func ParseSQL(sql string) (Statement, error) {
	p := NewParser(sql)
	return p.parseStatement()
}

func (p *Parser) parseStatement() (Statement, error) {
	if p.isKeyword("SELECT") {
		return p.parseSelect()
	}
	if p.isKeyword("INSERT") {
		return p.parseInsert()
	}
	if p.isKeyword("UPDATE") {
		return p.parseUpdate()
	}
	if p.isKeyword("DELETE") {
		return p.parseDelete()
	}
	if p.isKeyword("CREATE") {
		return p.parseCreate()
	}
	return nil, fmt.Errorf("unknown statement starting with: %s", p.current.Value)
}

func (p *Parser) parseSelect() (*SelectStmt, error) {
	if err := p.expectKeyword("SELECT"); err != nil {
		return nil, err
	}

	stmt := &SelectStmt{Columns: []string{}}

	// Parse column list
	if p.current.Type == TOK_STAR {
		stmt.Columns = append(stmt.Columns, "*")
		p.advance()
	} else {
		for {
			if p.current.Type != TOK_IDENT {
				return nil, fmt.Errorf("expected column name, got: %s", p.current.Value)
			}
			stmt.Columns = append(stmt.Columns, p.current.Value)
			p.advance()
			if p.current.Type != TOK_COMMA {
				break
			}
			p.advance() // skip comma
		}
	}

	// FROM clause
	if err := p.expectKeyword("FROM"); err != nil {
		return nil, err
	}
	if p.current.Type != TOK_IDENT {
		return nil, fmt.Errorf("expected table name")
	}
	stmt.Table = p.current.Value
	p.advance()

	// Optional WHERE clause
	if p.isKeyword("WHERE") {
		p.advance()
		where, err := p.parseWhereExpr()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}

	// Optional LIMIT clause
	if p.isKeyword("LIMIT") {
		p.advance()
		if p.current.Type != TOK_NUMBER {
			return nil, fmt.Errorf("expected number after LIMIT")
		}
		limit, err := strconv.Atoi(p.current.Value)
		if err != nil {
			return nil, err
		}
		stmt.Limit = limit
		p.advance()
	}

	return stmt, nil
}

func (p *Parser) parseInsert() (*InsertStmt, error) {
	if err := p.expectKeyword("INSERT"); err != nil {
		return nil, err
	}
	if err := p.expectKeyword("INTO"); err != nil {
		return nil, err
	}

	stmt := &InsertStmt{Columns: []string{}, Values: []interface{}{}}

	// Table name
	if p.current.Type != TOK_IDENT {
		return nil, fmt.Errorf("expected table name")
	}
	stmt.Table = p.current.Value
	p.advance()

	// Column list
	if err := p.expect(TOK_LPAREN); err != nil {
		return nil, err
	}
	for {
		if p.current.Type != TOK_IDENT {
			return nil, fmt.Errorf("expected column name")
		}
		stmt.Columns = append(stmt.Columns, p.current.Value)
		p.advance()
		if p.current.Type != TOK_COMMA {
			break
		}
		p.advance()
	}
	if err := p.expect(TOK_RPAREN); err != nil {
		return nil, err
	}

	// VALUES clause
	if err := p.expectKeyword("VALUES"); err != nil {
		return nil, err
	}
	if err := p.expect(TOK_LPAREN); err != nil {
		return nil, err
	}
	for {
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		stmt.Values = append(stmt.Values, val)
		if p.current.Type != TOK_COMMA {
			break
		}
		p.advance()
	}
	if err := p.expect(TOK_RPAREN); err != nil {
		return nil, err
	}

	return stmt, nil
}

func (p *Parser) parseUpdate() (*UpdateStmt, error) {
	if err := p.expectKeyword("UPDATE"); err != nil {
		return nil, err
	}

	stmt := &UpdateStmt{Sets: make(map[string]interface{})}

	// Table name
	if p.current.Type != TOK_IDENT {
		return nil, fmt.Errorf("expected table name")
	}
	stmt.Table = p.current.Value
	p.advance()

	// SET clause
	if err := p.expectKeyword("SET"); err != nil {
		return nil, err
	}
	for {
		if p.current.Type != TOK_IDENT {
			return nil, fmt.Errorf("expected column name")
		}
		col := p.current.Value
		p.advance()
		if err := p.expect(TOK_EQ); err != nil {
			return nil, err
		}
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		stmt.Sets[col] = val
		if p.current.Type != TOK_COMMA {
			break
		}
		p.advance()
	}

	// Optional WHERE clause
	if p.isKeyword("WHERE") {
		p.advance()
		where, err := p.parseWhereExpr()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}

	return stmt, nil
}

func (p *Parser) parseDelete() (*DeleteStmt, error) {
	if err := p.expectKeyword("DELETE"); err != nil {
		return nil, err
	}
	if err := p.expectKeyword("FROM"); err != nil {
		return nil, err
	}

	stmt := &DeleteStmt{}

	// Table name
	if p.current.Type != TOK_IDENT {
		return nil, fmt.Errorf("expected table name")
	}
	stmt.Table = p.current.Value
	p.advance()

	// Optional WHERE clause
	if p.isKeyword("WHERE") {
		p.advance()
		where, err := p.parseWhereExpr()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}

	return stmt, nil
}

func (p *Parser) parseCreate() (*CreateTableStmt, error) {
	if err := p.expectKeyword("CREATE"); err != nil {
		return nil, err
	}
	if err := p.expectKeyword("TABLE"); err != nil {
		return nil, err
	}

	stmt := &CreateTableStmt{Columns: []ColumnDef{}, PKeys: []string{}}

	// Table name
	if p.current.Type != TOK_IDENT {
		return nil, fmt.Errorf("expected table name")
	}
	stmt.Table = p.current.Value
	p.advance()

	// Column definitions
	if err := p.expect(TOK_LPAREN); err != nil {
		return nil, err
	}
	for {
		// Check for PRIMARY KEY clause
		if p.isKeyword("PRIMARY") {
			p.advance()
			if err := p.expectKeyword("KEY"); err != nil {
				return nil, err
			}
			if err := p.expect(TOK_LPAREN); err != nil {
				return nil, err
			}
			for {
				if p.current.Type != TOK_IDENT {
					return nil, fmt.Errorf("expected column name in PRIMARY KEY")
				}
				stmt.PKeys = append(stmt.PKeys, p.current.Value)
				p.advance()
				if p.current.Type != TOK_COMMA {
					break
				}
				p.advance()
			}
			if err := p.expect(TOK_RPAREN); err != nil {
				return nil, err
			}
		} else {
			// Column definition
			if p.current.Type != TOK_IDENT {
				break
			}
			col := ColumnDef{Name: p.current.Value}
			p.advance()
			if p.current.Type != TOK_IDENT {
				return nil, fmt.Errorf("expected column type")
			}
			col.Type = strings.ToUpper(p.current.Value)
			p.advance()
			stmt.Columns = append(stmt.Columns, col)
		}
		if p.current.Type != TOK_COMMA {
			break
		}
		p.advance()
	}
	if err := p.expect(TOK_RPAREN); err != nil {
		return nil, err
	}

	// If no primary key specified, use first column
	if len(stmt.PKeys) == 0 && len(stmt.Columns) > 0 {
		stmt.PKeys = []string{stmt.Columns[0].Name}
	}

	return stmt, nil
}

func (p *Parser) parseValue() (interface{}, error) {
	switch p.current.Type {
	case TOK_STRING:
		val := p.current.Value
		p.advance()
		return val, nil
	case TOK_NUMBER:
		val, err := strconv.ParseInt(p.current.Value, 10, 64)
		if err != nil {
			return nil, err
		}
		p.advance()
		return val, nil
	default:
		return nil, fmt.Errorf("expected value, got: %s", p.current.Value)
	}
}

func (p *Parser) parseWhereExpr() (*WhereExpr, error) {
	return p.parseOrExpr()
}

func (p *Parser) parseOrExpr() (*WhereExpr, error) {
	left, err := p.parseAndExpr()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("OR") {
		p.advance()
		right, err := p.parseAndExpr()
		if err != nil {
			return nil, err
		}
		left = &WhereExpr{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAndExpr() (*WhereExpr, error) {
	left, err := p.parseCompareExpr()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("AND") {
		p.advance()
		right, err := p.parseCompareExpr()
		if err != nil {
			return nil, err
		}
		left = &WhereExpr{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseCompareExpr() (*WhereExpr, error) {
	// Handle parentheses
	if p.current.Type == TOK_LPAREN {
		p.advance()
		expr, err := p.parseOrExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(TOK_RPAREN); err != nil {
			return nil, err
		}
		return expr, nil
	}

	// Column name
	if p.current.Type != TOK_IDENT {
		return nil, fmt.Errorf("expected column name in WHERE clause, got: %s", p.current.Value)
	}
	col := p.current.Value
	p.advance()

	// Comparison operator
	var op string
	switch p.current.Type {
	case TOK_EQ:
		op = "="
	case TOK_NE:
		op = "!="
	case TOK_LT:
		op = "<"
	case TOK_LE:
		op = "<="
	case TOK_GT:
		op = ">"
	case TOK_GE:
		op = ">="
	default:
		return nil, fmt.Errorf("expected comparison operator, got: %s", p.current.Value)
	}
	p.advance()

	// Value
	val, err := p.parseValue()
	if err != nil {
		return nil, err
	}

	return &WhereExpr{Op: op, Column: col, Value: val}, nil
}

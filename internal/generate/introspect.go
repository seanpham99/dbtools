// Package generate provides pydantic BaseModel rendering and shared schema
// types for database introspection.
package generate

import "database/sql"

// ColumnSchema represents a column's metadata across database engines.
type ColumnSchema struct {
	Name       string
	PyName     string // Name, sanitized for use as a Python identifier
	DataType   string
	PythonType string
	IsNullable bool
	MaxLength  sql.NullInt64
	Precision  sql.NullInt64
	Scale      sql.NullInt64
	// OrdinalPosition is the column's 1-based position in the table.
	OrdinalPosition int
	// DefaultValue is the raw dialect-native default expression text
	// ("" / invalid if none). Never compared across dialects — only
	// used to detect drift within the same database.
	DefaultValue sql.NullString
	// IsPrimaryKey is true if this column participates in the table's
	// primary key (composite or single-column).
	IsPrimaryKey bool
}

// IndexSchema represents one non-primary-key index. A primary key's
// backing index is never listed here — it's already represented by
// ColumnSchema.IsPrimaryKey on its member columns.
type IndexSchema struct {
	Name    string
	Columns []string // ordered
	Unique  bool
}

// ForeignKeySchema represents one foreign key constraint.
type ForeignKeySchema struct {
	Name       string
	Columns    []string // ordered, this table's side
	RefSchema  string
	RefTable   string
	RefColumns []string // ordered, referenced table's side
}

// CheckConstraintSchema represents one CHECK constraint. Always empty for
// SQLite tables — SQLite has no queryable CHECK-constraint catalog, only
// the raw CREATE TABLE text, and dbtools deliberately does not parse DDL
// text to approximate this (see the design spec).
type CheckConstraintSchema struct {
	Name       string
	Expression string
}

// TableSchema represents a table or view's metadata across database engines.
type TableSchema struct {
	Schema           string
	Name             string
	ClassName        string
	Columns          []ColumnSchema
	Indexes          []IndexSchema
	ForeignKeys      []ForeignKeySchema
	CheckConstraints []CheckConstraintSchema
}

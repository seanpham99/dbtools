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
}

// TableSchema represents a table or view's metadata across database engines.
type TableSchema struct {
	Schema    string
	Name      string
	ClassName string
	Columns   []ColumnSchema
}

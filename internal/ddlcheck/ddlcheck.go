// Package ddlcheck defines the shared object reference type used for DDL
// drift detection across database engines.
package ddlcheck

// ObjectRef names one database object a migration's DDL creates or drops.
type ObjectRef struct {
	Schema string
	Name   string
	Kind   string // "table", "procedure", "view", or "function"
}

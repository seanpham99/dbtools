// Package ddlcheck defines the shared object reference type used for DDL
// drift detection across database engines.
package ddlcheck

// ObjectRef names one database object a migration's DDL creates or drops.
type ObjectRef struct {
	Schema string
	Name   string
	Kind   string // "table", "procedure", "view", or "function"
}

// Rename is one ALTER … RENAME TO statement. From is the object under the
// name it carried before, To the name it carries after.
type Rename struct {
	From, To ObjectRef
}

// Extractor is the parsing half of an engine's DDL dialect: the object
// names one migration file creates, drops, and renames. Implemented by
// engine.DDLDialect.
type Extractor interface {
	ExtractObjects(sqlText string) []ObjectRef
	ExtractDroppedObjects(sqlText string) []ObjectRef
	ExtractRenamedObjects(sqlText string) []Rename
}

// Lifecycle is what one migration file does to object names: the objects it
// leaves behind for later versions, and the names it destroys on the way.
// A file that creates a table and renames it — the rebuild-in-place shape
// every constraint migration uses — leaves nothing behind under the
// temporary name, so a caller that requires every created name to exist
// would report drift against a schema that matches the file exactly.
type Lifecycle struct {
	// LeftBehind is every name the file leaves in place for later
	// versions: its CREATEs plus the names its renames produce, in source
	// order without repeats.
	LeftBehind []ObjectRef
	// Destroyed is every name the file removes: its DROPs plus the sources
	// of its renames.
	Destroyed []ObjectRef

	created   map[ObjectRef]bool
	destroyed map[ObjectRef]bool
	renamedTo map[ObjectRef]bool
}

// Temporary reports whether ref is a staging name the file creates and
// then destroys: the temporary table a rebuild in place renames away, or a
// scratch table the file drops again. No such name survives the file, so
// requiring it to exist afterwards would report drift against a schema
// that matches the file exactly. A name a rename produces is never
// temporary — the file does leave that one behind, so it stays checked.
func (l Lifecycle) Temporary(ref ObjectRef) bool {
	return l.created[ref] && l.destroyed[ref] && !l.renamedTo[ref]
}

// Resolve reads one migration file through ext and returns its lifecycle.
// A rename contributes both sides: its target is left behind under the new
// name, its source is destroyed under the old one.
func Resolve(ext Extractor, sqlText string) Lifecycle {
	lc := Lifecycle{
		created:   make(map[ObjectRef]bool),
		destroyed: make(map[ObjectRef]bool),
		renamedTo: make(map[ObjectRef]bool),
	}
	seen := make(map[ObjectRef]bool)
	leave := func(ref ObjectRef) {
		if !seen[ref] {
			seen[ref] = true
			lc.LeftBehind = append(lc.LeftBehind, ref)
		}
	}
	destroy := func(ref ObjectRef) {
		if !lc.destroyed[ref] {
			lc.destroyed[ref] = true
			lc.Destroyed = append(lc.Destroyed, ref)
		}
	}

	for _, obj := range ext.ExtractObjects(sqlText) {
		lc.created[obj] = true
		leave(obj)
	}
	for _, obj := range ext.ExtractDroppedObjects(sqlText) {
		destroy(obj)
	}
	for _, r := range ext.ExtractRenamedObjects(sqlText) {
		destroy(r.From)
		lc.renamedTo[r.To] = true
		leave(r.To)
	}
	return lc
}

// Package ddlcheck defines the shared object reference type used for DDL
// drift detection across database engines.
package ddlcheck

import "sort"

// ObjectRef names one database object a migration's DDL creates or drops.
type ObjectRef struct {
	Schema string
	Name   string
	Kind   string // "table", "procedure", "view", or "function"
}

// OpKind classifies what one DDL statement does to the name it names.
type OpKind int

const (
	// OpCreate is a CREATE that brings a name into existence.
	OpCreate OpKind = iota
	// OpDrop is a DROP that removes a name.
	OpDrop
	// OpRenameFrom is the name an ALTER … RENAME TO takes away.
	OpRenameFrom
	// OpRenameTo is the name the same ALTER … RENAME TO produces.
	OpRenameTo
)

// Operation is one thing a migration file does to one object name, at the
// byte offset where the statement naming it starts. The offset is its own
// field rather than part of ObjectRef because that type's three fields are
// the object's identity: the same name reached twice is one object, and a
// position carried inside the key would make it two.
type Operation struct {
	Kind OpKind
	Ref  ObjectRef
	Pos  int
}

// Rename is one ALTER … RENAME TO statement. From is the object under the
// name it carried before, To the name it carries after.
type Rename struct {
	From, To ObjectRef
}

// removes reports whether kind takes a name away rather than leaving one.
func removes(kind OpKind) bool { return kind == OpDrop || kind == OpRenameFrom }

// Submatch returns capture group n of a match located by
// regexp.Regexp.FindAllStringSubmatchIndex, or "" when the group took no
// part in the match.
func Submatch(text string, loc []int, n int) string {
	if 2*n+1 >= len(loc) || loc[2*n] < 0 {
		return ""
	}
	return text[loc[2*n]:loc[2*n+1]]
}

// RefsOf returns the refs the given operations act on, in the order they
// appear in ops.
func RefsOf(ops []Operation, kind OpKind) []ObjectRef {
	refs := make([]ObjectRef, 0, len(ops))
	for _, op := range ops {
		if op.Kind == kind {
			refs = append(refs, op.Ref)
		}
	}
	return refs
}

// RenamesOf pairs every OpRenameFrom with the OpRenameTo its statement
// produces, which sits at the same offset. Operations are expected in source
// order, as ExtractOperations returns them.
func RenamesOf(ops []Operation) []Rename {
	renames := make([]Rename, 0, len(ops))
	for i, op := range ops {
		if op.Kind != OpRenameFrom {
			continue
		}
		for _, to := range ops[i+1:] {
			if to.Kind != OpRenameTo || to.Pos != op.Pos {
				continue
			}
			renames = append(renames, Rename{From: op.Ref, To: to.Ref})
			break
		}
	}
	return renames
}

// Extractor is the parsing half of an engine's DDL dialect: the ordered
// object-naming operations one migration file contains, plus the per-kind
// projections of that stream. Only the ordered stream says whether a name
// survives the file, so Resolve reads that; the projections are what
// callers and engine tests ask for by name. Implemented by
// engine.DDLDialect.
type Extractor interface {
	ExtractOperations(sqlText string) []Operation
	ExtractObjects(sqlText string) []ObjectRef
	ExtractDroppedObjects(sqlText string) []ObjectRef
	ExtractRenamedObjects(sqlText string) []Rename
}

// Lifecycle is what one migration file does to object names: the objects it
// leaves behind for later versions, and the names it ends without. A file
// that creates a table and renames it — the rebuild-in-place shape every
// constraint migration uses — leaves nothing behind under the temporary name,
// so a caller that requires every created name to exist would report drift
// against a schema that matches the file exactly.
type Lifecycle struct {
	// LeftBehind is every name the file brings into existence or renames
	// onto, in source order without repeats. A name the file also ends
	// without is on this list but is not required to exist.
	LeftBehind []ObjectRef
	// Destroyed is every name the file ends without, in source order
	// without repeats: the names whose final operation is a removal.
	Destroyed []ObjectRef

	// produced is every name the file brings into existence, and last is
	// each name's final operation. A name is temporary when the file both
	// produces it and ends without it.
	produced map[ObjectRef]bool
	last     map[ObjectRef]OpKind
}

// Temporary reports whether ref is a staging name: the file brings it into
// existence and its final operation on that name removes it. That is the
// temporary table a rebuild in place renames away, a scratch table the file
// drops again, and a name a rename produces that the file then drops. No
// such name survives the file, so requiring it to exist afterwards would
// report drift against a schema that matches the file exactly. A name whose
// final operation leaves it in place — because it was created last, or
// because a rename renamed something onto it — is not temporary, and stays
// checked. A name the file never produces is not temporary either: an
// earlier version created it, so that version's check still applies.
func (l Lifecycle) Temporary(ref ObjectRef) bool {
	return l.produced[ref] && removes(l.last[ref])
}

// Resolve reads one migration file through ext and returns its lifecycle.
// Order decides the outcome: `DROP TABLE t; CREATE TABLE t (…)` leaves t in
// place and is not temporary, while `CREATE TABLE t; DROP TABLE t` leaves it
// absent and is.
func Resolve(ext Extractor, sqlText string) Lifecycle {
	ops := append([]Operation(nil), ext.ExtractOperations(sqlText)...)
	sort.SliceStable(ops, func(i, j int) bool { return ops[i].Pos < ops[j].Pos })

	lc := Lifecycle{
		produced: make(map[ObjectRef]bool),
		last:     make(map[ObjectRef]OpKind),
	}
	// One dedupe set per list: a name can be both produced and finally
	// removed, and it belongs on both lists.
	seenLeft := make(map[ObjectRef]bool)
	seenRemoved := make(map[ObjectRef]bool)
	for _, op := range ops {
		lc.last[op.Ref] = op.Kind
		switch op.Kind {
		case OpCreate, OpRenameTo:
			lc.produced[op.Ref] = true
			if !seenLeft[op.Ref] {
				seenLeft[op.Ref] = true
				lc.LeftBehind = append(lc.LeftBehind, op.Ref)
			}
		}
	}
	for _, op := range ops {
		if removes(op.Kind) && lc.last[op.Ref] == op.Kind && !seenRemoved[op.Ref] {
			seenRemoved[op.Ref] = true
			lc.Destroyed = append(lc.Destroyed, op.Ref)
		}
	}
	return lc
}

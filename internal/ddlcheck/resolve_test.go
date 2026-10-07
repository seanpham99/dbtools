package ddlcheck

import "testing"

// fakeExtractor is a hand-fed stand-in for an engine dialect, so Resolve's
// contract is tested without a database or a dialect regex. ops carries the
// positions the real extractors report, so a test can state an order the way
// a migration file writes it.
type fakeExtractor struct {
	ops []Operation
}

func (f fakeExtractor) ExtractOperations(string) []Operation { return f.ops }
func (f fakeExtractor) ExtractObjects(s string) []ObjectRef {
	return RefsOf(f.ExtractOperations(s), OpCreate)
}
func (f fakeExtractor) ExtractDroppedObjects(s string) []ObjectRef {
	return RefsOf(f.ExtractOperations(s), OpDrop)
}
func (f fakeExtractor) ExtractRenamedObjects(s string) []Rename {
	return RenamesOf(f.ExtractOperations(s))
}

func table(name string) ObjectRef { return ObjectRef{Schema: "public", Name: name, Kind: "table"} }

// createAt, dropAt, and renameAt build one operation at a source offset, so
// a test file's statement order is readable as its order.
func createAt(pos int, name string) Operation {
	return Operation{Kind: OpCreate, Ref: table(name), Pos: pos}
}

func dropAt(pos int, name string) Operation {
	return Operation{Kind: OpDrop, Ref: table(name), Pos: pos}
}

func renameAt(pos int, from, to string) []Operation {
	return []Operation{
		{Kind: OpRenameFrom, Ref: table(from), Pos: pos},
		{Kind: OpRenameTo, Ref: table(to), Pos: pos},
	}
}

func TestResolve_RenameLeavesTargetAndMakesSourceTemporary(t *testing.T) {
	ops := []Operation{createAt(0, "widget_new")}
	ops = append(ops, renameAt(80, "widget_new", "widget")...)

	lc := Resolve(fakeExtractor{ops: ops}, "")

	if len(lc.LeftBehind) != 2 || lc.LeftBehind[0] != table("widget_new") || lc.LeftBehind[1] != table("widget") {
		t.Errorf("LeftBehind = %+v, want the create first and the rename target second", lc.LeftBehind)
	}
	if !lc.Temporary(table("widget_new")) {
		t.Error("Temporary(widget_new) = false, want true — the rename consumes the name it created")
	}
	if lc.Temporary(table("widget")) {
		t.Error("Temporary(widget) = true, want false — the rename target is what the file leaves behind")
	}
}

// TestResolve_RenameTargetOfADroppedNameIsNotTemporary covers the case a
// rebuild in place always hits: the file drops the old name and produces it
// again from the rename, so the name is dropped AND left behind. Only the
// staging name is temporary.
func TestResolve_RenameTargetOfADroppedNameIsNotTemporary(t *testing.T) {
	ops := []Operation{dropAt(0, "widget"), createAt(40, "widget_new")}
	ops = append(ops, renameAt(120, "widget_new", "widget")...)

	lc := Resolve(fakeExtractor{ops: ops}, "")

	if lc.Temporary(table("widget")) {
		t.Error("Temporary(widget) = true, want false — the rename puts the name back last")
	}
	if !lc.Temporary(table("widget_new")) {
		t.Error("Temporary(widget_new) = false, want true")
	}
	if len(lc.Destroyed) != 1 || lc.Destroyed[0] != table("widget_new") {
		t.Errorf("Destroyed = %+v, want only the staging name — the old name's final operation renames something onto it", lc.Destroyed)
	}
}

func TestResolve_LeftBehindKeepsSourceOrderWithoutDuplicates(t *testing.T) {
	lc := Resolve(fakeExtractor{ops: []Operation{
		createAt(0, "a"), createAt(40, "b"), createAt(80, "a"),
	}}, "")

	if len(lc.LeftBehind) != 2 || lc.LeftBehind[0] != table("a") || lc.LeftBehind[1] != table("b") {
		t.Fatalf("LeftBehind = %+v, want [a b] in source order with the repeat dropped", lc.LeftBehind)
	}
}

// TestResolve_CreateThenDropIsTemporary pins the rebuild's scratch-table
// shape: created, then dropped, so nothing survives under that name.
func TestResolve_CreateThenDropIsTemporary(t *testing.T) {
	lc := Resolve(fakeExtractor{ops: []Operation{
		createAt(0, "scratch"), dropAt(60, "scratch"),
	}}, "")

	if !lc.Temporary(table("scratch")) {
		t.Error("Temporary(scratch) = false, want true — a same-file create+drop leaves the name absent")
	}
	if len(lc.LeftBehind) != 1 {
		t.Fatalf("LeftBehind = %+v, want the created name still listed once", lc.LeftBehind)
	}
	if len(lc.Destroyed) != 1 || lc.Destroyed[0] != table("scratch") {
		t.Errorf("Destroyed = %+v, want [scratch]", lc.Destroyed)
	}
}

// TestResolve_DropThenCreateIsNotTemporary is the same two statements in the
// other order, which is how a migration reshapes a table it keeps: the name
// must exist afterwards, so its absence is drift.
func TestResolve_DropThenCreateIsNotTemporary(t *testing.T) {
	lc := Resolve(fakeExtractor{ops: []Operation{
		dropAt(0, "widget"), createAt(60, "widget"),
	}}, "")

	if lc.Temporary(table("widget")) {
		t.Error("Temporary(widget) = true, want false — the CREATE is the file's last word on the name")
	}
	if len(lc.Destroyed) != 0 {
		t.Errorf("Destroyed = %+v, want empty — the file leaves the name in place", lc.Destroyed)
	}
}

func TestResolve_ADroppedNameTheFileDoesNotCreateIsNotTemporary(t *testing.T) {
	lc := Resolve(fakeExtractor{ops: []Operation{dropAt(0, "widget")}}, "")

	if lc.Temporary(table("widget")) {
		t.Error("Temporary(widget) = true, want false — an earlier version created it, so its check still applies")
	}
	if len(lc.LeftBehind) != 0 {
		t.Errorf("LeftBehind = %+v, want empty — the file drops the name without producing it", lc.LeftBehind)
	}
}

// TestResolve_RenameTargetThatTheFileThenDropsIsTemporary covers a name that
// only exists mid-file: the rename puts it there, the DROP takes it away.
func TestResolve_RenameTargetThatTheFileThenDropsIsTemporary(t *testing.T) {
	ops := renameAt(0, "widget_new", "widget")
	ops = append(ops, dropAt(80, "widget"))

	lc := Resolve(fakeExtractor{ops: ops}, "")

	if !lc.Temporary(table("widget")) {
		t.Error("Temporary(widget) = false, want true — the DROP is the file's last word on the name")
	}
	if len(lc.Destroyed) != 2 {
		t.Errorf("Destroyed = %+v, want the rename source and the dropped name", lc.Destroyed)
	}
}

// TestResolve_UnorderedOperationsAreSortedByPosition guards the resolver
// against an extractor that reports its kinds in pattern order rather than
// source order: DROP t then CREATE t must not read as CREATE then DROP.
func TestResolve_UnorderedOperationsAreSortedByPosition(t *testing.T) {
	lc := Resolve(fakeExtractor{ops: []Operation{
		createAt(60, "widget"), dropAt(0, "widget"),
	}}, "")

	if lc.Temporary(table("widget")) {
		t.Error("Temporary(widget) = true, want false — sorted by position, the CREATE is last")
	}
}

func TestRenamesOf(t *testing.T) {
	ops := []Operation{createAt(0, "a")}
	ops = append(ops, renameAt(40, "a", "b")...)
	ops = append(ops, createAt(120, "c"))
	ops = append(ops, renameAt(160, "c", "d")...)

	got := RenamesOf(ops)

	if len(got) != 2 || got[0] != (Rename{From: table("a"), To: table("b")}) || got[1] != (Rename{From: table("c"), To: table("d")}) {
		t.Fatalf("RenamesOf = %+v, want a→b then c→d", got)
	}
}

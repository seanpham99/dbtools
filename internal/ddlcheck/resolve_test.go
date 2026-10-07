package ddlcheck

import "testing"

// fakeExtractor is a hand-fed stand-in for an engine dialect, so Resolve's
// contract is tested without a database or a dialect regex.
type fakeExtractor struct {
	created, dropped []ObjectRef
	renamed          []Rename
}

func (f fakeExtractor) ExtractObjects(string) []ObjectRef        { return f.created }
func (f fakeExtractor) ExtractDroppedObjects(string) []ObjectRef { return f.dropped }
func (f fakeExtractor) ExtractRenamedObjects(string) []Rename    { return f.renamed }

func table(name string) ObjectRef { return ObjectRef{Schema: "public", Name: name, Kind: "table"} }

func TestResolve_RenameLeavesTargetAndMakesSourceTemporary(t *testing.T) {
	newName, finalName := table("widget_new"), table("widget")
	ext := fakeExtractor{
		created: []ObjectRef{newName},
		renamed: []Rename{{From: newName, To: finalName}},
	}

	lc := Resolve(ext, "")

	if len(lc.LeftBehind) != 2 || lc.LeftBehind[0] != newName || lc.LeftBehind[1] != finalName {
		t.Errorf("LeftBehind = %+v, want the create first and the rename target second", lc.LeftBehind)
	}
	if !lc.Temporary(newName) {
		t.Error("Temporary(widget_new) = false, want true — the rename consumes the name it created")
	}
	if lc.Temporary(finalName) {
		t.Error("Temporary(widget) = true, want false — the rename target is what the file leaves behind")
	}
}

// TestResolve_RenameTargetOfADroppedNameIsNotTemporary covers the case a
// rebuild in place always hits: the file drops the old name and produces it
// again from the rename, so the name is dropped AND left behind. Only the
// staging name is temporary.
func TestResolve_RenameTargetOfADroppedNameIsNotTemporary(t *testing.T) {
	oldName, newName, finalName := table("widget"), table("widget_new"), table("widget")
	ext := fakeExtractor{
		created: []ObjectRef{newName},
		dropped: []ObjectRef{oldName},
		renamed: []Rename{{From: newName, To: finalName}},
	}

	lc := Resolve(ext, "")

	if lc.Temporary(finalName) {
		t.Error("Temporary(widget) = true, want false — the file leaves the renamed name in place")
	}
	if !lc.Temporary(newName) {
		t.Error("Temporary(widget_new) = false, want true")
	}
	if len(lc.Destroyed) != 2 {
		t.Errorf("Destroyed = %+v, want both the dropped name and the rename source", lc.Destroyed)
	}
}

func TestResolve_LeftBehindKeepsSourceOrderWithoutDuplicates(t *testing.T) {
	ext := fakeExtractor{created: []ObjectRef{table("a"), table("b"), table("a")}}

	lc := Resolve(ext, "")

	if len(lc.LeftBehind) != 2 || lc.LeftBehind[0] != table("a") || lc.LeftBehind[1] != table("b") {
		t.Fatalf("LeftBehind = %+v, want [a b] in source order with the repeat dropped", lc.LeftBehind)
	}
}

func TestResolve_CreateThenDropIsTemporary(t *testing.T) {
	scratch := table("scratch")
	ext := fakeExtractor{created: []ObjectRef{scratch}, dropped: []ObjectRef{scratch}}

	lc := Resolve(ext, "")

	if !lc.Temporary(scratch) {
		t.Error("Temporary(scratch) = false, want true — a same-file create+drop leaves the name absent")
	}
	if len(lc.LeftBehind) != 1 {
		t.Fatalf("LeftBehind = %+v, want the created name still listed once", lc.LeftBehind)
	}
}

func TestResolve_ADroppedNameTheFileDoesNotCreateIsNotTemporary(t *testing.T) {
	ext := fakeExtractor{dropped: []ObjectRef{table("widget")}}

	lc := Resolve(ext, "")

	if lc.Temporary(table("widget")) {
		t.Error("Temporary(widget) = true, want false — an earlier version created it, so its check still applies")
	}
	if len(lc.LeftBehind) != 0 {
		t.Errorf("LeftBehind = %+v, want empty — the file drops the name without producing it", lc.LeftBehind)
	}
}

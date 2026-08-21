package generate

import (
	"os/exec"
	"strings"
	"testing"
)

// TestRenderOutputParsesAsPython asserts the generated module compiles with
// python3 (when available) — the C6 regression class (trailing import
// commas, invalid identifiers) would fail here.
func TestRenderOutputParsesAsPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	tables := []TableSchema{
		{
			Schema: "dbo",
			Name:   "2fa_codes",
			Columns: []ColumnSchema{
				{Name: "id", PyName: "id", PythonType: "int", IsNullable: false},
				{Name: "user id", PyName: "userid", PythonType: "str", IsNullable: true},
				{Name: "class", PyName: "class_", PythonType: "str", IsNullable: false},
				{Name: "payload", PyName: "payload", PythonType: "Any", IsNullable: false},
			},
		},
		{
			Schema: "dbo",
			Name:   "trade-log",
			Columns: []ColumnSchema{
				{Name: "ts", PyName: "ts", PythonType: "datetime", IsNullable: false},
			},
		},
	}

	out, err := Render(tables, "local")
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	cmd := exec.Command("python3", "-c", "import ast,sys; ast.parse(sys.stdin.read())")
	cmd.Stdin = strings.NewReader(out)
	if err := cmd.Run(); err != nil {
		t.Fatalf("generated Python does not parse:\n%s\n---\n%v", out, err)
	}
}

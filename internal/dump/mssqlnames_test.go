package dump_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/dump"
)

// Real shapes observed from sys.key_constraints / sys.check_constraints /
// sys.default_constraints / sys.foreign_keys on SQL Server.
const mssqlDumpFixture = `SET XACT_ABORT OFF
GO
IF OBJECT_ID(N'audit.dim_pipeline_run', N'U') IS NULL
BEGIN
CREATE TABLE [audit].[dim_pipeline_run] (
    [pipeline_run_key] bigint NOT NULL,
    [started_at] datetime2(0) NOT NULL CONSTRAINT [DF_audit_dim_pipeline_run_started_at] DEFAULT (sysutcdatetime()),
    [created_at] datetime2(0) NOT NULL CONSTRAINT [DF__audit_dim__creat__6BE40491] DEFAULT (sysutcdatetime()),
    [updated_at] datetime2(0) NOT NULL CONSTRAINT [DF__audit_dim__updat__2F9A1060] DEFAULT (sysutcdatetime()),
    CONSTRAINT [PK_audit_dim_pipeline_run] PRIMARY KEY CLUSTERED ([pipeline_run_key]),
    CONSTRAINT [UQ_audit_dim_pipeline_run_batch] UNIQUE NONCLUSTERED ([ingestion_batch_id]),
    CONSTRAINT [PK__audit_dim__3213E83F732A0327] PRIMARY KEY NONCLUSTERED ([other_key])
)
END
GO
IF NOT EXISTS (SELECT 1 FROM sys.foreign_keys WHERE name = N'FK__fact_child__pid__440B1D61' AND parent_object_id = OBJECT_ID(N'audit.fact_child'))
ALTER TABLE [audit].[fact_child] ADD CONSTRAINT [FK__fact_child__pid__440B1D61] FOREIGN KEY ([pid]) REFERENCES [audit].[dim_pipeline_run] ([pipeline_run_key])
GO
IF NOT EXISTS (SELECT 1 FROM sys.check_constraints WHERE name = N'CK__t_ck__x__403A8C7D' AND parent_object_id = OBJECT_ID(N'audit.t_ck'))
ALTER TABLE [audit].[t_ck] ADD CONSTRAINT [CK__t_ck__x__403A8C7D] CHECK ([x] > 0)
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_audit_dim_pipeline_run_started' AND object_id = OBJECT_ID(N'audit.dim_pipeline_run'))
CREATE NONCLUSTERED INDEX [IX_audit_dim_pipeline_run_started] ON [audit].[dim_pipeline_run] ([started_at])
GO
`

func TestNormalizeMSSQLConstraintNames(t *testing.T) {
	out := dump.NormalizeMSSQLConstraintNames(mssqlDumpFixture)

	// Auto-generated names are gone.
	for _, stale := range []string{
		"3213E83F732A0327", "6BE40491", "2F9A1060",
		"440B1D61", "403A8C7D",
	} {
		if strings.Contains(out, stale) {
			t.Errorf("output still contains random tail %q:\n%s", stale, out)
		}
	}

	// Explicitly named constraints are untouched.
	for _, kept := range []string{
		"[PK_audit_dim_pipeline_run]",
		"[UQ_audit_dim_pipeline_run_batch]",
		"[DF_audit_dim_pipeline_run_started_at]",
		"[IX_audit_dim_pipeline_run_started]",
	} {
		if !strings.Contains(out, kept) {
			t.Errorf("output lost explicit name %q:\n%s", kept, out)
		}
	}

	// Canonical names keep the PREFIX__object__HEX shape and are stable:
	// two runs over dumps that differ only in random tails produce
	// identical output.
	other := strings.NewReplacer(
		"3213E83F732A0327", "AAAAAAAAAAAAAAAA",
		"6BE40491", "BBBBBBBB",
		"2F9A1060", "CCCCCCCC",
		"440B1D61", "DDDDDDDD",
		"403A8C7D", "EEEEEEEE",
	).Replace(mssqlDumpFixture)
	if got := dump.NormalizeMSSQLConstraintNames(other); got != out {
		t.Errorf("normalization not byte-deterministic:\nfirst:\n%s\nsecond:\n%s", out, got)
	}

	// Declaration and guard occurrences of the same name were renamed
	// together: the FK name appears in both the sys.foreign_keys probe
	// and the ADD CONSTRAINT clause with the same canonical spelling.
	if !strings.Contains(out, "PK__audit_dim__") {
		t.Errorf("output lost the PK auto-name prefix:\n%s", out)
	}
}

func TestNormalizeMSSQLConstraintNames_NoAutoNames(t *testing.T) {
	in := "CREATE TABLE [t] ([id] int CONSTRAINT [PK_t] PRIMARY KEY ([id]));\n"
	if got := dump.NormalizeMSSQLConstraintNames(in); got != in {
		t.Errorf("NormalizeMSSQLConstraintNames() changed output without auto names: %q -> %q", in, got)
	}
}

func TestNormalizeMSSQLConstraintNames_SpacedObjectName(t *testing.T) {
	// Auto-generated names inherit the object name verbatim, spaces
	// included — a table literally named "my tabl" yields
	// PK__my tabl__<hex>. Both the bracketed and N'...' forms must be
	// found and renamed consistently.
	in := "CREATE TABLE [my tabl] ([id] int CONSTRAINT [PK__my tabl__3213E83F732A0327] PRIMARY KEY ([id]));\n" +
		"IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'PK__my tabl__3213E83F732A0327' AND object_id = OBJECT_ID(N'dbo.my tabl'))\n"
	out := dump.NormalizeMSSQLConstraintNames(in)
	if strings.Contains(out, "3213E83F732A0327") || strings.Contains(out, "AAAAAAAAAAAAAAAA") {
		t.Errorf("spaced auto-name not normalized:\n%s", out)
	}
	if strings.Count(out, "PK__my tabl__") != 2 {
		t.Errorf("declaration and guard renamed inconsistently:\n%s", out)
	}
}

func TestNormalizeMSSQLConstraintNames_TailExchangeDeterminism(t *testing.T) {
	// Two constraints share the truncated prefix PK__shared__ but carry
	// different random tails. If a second scratch database happens to
	// assign those tails to the opposite objects, canonical assignment
	// must still follow dump position — not the random tail — so the
	// emitted bytes stay identical.
	a := "CREATE TABLE [t1] ([id] int CONSTRAINT [PK__shared__1111111111111111] PRIMARY KEY ([id]));\n" +
		"CREATE TABLE [t2] ([id] int CONSTRAINT [PK__shared__EEEEEEEEEEEEEEEE] PRIMARY KEY ([id]));\n"
	b := "CREATE TABLE [t1] ([id] int CONSTRAINT [PK__shared__EEEEEEEEEEEEEEEE] PRIMARY KEY ([id]));\n" +
		"CREATE TABLE [t2] ([id] int CONSTRAINT [PK__shared__1111111111111111] PRIMARY KEY ([id]));\n"

	outA := dump.NormalizeMSSQLConstraintNames(a)
	outB := dump.NormalizeMSSQLConstraintNames(b)
	if outA != outB {
		t.Fatalf("tail exchange changed output:\nA:\n%s\nB:\n%s", outA, outB)
	}

	// Position 1 gets the unsuffixed canonical name, position 2 the _2
	// suffix — in both databases.
	canon := regexp.MustCompile(`PK__shared__[0-9A-F]{8}(_2)?`)
	got := canon.FindAllString(outA, -1)
	if len(got) != 2 || got[0] == got[1] || !strings.HasSuffix(got[1], "_2") {
		t.Fatalf("canonical suffix assignment not positional: %v\n%s", got, outA)
	}
	if strings.Contains(outA, "1111111111111111") || strings.Contains(outA, "EEEEEEEEEEEEEEEE") {
		t.Fatalf("random tails survived normalization:\n%s", outA)
	}
}

func TestNormalizeMSSQLConstraintNames_ReservesExistingSuffixedNames(t *testing.T) {
	// Simulate a later migration landing on top of an already-normalized
	// baseline: t1 and t2 carry the canonical names H and H_2 produced by
	// an earlier squash (H is itself auto-name-shaped and re-normalizes
	// to itself; H_2 does not match the auto pattern and must be
	// reserved). A new constraint sharing the prefix must not be handed
	// H_2 — that name is already taken.
	probe := dump.NormalizeMSSQLConstraintNames("CONSTRAINT [PK__shared__AAAAAAAAAAAAAAAA] PRIMARY KEY")
	hCanon := regexp.MustCompile(`PK__shared__[0-9A-F]{8}`).FindString(probe)
	if hCanon == "" {
		t.Fatalf("could not discover canonical name from %q", probe)
	}
	explicit := hCanon + "_2"

	in := "CREATE TABLE [t1] ([id] int CONSTRAINT [" + hCanon + "] PRIMARY KEY ([id]));\n" +
		"CREATE TABLE [t2] ([id] int CONSTRAINT [" + explicit + "] PRIMARY KEY ([id]));\n" +
		"CREATE TABLE [t3] ([id] int CONSTRAINT [PK__shared__BBBBBBBBBBBBBBBB] PRIMARY KEY ([id]));\n"

	out := dump.NormalizeMSSQLConstraintNames(in)

	// The new constraint skips the taken H_2 and lands on H_3.
	if !strings.Contains(out, "["+hCanon+"_3]") {
		t.Errorf("new constraint did not skip reserved %q:\n%s", explicit, out)
	}
	// H_2 still appears exactly once — for t2 only.
	if n := strings.Count(out, explicit); n != 1 {
		t.Errorf("reserved name %q appears %d times, want 1:\n%s", explicit, n, out)
	}
}

func TestNormalizeMSSQLConstraintNames_EscapedDelimiters(t *testing.T) {
	// SQL Server escapes ] as ]] in bracketed identifiers and ' as '' in
	// literals. Names containing those characters must decode before
	// matching and re-escape on write-back, in both quoting forms.
	in := "CREATE TABLE [t] (\n" +
		"  [id] int CONSTRAINT [PK__odd]]name__3213E83F732A0327] PRIMARY KEY ([id]),\n" +
		"  [id2] int CONSTRAINT [PK__odd'name__AAAAAAAAAAAAAAAA] PRIMARY KEY ([id2])\n" +
		");\n" +
		"GO\n" +
		"IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'PK__odd]name__3213E83F732A0327' AND object_id = OBJECT_ID(N't'))\n" +
		"CREATE INDEX [ix1] ON [t] ([id])\n" +
		"GO\n" +
		"IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'PK__odd''name__AAAAAAAAAAAAAAAA' AND object_id = OBJECT_ID(N't2'))\n" +
		"CREATE INDEX [ix2] ON [t] ([id2])\n"

	out := dump.NormalizeMSSQLConstraintNames(in)

	if strings.Contains(out, "3213E83F732A0327") || strings.Contains(out, "AAAAAAAAAAAAAAAA") {
		t.Fatalf("escaped auto-names not normalized:\n%s", out)
	}

	// Both quoting forms of each name must have been renamed to the same
	// canonical tail. nameA decodes to PK__odd]name__<hex>, nameB to
	// PK__odd'name__<hex> — the separator between 'odd' and 'name'
	// differs per quoting form (]]/] vs '/''), the hex tail must not.
	re := regexp.MustCompile(`PK__odd(.{1,2})name__([0-9A-F]{8})`)
	ms := re.FindAllStringSubmatch(out, -1)
	if len(ms) != 4 {
		t.Fatalf("expected 4 canonical occurrences, got %d:\n%s", len(ms), out)
	}
	hexBySep := make(map[string]string)
	for _, m := range ms {
		canonSep := map[string]string{"]": "]", "]]": "]", "'": "'", "''": "'"}[m[1]]
		if prev, ok := hexBySep[canonSep]; ok && prev != m[2] {
			t.Fatalf("quoting forms of %s-name renamed to different tails %s vs %s:\n%s", canonSep, prev, m[2], out)
		}
		hexBySep[canonSep] = m[2]
	}
	if len(hexBySep) != 2 {
		t.Fatalf("expected 2 distinct canonical names, got %v:\n%s", hexBySep, out)
	}

	// Re-escaping on write-back: the ]-name keeps ]] inside brackets but
	// raw ] inside the literal; the '-name keeps raw ' inside brackets
	// but '' inside the literal.
	for _, want := range []string{
		"[PK__odd]]name__", "name = N'PK__odd]name__",
		"[PK__odd'name__", "name = N'PK__odd''name__",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing re-escaped form %q:\n%s", want, out)
		}
	}
}

func TestNormalizeMSSQLConstraintNames_LeavesDataLiterals(t *testing.T) {
	// A quoted literal matching the auto-name shape is only rewritten
	// where it is really a constraint identifier — catalog probes and
	// CONSTRAINT clauses. DEFAULT constants and CHECK comparisons are
	// data and must survive verbatim.
	in := "IF OBJECT_ID(N't', N'U') IS NULL\n" +
		"BEGIN\n" +
		"CREATE TABLE [t] (\n" +
		"  [id] int CONSTRAINT [DF__t__id__11111111] DEFAULT (sysutcdatetime()),\n" +
		"  [payload] nvarchar(50) CONSTRAINT [DF__t__payload__22222222] DEFAULT (N'PK__sentinel__99999999'),\n" +
		"  [name] nvarchar(50) CONSTRAINT [CK__t__name__33333333] CHECK ([name] = N'PK__sentinel__99999999'),\n" +
		"  CONSTRAINT [PK__t__AAAAAAAAAAAAAAAA] PRIMARY KEY ([id])\n" +
		")\n" +
		"END\n" +
		"GO\n" +
		"IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'PK__t__AAAAAAAAAAAAAAAA' AND object_id = OBJECT_ID(N't'))\n" +
		"CREATE INDEX [ix] ON [t] ([id])\n"

	out := dump.NormalizeMSSQLConstraintNames(in)

	// Both data literals are untouched.
	if n := strings.Count(out, "N'PK__sentinel__99999999'"); n != 2 {
		t.Errorf("data literal rewritten or lost, count = %d, want 2:\n%s", n, out)
	}
	// Real constraint names still canonicalized — declaration and the
	// catalog-probe literal agree on the new name.
	if strings.Contains(out, "AAAAAAAAAAAAAAAA") || strings.Contains(out, "11111111") ||
		strings.Contains(out, "22222222") || strings.Contains(out, "33333333") {
		t.Errorf("auto-name tails survived normalization:\n%s", out)
	}
	if !strings.Contains(out, "name = N'PK__t__") {
		t.Errorf("catalog probe literal not rewritten consistently:\n%s", out)
	}
}

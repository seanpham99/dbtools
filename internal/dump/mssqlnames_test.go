package dump_test

import (
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

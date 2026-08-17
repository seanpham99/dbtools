package ddlcheck

import "testing"

func TestExtractObjects_CreateTableBracketed(t *testing.T) {
	sql := `CREATE TABLE [app].[widget_order] (
    [widget_order_id] BIGINT IDENTITY (1, 1) NOT NULL
);`
	got := ExtractObjects(sql)
	if len(got) != 1 {
		t.Fatalf("ExtractObjects() = %+v, want 1 object", got)
	}
	want := ObjectRef{Schema: "app", Name: "widget_order", Kind: "table"}
	if got[0] != want {
		t.Errorf("ExtractObjects()[0] = %+v, want %+v", got[0], want)
	}
}

func TestExtractObjects_CreateOrAlterProcedureBareSchema(t *testing.T) {
	sql := `CREATE OR ALTER PROCEDURE app.usp_stage_widget_order
    @json_payload NVARCHAR(MAX)
AS
BEGIN
    SET NOCOUNT ON;
END;`
	got := ExtractObjects(sql)
	if len(got) != 1 {
		t.Fatalf("ExtractObjects() = %+v, want 1 object", got)
	}
	want := ObjectRef{Schema: "app", Name: "usp_stage_widget_order", Kind: "procedure"}
	if got[0] != want {
		t.Errorf("ExtractObjects()[0] = %+v, want %+v", got[0], want)
	}
}

func TestExtractObjects_NoSchemaDefaultsToDbo(t *testing.T) {
	sql := `CREATE PROCEDURE dbtools_test_proc_a
AS
BEGIN
    SELECT 1;
END;`
	got := ExtractObjects(sql)
	if len(got) != 1 {
		t.Fatalf("ExtractObjects() = %+v, want 1 object", got)
	}
	want := ObjectRef{Schema: "dbo", Name: "dbtools_test_proc_a", Kind: "procedure"}
	if got[0] != want {
		t.Errorf("ExtractObjects()[0] = %+v, want %+v", got[0], want)
	}
}

func TestExtractObjects_MultipleObjectsGoSeparated(t *testing.T) {
	sql := `CREATE PROCEDURE dbtools_test_proc_a
AS
BEGIN
    DECLARE @payload INT = 1;
    SELECT @payload;
END;
GO
CREATE PROCEDURE dbtools_test_proc_b
AS
BEGIN
    DECLARE @payload INT = 2;
    SELECT @payload;
END;
GO
`
	got := ExtractObjects(sql)
	if len(got) != 2 {
		t.Fatalf("ExtractObjects() = %+v, want 2 objects", got)
	}
	if got[0].Name != "dbtools_test_proc_a" || got[1].Name != "dbtools_test_proc_b" {
		t.Errorf("ExtractObjects() = %+v, want proc_a then proc_b", got)
	}
}

func TestExtractObjects_ViewAndFunction(t *testing.T) {
	sql := `CREATE OR ALTER VIEW app.v_widget_summary AS SELECT 1 AS x;
GO
CREATE FUNCTION app.fn_widget_offset (@d DATE) RETURNS DATE AS
BEGIN
    RETURN @d;
END;`
	got := ExtractObjects(sql)
	if len(got) != 2 {
		t.Fatalf("ExtractObjects() = %+v, want 2 objects", got)
	}
	if got[0] != (ObjectRef{Schema: "app", Name: "v_widget_summary", Kind: "view"}) {
		t.Errorf("ExtractObjects()[0] = %+v, want view app.v_widget_summary", got[0])
	}
	if got[1] != (ObjectRef{Schema: "app", Name: "fn_widget_offset", Kind: "function"}) {
		t.Errorf("ExtractObjects()[1] = %+v, want function app.fn_widget_offset", got[1])
	}
}

func TestExtractObjects_AlterTableAddColumnNotExtracted(t *testing.T) {
	sql := `ALTER TABLE app.widget_order
    ADD widget_amount DECIMAL(19, 6) NULL;`
	got := ExtractObjects(sql)
	if len(got) != 0 {
		t.Errorf("ExtractObjects() = %+v, want 0 objects (ALTER introduces no new named object)", got)
	}
}

func TestExtractObjects_CreateIndexNotExtracted(t *testing.T) {
	sql := `CREATE NONCLUSTERED INDEX [IX_app_widget_order_code_date]
    ON [app].[widget_order]([code] ASC, [created_date] ASC);`
	got := ExtractObjects(sql)
	if len(got) != 0 {
		t.Errorf("ExtractObjects() = %+v, want 0 objects (indexes are out of scope)", got)
	}
}

func TestExtractDroppedObjects_DropTableBracketed(t *testing.T) {
	sql := `DROP TABLE [app].[widget_order];`
	got := ExtractDroppedObjects(sql)
	if len(got) != 1 {
		t.Fatalf("ExtractDroppedObjects() = %+v, want 1 object", got)
	}
	want := ObjectRef{Schema: "app", Name: "widget_order", Kind: "table"}
	if got[0] != want {
		t.Errorf("ExtractDroppedObjects()[0] = %+v, want %+v", got[0], want)
	}
}

func TestExtractDroppedObjects_GuardedDropWithObjectIDCheck(t *testing.T) {
	sql := "IF OBJECT_ID('[dbo].[legacy_widget_tracking]', 'U') IS NOT NULL\n    DROP TABLE [dbo].[legacy_widget_tracking];"
	got := ExtractDroppedObjects(sql)
	if len(got) != 1 {
		t.Fatalf("ExtractDroppedObjects() = %+v, want 1 object", got)
	}
	want := ObjectRef{Schema: "dbo", Name: "legacy_widget_tracking", Kind: "table"}
	if got[0] != want {
		t.Errorf("ExtractDroppedObjects()[0] = %+v, want %+v", got[0], want)
	}
}

func TestExtractDroppedObjects_DropProcedureViewFunction(t *testing.T) {
	sql := `DROP PROCEDURE app.usp_stage_widget_order;
GO
DROP VIEW app.v_widget_summary;
GO
DROP FUNCTION app.fn_widget_offset;`
	got := ExtractDroppedObjects(sql)
	if len(got) != 3 {
		t.Fatalf("ExtractDroppedObjects() = %+v, want 3 objects", got)
	}
	if got[0] != (ObjectRef{Schema: "app", Name: "usp_stage_widget_order", Kind: "procedure"}) {
		t.Errorf("ExtractDroppedObjects()[0] = %+v, want procedure app.usp_stage_widget_order", got[0])
	}
	if got[1] != (ObjectRef{Schema: "app", Name: "v_widget_summary", Kind: "view"}) {
		t.Errorf("ExtractDroppedObjects()[1] = %+v, want view app.v_widget_summary", got[1])
	}
	if got[2] != (ObjectRef{Schema: "app", Name: "fn_widget_offset", Kind: "function"}) {
		t.Errorf("ExtractDroppedObjects()[2] = %+v, want function app.fn_widget_offset", got[2])
	}
}

func TestExtractDroppedObjects_CreateTableAloneProducesZero(t *testing.T) {
	sql := `CREATE TABLE [app].[widget_order] (
    [widget_order_id] BIGINT IDENTITY (1, 1) NOT NULL
);`
	got := ExtractDroppedObjects(sql)
	if len(got) != 0 {
		t.Errorf("ExtractDroppedObjects() = %+v, want 0 objects (no DROP statement present)", got)
	}
}

func TestExtractDroppedObjects_NoSchemaDefaultsToDbo(t *testing.T) {
	sql := `DROP TABLE dbtools_test_proc_a;`
	got := ExtractDroppedObjects(sql)
	if len(got) != 1 {
		t.Fatalf("ExtractDroppedObjects() = %+v, want 1 object", got)
	}
	want := ObjectRef{Schema: "dbo", Name: "dbtools_test_proc_a", Kind: "table"}
	if got[0] != want {
		t.Errorf("ExtractDroppedObjects()[0] = %+v, want %+v", got[0], want)
	}
}

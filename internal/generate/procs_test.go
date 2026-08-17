package generate

import "testing"

const ordersProcBody = `CREATE PROCEDURE sales.usp_stage_orders
    @json_payload NVARCHAR(MAX)
AS
BEGIN
    SET NOCOUNT ON;
    SET XACT_ABORT ON;
    DECLARE @merge_actions TABLE (ActionName NVARCHAR(10) NOT NULL);

    IF @json_payload IS NULL OR ISJSON(@json_payload) <> 1
    BEGIN
        THROW 50008, 'Invalid JSON payload for sales.usp_stage_orders.', 1;
    END;

    WITH src AS (
        SELECT
            TRY_CONVERT(BIGINT, customer_key)        AS customer_key,
            [status],
            TRY_CONVERT(DATE, order_date)            AS order_date,
            TRY_CONVERT(DECIMAL(19, 6), total_amount) AS total_amount,
            TRY_CONVERT(DECIMAL(19, 6), discount_rate) AS discount_rate,
            TRY_CONVERT(BIGINT, batch_id)           AS batch_id
        FROM OPENJSON(@json_payload) WITH (
            customer_key        NVARCHAR(20)  '$.customer_key',
            [status]            NVARCHAR(8)   '$.status',
            order_date          NVARCHAR(20)  '$.order_date',
            total_amount        NVARCHAR(30)  '$.total_amount',
            discount_rate       NVARCHAR(30)  '$.discount_rate',
            batch_id            NVARCHAR(40)  '$.batch_id'
        )
    )
    SELECT 1;
END;`

const metricsStagingProcBody = `CREATE PROCEDURE staging.usp_stage_daily_metrics
    @json_payload NVARCHAR(MAX)
AS
BEGIN
    SET NOCOUNT ON;
    SET XACT_ABORT ON;
    IF @json_payload IS NULL
        OR TRY_CONVERT(NVARCHAR(MAX), @json_payload) IS NULL
        OR ISJSON(@json_payload) <> 1 BEGIN
    THROW 50009,
'Invalid JSON payload for staging.usp_stage_daily_metrics.',
1;
END;
;
WITH
    src
    AS
    (
        SELECT UPPER(LTRIM(RTRIM([symbol]))) AS Symbol,
            [category] AS [Category],
            TRY_CONVERT(DATE, metricDate) AS MetricDate,
            TRY_CONVERT(DECIMAL(7, 2), [value]) AS [Value],
            TRY_CONVERT(BIGINT, count) AS count,
            TRY_CONVERT(BIGINT, batch_id) AS batch_id,
            TRY_CONVERT(DATETIME2(0), COALESCE(loaded_at, loadedAtUtc)) AS loaded_at,
            COALESCE(source_file_name, sourceFileName) AS source_file_name
        FROM OPENJSON(@json_payload) WITH (
            [symbol] NVARCHAR(20) '$.symbol',
            [category] NVARCHAR(8) '$.category',
            metricDate NVARCHAR(20) '$.metricDate',
            [value] NVARCHAR(40) '$.value',
            count NVARCHAR(40) '$.count',
            batch_id NVARCHAR(40) '$.batch_id',
            loaded_at NVARCHAR(40) '$.loaded_at',
            loadedAtUtc NVARCHAR(40) '$.loadedAtUtc',
            source_file_name NVARCHAR(260) '$.source_file_name',
            sourceFileName NVARCHAR(260) '$.sourceFileName'
        )
    )
    SELECT 1;
END;`

func TestExtractOpenJSONContractCrossReferencesTryConvert(t *testing.T) {
	params := ExtractOpenJSONContract(ordersProcBody)

	got := make(map[string]string, len(params))
	for _, p := range params {
		got[p.Name] = p.PythonType
	}

	want := map[string]string{
		"customer_key":  "int",      // TRY_CONVERT(BIGINT, customer_key) found -> cross-referenced
		"status":        "str",      // no TRY_CONVERT for status -> falls back to declared NVARCHAR
		"order_date":    "datetime", // TRY_CONVERT(DATE, order_date) found
		"total_amount":  "Decimal",
		"discount_rate": "Decimal",
		"batch_id":      "int",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d fields, want %d: %v", len(got), len(want), got)
	}
	for name, wantType := range want {
		if got[name] != wantType {
			t.Errorf("field %q: got PythonType %q, want %q", name, got[name], wantType)
		}
	}
}

func TestExtractOpenJSONContractKeepsLegacyKeyAliases(t *testing.T) {
	// Proc accepts two JSON key spellings for the same concept (loaded_at/loadedAtUtc, source_file_name/sourceFileName)
	// — both must survive as distinct fields since a caller could send either.
	params := ExtractOpenJSONContract(metricsStagingProcBody)

	names := make(map[string]bool, len(params))
	for _, p := range params {
		names[p.Name] = true
	}

	for _, want := range []string{
		"symbol", "category", "metricDate", "value", "count", "batch_id",
		"loaded_at", "loadedAtUtc", "source_file_name", "sourceFileName",
	} {
		if !names[want] {
			t.Errorf("expected field %q in extracted contract, got %v", want, names)
		}
	}
	if len(params) != 10 {
		t.Errorf("got %d fields, want 10: %v", len(params), names)
	}
}

func TestExtractOpenJSONContractKnownGapCoalescedTryConvert(t *testing.T) {
	// Known limitation, documented rather than silently wrong: TRY_CONVERT(type,
	// COALESCE(a, b)) doesn't match the simple TRY_CONVERT(type, column) regex,
	// so loaded_at falls back to its WITH-declared NVARCHAR -> str, even though
	// the proc actually converts it to DATETIME2 after the COALESCE resolves.
	params := ExtractOpenJSONContract(metricsStagingProcBody)

	for _, p := range params {
		if p.Name == "loaded_at" && p.PythonType != "str" {
			t.Errorf("expected known-gap fallback str for COALESCE-wrapped TRY_CONVERT, got %q", p.PythonType)
		}
	}
}

func TestExtractOpenJSONContractReturnsNilForNonJSONProc(t *testing.T) {
	body := `CREATE PROCEDURE dbo.usp_sync_categories AS BEGIN SELECT 1; END;`
	if params := ExtractOpenJSONContract(body); params != nil {
		t.Errorf("expected nil for a proc with no OPENJSON, got %v", params)
	}
}

const multiOpenJSONProcBody = `CREATE PROCEDURE staging.usp_stage_export_batch
    @json_payload NVARCHAR(MAX)
AS
BEGIN
    SET NOCOUNT ON;
    SET XACT_ABORT ON;

    IF ISJSON(@json_payload) <> 1
        THROW 50002, 'Invalid JSON payload for staging.usp_stage_export_batch.', 1;

    DECLARE @reporting_period DATE;
    SELECT TOP 1 @reporting_period = CAST(reporting_period AS DATE)
    FROM OPENJSON(@json_payload) WITH (reporting_period NVARCHAR(50));

    IF @reporting_period IS NOT NULL
    BEGIN
        DELETE FROM staging.export_batch WHERE reporting_period = @reporting_period;
    END

    INSERT INTO staging.export_batch
        (reporting_period, line_no, product_name, unit, monthly_volume,
         monthly_value, ytd_volume, ytd_value,
         batch_id, loaded_at, source_file_name)
    SELECT
        reporting_period, line_no, product_name, unit, monthly_volume,
        monthly_value, ytd_volume, ytd_value,
        batch_id,
        COALESCE(TRY_CONVERT(DATETIME2(7), loaded_at, 126), SYSUTCDATETIME()),
        source_file_name
    FROM OPENJSON(@json_payload)
    WITH (
        reporting_period            DATE '$.reporting_period',
        line_no                     NVARCHAR(50) '$.line_no',
        product_name                NVARCHAR(255) '$.product_name',
        unit                        NVARCHAR(50) '$.unit',
        monthly_volume              DECIMAL(19,6) '$.monthly_volume',
        monthly_value               DECIMAL(19,6) '$.monthly_value',
        ytd_volume                  DECIMAL(19,6) '$.ytd_volume',
        ytd_value                   DECIMAL(19,6) '$.ytd_value',
        batch_id                    BIGINT '$.batch_id',
        loaded_at                   NVARCHAR(50) '$.loaded_at',
        source_file_name            NVARCHAR(260) '$.source_file_name'
    );
END;`

func TestExtractOpenJSONContractMultipleWithBlocksKeepsLargest(t *testing.T) {
	params := ExtractOpenJSONContract(multiOpenJSONProcBody)
	if len(params) != 11 {
		t.Fatalf("got %d params, want 11 for main OPENJSON block", len(params))
	}
	wantFirst := "reporting_period"
	if params[0].Name != wantFirst {
		t.Errorf("first param: got %q, want %q", params[0].Name, wantFirst)
	}
}

const jsonValueProcBody = `CREATE PROCEDURE staging.usp_stage_company_profile
    @json_payload NVARCHAR(MAX)
AS
BEGIN
    SET NOCOUNT ON;
    SET XACT_ABORT ON;

    IF ISJSON(@json_payload) <> 1
        THROW 50001, 'Invalid JSON payload for staging.usp_stage_company_profile.', 1;

    WITH src AS (
        SELECT
            TRY_CAST(JSON_VALUE(j.value, '$.organizationId') AS INT) AS organization_id,
            UPPER(LTRIM(RTRIM(JSON_VALUE(j.value, '$.code')))) AS code,
            JSON_VALUE(j.value, '$.taxCode') AS tax_code,
            JSON_VALUE(j.value, '$.organizationName') AS organization_name,
            JSON_VALUE(j.value, '$.regionCode') AS region_code,
            JSON_VALUE(j.value, '$.sector') AS sector,
            TRY_CAST(JSON_VALUE(j.value, '$.categoryId') AS INT) AS category_id,
            TRY_CAST(JSON_VALUE(j.value, '$.batch_id') AS BIGINT) AS batch_id,
            TRY_CAST(COALESCE(JSON_VALUE(j.value, '$.loaded_at'), JSON_VALUE(j.value, '$.loadedAtUtc')) AS DATETIME2(0)) AS loaded_at,
            COALESCE(JSON_VALUE(j.value, '$.source_file_name'), JSON_VALUE(j.value, '$.sourceFileName')) AS source_file_name
        FROM OPENJSON(@json_payload) AS j
    )
    SELECT 1;
END;`

func TestExtractOpenJSONContractJSONValueFallback(t *testing.T) {
	params := ExtractOpenJSONContract(jsonValueProcBody)
	got := make(map[string]string, len(params))
	for _, p := range params {
		got[p.Name] = p.PythonType
	}

	want := map[string]string{
		"organizationId":   "int",
		"code":             "str",
		"taxCode":          "str",
		"organizationName": "str",
		"regionCode":       "str",
		"sector":           "str",
		"categoryId":       "int",
		"batch_id":         "int",
		"loaded_at":        "datetime",
		"loadedAtUtc":      "datetime",
		"source_file_name": "str",
		"sourceFileName":   "str",
	}

	if len(got) != len(want) {
		t.Fatalf("got %d fields, want %d: %v", len(got), len(want), got)
	}
	for name, wantType := range want {
		if got[name] != wantType {
			t.Errorf("field %q: got PythonType %q, want %q", name, got[name], wantType)
		}
	}
}

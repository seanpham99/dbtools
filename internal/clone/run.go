package clone

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/generate"
)

// Options configures one Run call.
type Options struct {
	// Mask enables the default-deny sensitive-column masking (see
	// mask.go). false is the explicit --no-mask opt-out.
	Mask bool
	// Limit caps the rows copied per table. 0 means no limit.
	Limit int
	// Where, when non-empty, is appended as-is to every table's SELECT
	// (WHERE <Where>) — an escape hatch for subsetting, trusted the same
	// way migration SQL is trusted elsewhere in dbtools.
	Where string
}

// TableResult is one table's clone outcome.
type TableResult struct {
	Table         string   `json:"table"`
	RowsCopied    int      `json:"rows_copied"`
	MaskedColumns []string `json:"masked_columns,omitempty"`
}

// Result is the full outcome of one Run call.
type Result struct {
	Source string        `json:"source"`
	Dest   string        `json:"dest"`
	Tables []TableResult `json:"tables"`
}

// Run copies every table sourceTarget's engine reports via Introspect into
// destTarget, clearing each dest table first. sourceTarget and destTarget
// must resolve to the same engine (see the design note in this plan's
// Global Constraints — cross-dialect clone is out of scope) and must be
// different targets.
func Run(cfg *config.Config, sourceTarget, destTarget string, opts Options) (*Result, error) {
	if sourceTarget == destTarget {
		return nil, fmt.Errorf("clone source and dest must be different targets (both are %q)", sourceTarget)
	}

	sourceURL, err := cfg.ResolveURL(sourceTarget)
	if err != nil {
		return nil, fmt.Errorf("source target %q: %w", sourceTarget, err)
	}
	destURL, err := cfg.ResolveURL(destTarget)
	if err != nil {
		return nil, fmt.Errorf("dest target %q: %w", destTarget, err)
	}

	sourceEng, err := engine.ForTarget(cfg.EngineName(sourceTarget), sourceURL)
	if err != nil {
		return nil, fmt.Errorf("source target %q: %w", sourceTarget, err)
	}
	destEng, err := engine.ForTarget(cfg.EngineName(destTarget), destURL)
	if err != nil {
		return nil, fmt.Errorf("dest target %q: %w", destTarget, err)
	}
	if sourceEng.Name() != destEng.Name() {
		return nil, fmt.Errorf("clone requires source and dest to use the same engine (source %q is %q, dest %q is %q)", sourceTarget, sourceEng.Name(), destTarget, destEng.Name())
	}

	sourceDB, err := sourceEng.Open(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("opening source %q: %w", sourceTarget, err)
	}
	defer sourceDB.Close()

	destDB, err := destEng.Open(destURL)
	if err != nil {
		return nil, fmt.Errorf("opening dest %q: %w", destTarget, err)
	}
	defer destDB.Close()

	exclude := append(append([]string{}, cfg.Generate.Exclude...), cfg.Clone.Exclude...)
	tables, _, err := sourceEng.Introspect(sourceDB, exclude)
	if err != nil {
		return nil, fmt.Errorf("introspecting source %q: %w", sourceTarget, err)
	}

	result := &Result{Source: sourceTarget, Dest: destTarget}
	for _, tbl := range tables {
		tr, err := copyTable(sourceDB, destDB, sourceEng.Name(), tbl, cfg.Clone.Mask, opts)
		if err != nil {
			return nil, fmt.Errorf("cloning table %s: %w", tbl.Name, err)
		}
		result.Tables = append(result.Tables, *tr)
	}
	return result, nil
}

// copyTable clears tbl in destDB, then copies every row (matching opts'
// Limit/Where) from sourceDB, masking columns per plan when opts.Mask.
func copyTable(sourceDB, destDB *sql.DB, engineName string, tbl generate.TableSchema, maskConfig map[string]string, opts Options) (*TableResult, error) {
	colNames := make([]string, len(tbl.Columns))
	for i, c := range tbl.Columns {
		colNames[i] = c.Name
	}

	plan := map[string]MaskStrategy{}
	if opts.Mask {
		plan = maskPlanFor(colNames, maskConfig)
	}

	tr := &TableResult{Table: tbl.Name}
	for name := range plan {
		tr.MaskedColumns = append(tr.MaskedColumns, name)
	}
	sort.Strings(tr.MaskedColumns)

	selectSQL := buildSelectSQL(engineName, tbl.Name, opts.Limit, opts.Where)
	rows, err := sourceDB.Query(selectSQL)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", tbl.Name, err)
	}
	defer rows.Close()

	if _, err := destDB.Exec(fmt.Sprintf("DELETE FROM %s", tbl.Name)); err != nil {
		return nil, fmt.Errorf("clearing dest %s: %w", tbl.Name, err)
	}

	insertSQL := buildInsertSQL(engineName, tbl.Name, colNames)
	counters := map[string]int{}

	for rows.Next() {
		values := make([]any, len(colNames))
		ptrs := make([]any, len(colNames))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scanning row from %s: %w", tbl.Name, err)
		}
		for i, colName := range colNames {
			if strat, ok := plan[colName]; ok {
				values[i] = applyMask(strat, values[i], counters, colName)
			}
		}
		if _, err := destDB.Exec(insertSQL, values...); err != nil {
			return nil, fmt.Errorf("inserting row into %s: %w", tbl.Name, err)
		}
		tr.RowsCopied++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows from %s: %w", tbl.Name, err)
	}
	return tr, nil
}

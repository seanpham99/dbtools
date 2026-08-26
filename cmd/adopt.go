package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/seanpham99/dbtools/internal/adopt"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/ddlcheck"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/spf13/cobra"
)

var (
	adoptYes             bool
	adoptForce           bool
	adoptFromTable       string
	adoptVersionColumn   string
	adoptAppliedAtColumn string
)

var adoptCmd = &cobra.Command{
	Use:   "adopt [target]",
	Short: "Import an existing migration ledger from another tool into dbtools",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if adoptFromTable != "" && adoptVersionColumn == "" {
			return fmt.Errorf("--from-table requires --version-column")
		}
		return runAdopt(args[0])
	},
}

func init() {
	adoptCmd.Flags().BoolVar(&adoptYes, "yes", false, "write the imported rows to the ledger (omit to only print the plan)")
	adoptCmd.Flags().BoolVar(&adoptForce, "force", false, "proceed even if orphan history rows exist (rows with no matching file)")
	adoptCmd.Flags().StringVar(&adoptFromTable, "from-table", "", "bespoke source table name (auto-detected from a known list if omitted)")
	adoptCmd.Flags().StringVar(&adoptVersionColumn, "version-column", "", "column in --from-table holding the migration version")
	adoptCmd.Flags().StringVar(&adoptAppliedAtColumn, "applied-at-column", "", "optional column in --from-table holding the applied timestamp")
	rootCmd.AddCommand(adoptCmd)
}

func tableExists(eng engine.Engine, db *sql.DB, tableName string) (bool, error) {
	schema := ""
	switch eng.Name() {
	case "postgres":
		schema = "public"
	case "sqlite":
		schema = "main"
	case "mssql":
		schema = "dbo"
	case "mysql":
		schema = ""
	}
	return eng.DDL().Exists(db, ddlcheck.ObjectRef{Schema: schema, Name: tableName, Kind: "table"})
}

func runAdopt(targetName string) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	url, err := cfg.ResolveURLOrFlag(targetName, "")
	if err != nil {
		return err
	}

	eng, err := engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return err
	}

	db, err := eng.Open(url)
	if err != nil {
		return err
	}
	defer db.Close()

	migrationsDir, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table)

	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return err
	}

	table := adoptFromTable
	versionCol := adoptVersionColumn
	appliedAtCol := adoptAppliedAtColumn

	if table == "" {
		existsFunc := func(_ ledger.DBTX, name string) (bool, error) {
			return tableExists(eng, db, name)
		}
		table, err = adopt.DetectSourceTable(db, existsFunc, adopt.KnownTableNames())
		if err != nil {
			return err
		}
		defVer, defApp := adopt.DefaultColumnsForTable(table)
		if versionCol == "" {
			versionCol = defVer
		}
		if appliedAtCol == "" {
			appliedAtCol = defApp
		}
	}

	sourceRows, err := adopt.ReadSourceRows(db, table, versionCol, appliedAtCol)
	if err != nil {
		return err
	}

	plan := adopt.BuildPlan(table, sourceRows, dir)
	printAdoptPlan(plan)

	if len(plan.Orphan) > 0 && !adoptForce {
		return &ExitCodeError{
			Code:    1,
			Message: fmt.Sprintf("adopt found %d orphan history row(s) with no matching migration file — review them, then pass --force to proceed anyway", len(plan.Orphan)),
		}
	}

	// A pending version below the highest matched one would become
	// permanently unreachable: PendingAfter only returns versions above
	// the stamped cursor, so stamping past it would silently strand that
	// migration's file forever. Checked before any write, like the
	// orphan gate above.
	if len(plan.Matched) > 0 {
		highest := plan.Matched[len(plan.Matched)-1]
		for _, p := range plan.Pending {
			if p <= highest {
				return fmt.Errorf("adopt found pending version %d below the highest matched version %d — stamping the cursor past it would make it permanently unreachable; apply or remove that migration file first", p, highest)
			}
		}
	}

	if !adoptYes {
		logger.Infof("dry run — pass --yes to write %d matched version(s) to the ledger", len(plan.Matched))
		return nil
	}

	if err := requireUnprotected(cfg, targetName); err != nil {
		return err
	}

	m, err := migrator.Open(url, migrationsDir)
	if err != nil {
		return err
	}
	defer m.Close()

	// EnsureSchema only, never Sync: Sync also backfills a row for every
	// version the golang-migrate cursor already considers applied, tagged
	// with the normal (non-adopted) hash source — exactly the "hash now,
	// treat as verified" outcome adopt's hash_source design exists to
	// avoid. adopt writes only the matched set below, all tagged adopted.
	if err := eng.Ledger().EnsureSchema(db, ledgerTable); err != nil {
		return err
	}

	for _, v := range plan.Matched {
		hash, _ := dir.ContentHash(v)
		if err := eng.Ledger().SetStatusAdopted(db, v, "adopted from "+table, hash, ledgerTable); err != nil {
			return err
		}
	}

	if len(plan.Matched) > 0 {
		highest := plan.Matched[len(plan.Matched)-1]
		if err := m.Stamp(highest); err != nil {
			return err
		}
	}

	logger.Infof("adopted %d version(s) from %s", len(plan.Matched), table)
	return nil
}

func printAdoptPlan(plan adopt.Plan) {
	if jsonOutput {
		b, _ := json.Marshal(plan)
		fmt.Println(string(b))
		return
	}
	logger.Infof("source table: %s", plan.SourceTable)
	logger.Infof("matched: %v", plan.Matched)
	logger.Infof("pending (no source row): %v", plan.Pending)
	logger.Infof("orphan (no file): %v", plan.Orphan)
}

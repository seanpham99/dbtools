package testdb

import (
	"github.com/seanpham99/dbtools/internal/engine/mssqlengine"
)

// ResetTracking drops golang-migrate's version-tracking table and the
// migration ledger, so an integration test starts from a clean slate
// regardless of what any other test left behind in the same shared
// database — both tables live in the target database, not scoped per
// migrations-directory.
func ResetTracking(rawURL string) error {
	db, err := mssqlengine.Open(rawURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE IF EXISTS schema_migrations"); err != nil {
		return err
	}
	_, err = db.Exec("DROP TABLE IF EXISTS dbtools_migration_history")
	return err
}

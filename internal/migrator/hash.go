package migrator

// ContentHash returns the hex SHA-256 of a migration file's contents. It
// is what the ledger stores when a migration is applied, so verify can
// detect an applied migration being edited after the fact — the most
// common real-world schema drift.
func ContentHash(migrationsDir, upSuffix string, version uint64) (string, error) {
	d, err := ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return "", err
	}
	return d.ContentHash(version)
}

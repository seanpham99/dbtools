package seed

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/dbtools/dbtools/internal/dbconn"
)

const Filename = "seed.sql"

var goSeparator = regexp.MustCompile(`(?im)^\s*GO\s*$`)

func splitBatches(sqlText string) []string {
	var batches []string
	for _, batch := range goSeparator.Split(sqlText, -1) {
		batch = strings.TrimSpace(batch)
		if batch == "" {
			continue
		}
		batches = append(batches, batch)
	}
	return batches
}

func Run(databaseURL string) error {
	data, err := os.ReadFile(Filename)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", Filename, err)
	}

	db, err := dbconn.Open(databaseURL)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	for _, batch := range splitBatches(string(data)) {
		if _, err := db.Exec(batch); err != nil {
			return fmt.Errorf("executing %s batch: %w", Filename, err)
		}
	}
	return nil
}

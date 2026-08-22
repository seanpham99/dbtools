//go:build integration

package mssqlengine

import (
	"os"
	"testing"

	"github.com/seanpham99/dbtools/internal/testutil"
)

func TestIntegrationAssets(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	testutil.RunAssets(t, "mssql", rawURL)
}

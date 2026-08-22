//go:build integration

package postgresengine

import (
	"os"
	"testing"

	"github.com/seanpham99/dbtools/internal/testutil"
)

func TestIntegrationAssets(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_POSTGRES_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_POSTGRES_URL not set, skipping integration test")
	}
	testutil.RunAssets(t, "postgres", rawURL)
}

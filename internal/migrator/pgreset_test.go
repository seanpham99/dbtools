package migrator

import (
	"io"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4/database"
)

// fakeResetInner records what Run received so the wrapper's prepend
// behaviour is testable without a live postgres.
type fakeResetInner struct {
	lastMigration string
}

func (f *fakeResetInner) Open(string) (database.Driver, error) { return f, nil }
func (f *fakeResetInner) Close() error                         { return nil }
func (f *fakeResetInner) Lock() error                          { return nil }
func (f *fakeResetInner) Unlock() error                        { return nil }
func (f *fakeResetInner) SetVersion(int, bool) error           { return nil }
func (f *fakeResetInner) Version() (int, bool, error)          { return 0, false, nil }
func (f *fakeResetInner) Drop() error                          { return nil }
func (f *fakeResetInner) Run(r io.Reader) error {
	raw, _ := io.ReadAll(r)
	f.lastMigration = string(raw)
	return nil
}

// TestPgResetDriver_PrependsSessionReset is the R6/C5 regression: every
// migration must run with a clean search_path/client_min_messages, so a
// pg_dump baseline's session-scoped SETs can't poison later migrations.
func TestPgResetDriver_PrependsSessionReset(t *testing.T) {
	inner := &fakeResetInner{}
	d := &pgResetDriver{inner: inner}

	if err := d.Run(strings.NewReader("CREATE TABLE t (id int);")); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if !strings.HasPrefix(inner.lastMigration, "SET search_path TO public; RESET client_min_messages;") {
		t.Fatalf("Run() did not prepend the session reset; got %q", inner.lastMigration)
	}
	if !strings.Contains(inner.lastMigration, "CREATE TABLE t (id int);") {
		t.Fatalf("Run() dropped the migration body; got %q", inner.lastMigration)
	}
}

// TestPgResetDriver_DelegatesTheRest: the wrapper must pass through the
// driver bookkeeping untouched.
func TestPgResetDriver_DelegatesTheRest(t *testing.T) {
	inner := &fakeResetInner{}
	d := &pgResetDriver{inner: inner}

	if err := d.SetVersion(3, true); err != nil {
		t.Errorf("SetVersion() returned error: %v", err)
	}
	v, dirty, err := d.Version()
	if err != nil || v != 0 || dirty {
		t.Errorf("Version() = (%d, %v, %v), want (0, false, nil)", v, dirty, err)
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
	if err := d.Lock(); err != nil {
		t.Errorf("Lock() returned error: %v", err)
	}
	if err := d.Unlock(); err != nil {
		t.Errorf("Unlock() returned error: %v", err)
	}
	if err := d.Drop(); err != nil {
		t.Errorf("Drop() returned error: %v", err)
	}
}

func TestOpenPostgresResetDriver_BadURL(t *testing.T) {
	_, err := openPostgresResetDriver("postgres://invalid:port:is:bad")
	if err == nil {
		t.Fatalf("openPostgresResetDriver() expected error for invalid URL, got nil")
	}
}

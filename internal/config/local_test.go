package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadDoc(t *testing.T, body string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dbtools.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	return cfg
}

// A nil [local] port is what lets the deprecated [container] port be
// recognized as the active value; an explicit 0 must be distinguishable so
// a project can unpin a port the old key still carries.
func TestLocalPort_PointerDistinguishesUnsetFromZero(t *testing.T) {
	unset := loadDoc(t, "[targets.local]\nurl_env = \"X\"\n")
	if unset.Local.Port != nil {
		t.Fatalf("Local.Port = %v, want nil when the key is absent", *unset.Local.Port)
	}
	if port, deprecated := unset.LocalPort(); port != 0 || deprecated {
		t.Fatalf("LocalPort() = (%d, %v), want (0, false)", port, deprecated)
	}

	zero := loadDoc(t, "[local]\nport = 0\n[targets.local]\nurl_env = \"X\"\n")
	if zero.Local.Port == nil {
		t.Fatal("Local.Port = nil, want a pointer to 0: an explicit 0 must be distinguishable from unset")
	}
	if *zero.Local.Port != 0 {
		t.Fatalf("Local.Port = %d, want 0", *zero.Local.Port)
	}
}

func TestLocalPort_LocalKeyWinsOverDeprecatedContainerKey(t *testing.T) {
	cfg := loadDoc(t, "[local]\nport = 55432\n[container]\nport = 1234\n[targets.local]\nurl_env = \"X\"\n")
	port, deprecated := cfg.LocalPort()
	if port != 55432 {
		t.Fatalf("LocalPort() = %d, want 55432", port)
	}
	if deprecated {
		t.Fatal("LocalPort() reported the deprecated key even though [local] port was set")
	}
}

func TestLocalPort_DeprecatedContainerKeyIsReported(t *testing.T) {
	cfg := loadDoc(t, "[container]\nport = 55432\n[targets.local]\nurl_env = \"X\"\n")
	port, deprecated := cfg.LocalPort()
	if port != 55432 {
		t.Fatalf("LocalPort() = %d, want 55432", port)
	}
	if !deprecated {
		t.Fatal("LocalPort() did not report the deprecated [container] port key")
	}
}

// The natural spelling of a version is a bare integer; both that and the
// quoted form must decode, or a user writing postgres_major = 17 would get
// "unset" and the silent provider default the setting exists to remove.
func TestPostgresMajor_AcceptsIntegerAndString(t *testing.T) {
	for _, body := range []string{
		"[local]\npostgres_major = 17\n[targets.local]\nurl_env = \"X\"\n",
		"[local]\npostgres_major = \"17\"\n[targets.local]\nurl_env = \"X\"\n",
	} {
		if got := loadDoc(t, body).Local.PostgresMajor; got != "17" {
			t.Fatalf("postgres_major decoded as %q, want %q", got, "17")
		}
	}
}

func TestPostgresMajor_UnsetIsEmpty(t *testing.T) {
	if got := loadDoc(t, "[targets.local]\nurl_env = \"X\"\n").Local.PostgresMajor; got != "" {
		t.Fatalf("postgres_major = %q, want empty when unset", got)
	}
}

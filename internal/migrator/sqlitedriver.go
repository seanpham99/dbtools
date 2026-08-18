package migrator

// golang-migrate's sqlite driver (modernc.org/sqlite based, pure Go, no
// CGO) self-registers under the "sqlite" URL scheme on import; migration
// files execute as-is, no batch splitting needed.
import (
	_ "github.com/golang-migrate/migrate/v4/database/sqlite"
)

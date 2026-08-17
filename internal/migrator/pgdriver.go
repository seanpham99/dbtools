package migrator

// golang-migrate's postgres driver self-registers under the "postgres"
// (and "postgresql") URL schemes on import; unlike MSSQL it needs no
// batch-splitting wrapper — migration files execute as-is.
import (
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
)

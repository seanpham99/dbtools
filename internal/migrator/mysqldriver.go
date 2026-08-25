package migrator

// golang-migrate's mysql driver self-registers under the "mysql" URL scheme
// on import (confirmed in its Open(): it does
// mysql.ParseDSN(strings.TrimPrefix(url, "mysql://")) — no scheme rewrite
// needed, unlike MSSQL's mssql:// -> sqlserver:// wrapper). No batch
// splitting or session reset needed: migration files execute as-is.
import (
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
)

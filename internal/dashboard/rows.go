package dashboard

import (
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/statusinfo"
)

type CollectFunc func(databaseURL, migrationsDir, targetName string) (*statusinfo.Status, error)

type Row struct {
	Target string
	Status *statusinfo.Status
	Err    error
}

func BuildRows(cfg *config.Config, collect CollectFunc) []Row {
	if collect == nil {
		collect = statusinfo.Collect
	}
	rows := make([]Row, 0, len(cfg.Targets))
	for _, name := range cfg.TargetNames() {
		url, err := cfg.ResolveURL(name)
		if err != nil {
			rows = append(rows, Row{Target: name, Err: err})
			continue
		}

		if _, err := engine.ForTarget(cfg.EngineName(name), url); err != nil {
			rows = append(rows, Row{Target: name, Err: err})
			continue
		}

		status, err := collect(url, cfg.MigrationsDir, name)
		if err != nil {
			rows = append(rows, Row{Target: name, Err: err})
			continue
		}

		rows = append(rows, Row{Target: name, Status: status})
	}
	return rows
}

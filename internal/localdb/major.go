package localdb

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/seanpham99/dbtools/internal/pgx"
)

// EffectiveMajor returns the Postgres major the local database runs at for
// the configured provider, or "" when the engine has no Postgres major. It
// reports an error when the configured pin cannot be served — the same
// answer dbtools start would give.
func EffectiveMajor(cfg *config.Config, engineName string) (string, error) {
	p, err := Select(cfg, engineName)
	if err != nil {
		return "", err
	}
	return p.Major(requestedMajor(cfg))
}

// requestedMajor returns the Postgres major the project pinned, or "" when
// it pinned none.
func requestedMajor(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(string(cfg.Local.PostgresMajor))
}

// validateMajor rejects a pin that is not a plain major version number.
//
// It deliberately runs where the local database is provisioned rather than
// in config.Load: Load runs on every command, and a malformed value there
// would also refuse `dbtools stop` and `dbtools logs`, which must keep
// working precisely when the configuration is wrong.
func validateMajor(major string) error {
	n, err := strconv.Atoi(major)
	if err != nil || n <= 0 {
		return fmt.Errorf("dbtools.toml: [local] postgres_major must be a PostgreSQL major version number like 17, got %q", major)
	}
	return nil
}

// imagePostgresMajor returns the Postgres major an image tag encodes, e.g.
// "17" for postgres:17-alpine. It returns "" when the tag names none.
func imagePostgresMajor(image string) string {
	repo, tag, ok := strings.Cut(image, ":")
	if !ok || repo != "postgres" {
		return ""
	}
	major, _, _ := strings.Cut(tag, "-")
	if _, err := strconv.Atoi(major); err != nil {
		return ""
	}
	return major
}

// warnMajorChange says so plainly when choosing this provider changes the
// local database's Postgres major.
//
// The change is derived, never remembered: the two providers' majors are
// known (docker's image tag, the pgx runtime's own report), and the other
// provider's local database for this project is visible on disk. The
// warning therefore fires exactly when the project actually has a local
// database under the other provider, and a project that has only ever used
// one provider stays quiet.
func warnMajorChange(p Provider, engineName, projectID, major string) {
	if engineName != "postgres" || major == "" {
		return
	}
	switch p.Name() {
	case ProviderPgX:
		// The volume outlives the container: `dbtools stop` deletes the
		// container and keeps the data. So the volume's image label is the
		// record a provider switch has to be judged against, and the
		// container is only a fallback for a volume that predates labels.
		image, ok := container.LocalVolumeImage(engineName, projectID)
		if !ok {
			image, ok = container.LocalImage(engineName, projectID)
			if !ok {
				return
			}
		}
		previous := imagePostgresMajor(image)
		if previous == "" || previous == major {
			return
		}
		logger.Warnf("the local database's PostgreSQL major changes with the provider: this project's docker local database uses %s, and the pgx provider serves PostgreSQL %s. A schema developed against one major is rendered differently by the other, so pin [local] postgres_major (the pgx provider can only serve PostgreSQL %s).", image, major, major)
	case ProviderDocker:
		if !pgx.ClusterExists(engineName, projectID) {
			return
		}
		previous, err := pgx.ServerMajor()
		if err != nil || previous == "" || previous == major {
			return
		}
		logger.Warnf("the local database's PostgreSQL major changes with the provider: this project's pgx cluster serves PostgreSQL %s, and the docker provider now runs PostgreSQL %s. A schema developed against one major is rendered differently by the other, so pin [local] postgres_major.", previous, major)
	}
}

// ObservedMajor returns the Postgres major the project's local database
// actually runs, and whether that is knowable from the provider's artifact.
//
// It deliberately reads what is on disk rather than the configuration: a
// pin that has not been applied yet is exactly the case a report must not
// present as fact. Nothing is started or connected to here.
func ObservedMajor(cfg *config.Config, engineName, projectID string) (string, bool) {
	p, err := Select(cfg, engineName)
	if err != nil {
		return "", false
	}
	switch p.Name() {
	case ProviderDocker:
		// The volume outlives the container and records the image that
		// initialised it, so it is the durable evidence.
		if image, ok := container.LocalVolumeImage(engineName, projectID); ok {
			if major := imagePostgresMajor(image); major != "" {
				return major, true
			}
			return "", false
		}
		// A volume created before dbtools recorded its image leaves the
		// container as the only evidence.
		if image, ok := container.LocalImage(engineName, projectID); ok {
			if major := imagePostgresMajor(image); major != "" {
				return major, true
			}
		}
		return "", false
	case ProviderPgX:
		// The pgx runtime serves one major per installation, so a cluster
		// it started carries that major. A cluster created by an older
		// runtime is not distinguishable without connecting to it.
		if !pgx.ClusterExists(engineName, projectID) {
			return "", false
		}
		major, err := pgx.ServerMajor()
		if err != nil || major == "" {
			return "", false
		}
		return major, true
	}
	return "", false
}

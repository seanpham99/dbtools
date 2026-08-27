package diff

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/scratchdb"
)

// Run provisions a scratch database (or uses againstURL if non-empty,
// skipping provisioning), replays every migration file into it via the
// real apply path, introspects both it and target, and returns the
// structural comparison. Never writes to target.
func Run(cfg *config.Config, targetName, againstURL string) (findings []Finding, notes []string, err error) {
	targetURL, err := cfg.ResolveURLOrFlag(targetName, "")
	if err != nil {
		return nil, nil, err
	}
	eng, err := engine.ForTarget(cfg.EngineName(targetName), targetURL)
	if err != nil {
		return nil, nil, err
	}

	// Open the target first, purely to read its server version: the scratch
	// database has to render catalog metadata the same way, or the
	// comparison reports version differences as schema drift. Parity is what
	// makes byte-exact comparison correct — see
	// docs/adr/003-v0.7-native-runner.md.
	targetDB, err := eng.Open(targetURL)
	if err != nil {
		return nil, nil, err
	}
	defer targetDB.Close()
	targetSeries := scratchdb.ServerSeries(targetDB, eng.Name())

	// A caller-supplied scratch database is not ours to pin, so verify it
	// instead. Refusing is the right outcome: a mismatched --against
	// produces findings that look real and are not, and a wrong answer from
	// a drift checker is worse than no answer.
	if againstURL != "" {
		if err := verifyAgainstParity(eng, againstURL, targetSeries); err != nil {
			return nil, nil, err
		}
	}
	if targetSeries == "" && container.PinsVersion(eng.Name()) {
		notes = append(notes, fmt.Sprintf(
			"could not determine the %s server version; the scratch database uses the default image, "+
				"so version-specific rendering differences may appear as findings", eng.Name()))
	}

	scratch, err := scratchdb.ProvisionSeries(eng, againstURL, targetSeries)
	if err != nil {
		return nil, nil, err
	}
	scratchURL := scratch.URL
	if cleanup := scratch.Cleanup; cleanup != nil {
		defer func() {
			cerr := cleanup()
			if cerr == nil {
				return
			}
			if err != nil {
				// The run already failed — fold the cleanup failure into
				// that error rather than the notes slice, which callers
				// don't render on a non-nil error (see cmd/diff.go).
				err = fmt.Errorf("%w (scratch database cleanup also failed: %v)", err, cerr)
				return
			}
			notes = append(notes, fmt.Sprintf("warning: scratch database cleanup failed: %v", cerr))
		}()
	}

	if _, err := apply.Run(cfg, "diff-scratch", scratchURL); err != nil {
		return nil, nil, fmt.Errorf("replaying migrations into scratch database: %w", err)
	}

	scratchEng, err := engine.ForURL(scratchURL)
	if err != nil {
		return nil, nil, err
	}
	scratchDB, err := scratchEng.Open(scratchURL)
	if err != nil {
		return nil, nil, err
	}
	defer scratchDB.Close()
	scratchSchema, _, err := scratchEng.Introspect(scratchDB, cfg.Generate.Exclude)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting scratch database: %w", err)
	}

	targetSchema, _, err := eng.Introspect(targetDB, cfg.Generate.Exclude)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting target database: %w", err)
	}

	compareFindings, compareNotes := Compare(scratchSchema, targetSchema)
	return compareFindings, append(notes, compareNotes...), nil
}

// verifyAgainstParity refuses a caller-supplied scratch database whose
// server version does not match the target's. targetSeries may be empty
// (version undetectable), in which case there is nothing to compare against
// and the caller has already been warned.
func verifyAgainstParity(eng engine.Engine, againstURL, targetSeries string) error {
	if targetSeries == "" {
		return nil
	}
	againstDB, err := eng.Open(againstURL)
	if err != nil {
		return fmt.Errorf("opening --against database: %w", err)
	}
	defer againstDB.Close()

	againstSeries := scratchdb.ServerSeries(againstDB, eng.Name())
	if againstSeries == "" {
		return fmt.Errorf(
			"could not determine the --against database's %s version, so it cannot be confirmed to match "+
				"the target (%s). Supply a scratch database on the same version, or omit --against and let "+
				"dbtools provision a matching one",
			eng.Name(), targetSeries)
	}
	if againstSeries != targetSeries {
		return newMismatchError(eng.Name(), againstSeries, targetSeries)
	}
	return nil
}

// newMismatchError explains a --against database whose version does not
// match the target's, and names the version the user needs. Separated out so
// the remediation can be tested without a live server on two versions.
func newMismatchError(engineName, againstSeries, targetSeries string) error {
	return fmt.Errorf(
		"--against database is %[1]s %[2]s but the target is %[1]s %[3]s. Comparing schemas across "+
			"versions reports rendering differences as drift, so the result would be wrong rather than "+
			"merely noisy. Supply a scratch database on %[1]s %[3]s, or omit --against and let dbtools "+
			"provision a matching one",
		engineName, againstSeries, targetSeries)
}

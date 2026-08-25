package cmd

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/clone"
	"github.com/seanpham99/dbtools/internal/config"
)

func TestCloneCmd_RequiresYesFlag(t *testing.T) {
	cloneYes = false
	t.Cleanup(func() { cloneYes = false })
	err := cloneCmd.RunE(cloneCmd, []string{"prod", "dev"})
	if err == nil {
		t.Fatal("expected error when --yes is not set, got nil")
	}
}

func TestCloneCmd_RejectsBothMaskFlags(t *testing.T) {
	cloneYes = true
	cloneMask = true
	cloneNoMask = true
	t.Cleanup(func() { cloneYes, cloneMask, cloneNoMask = false, false, false })
	err := cloneCmd.RunE(cloneCmd, []string{"prod", "dev"})
	if err == nil {
		t.Fatal("expected error when both --mask and --no-mask are set, got nil")
	}
}

func TestRunClone_RefusesProtectedDest(t *testing.T) {
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			Targets: map[string]config.Target{
				"prod": {URLEnv: "DBTOOLS_CLONE_PROD_URL"},
				"dev":  {URLEnv: "DBTOOLS_CLONE_DEV_URL", Protected: true},
			},
		}, nil
	}
	cloneYes = true
	t.Cleanup(func() { cloneYes = false })

	if err := runClone("prod", "dev"); err == nil {
		t.Fatal("runClone() into a protected dest should refuse")
	}
}

func TestRunClone_CallsCloneRunWithResolvedOptions(t *testing.T) {
	origLoadConfig := loadConfig
	origCloneRun := cloneRun
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		cloneRun = origCloneRun
	})
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			Targets: map[string]config.Target{
				"prod": {URLEnv: "DBTOOLS_CLONE_PROD_URL"},
				"dev":  {URLEnv: "DBTOOLS_CLONE_DEV_URL"},
			},
		}, nil
	}
	var gotSource, gotDest string
	var gotOpts clone.Options
	cloneRun = func(cfg *config.Config, source, dest string, opts clone.Options) (*clone.Result, error) {
		gotSource, gotDest, gotOpts = source, dest, opts
		return &clone.Result{Source: source, Dest: dest}, nil
	}

	cloneYes = true
	cloneNoMask = true
	cloneLimit = 50
	cloneWhere = "status = 'Shipped'"
	t.Cleanup(func() { cloneYes, cloneNoMask, cloneLimit, cloneWhere = false, false, 0, "" })

	if err := runClone("prod", "dev"); err != nil {
		t.Fatalf("runClone() returned error: %v", err)
	}
	if gotSource != "prod" || gotDest != "dev" {
		t.Errorf("cloneRun called with (%q, %q), want (prod, dev)", gotSource, gotDest)
	}
	if gotOpts.Mask {
		t.Errorf("Options.Mask = true, want false (--no-mask was set)")
	}
	if gotOpts.Limit != 50 || gotOpts.Where != "status = 'Shipped'" {
		t.Errorf("Options = %+v, want Limit=50 Where=\"status = 'Shipped'\"", gotOpts)
	}
}

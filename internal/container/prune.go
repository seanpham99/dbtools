package container

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Label keys stamped on every tool-owned container and data volume at
// creation. projectID is a one-way hash of the config path, so a
// dbtools-<engine>-<hash> name can never be resolved back to the worktree
// that owns it — the configpath label is what lets Prune prove the owning
// project directory is gone.
const (
	LabelManaged    = "dbtools.managed"
	LabelEngine     = "dbtools.engine"
	LabelConfigPath = "dbtools.configpath"
	// LabelImage records the image a data volume was initialised by. A
	// PostgreSQL data directory belongs to the major that wrote it, so the
	// label is what lets StartForWithTimeout refuse to open an existing
	// volume under a different image instead of letting the server fail
	// with a message that does not name the remedy.
	LabelImage = "dbtools.image"
)

// managedLabels returns the docker --label args identifying a tool-owned
// resource and the dbtools.toml that owns it. configPath may be "" (scratch
// containers, callers without a config); the label is still emitted so Prune
// treats an empty path as "not provably dead" and leaves the resource alone.
func managedLabels(engineName, configPath string) []string {
	return []string{
		"--label", LabelManaged + "=true",
		"--label", LabelEngine + "=" + engineName,
		"--label", LabelConfigPath + "=" + configPath,
	}
}

// insertLabels inserts docker run label args before the final image
// argument — every runArgs/scratchRunArgs template ends with s.image.
func insertLabels(args, labels []string) []string {
	return append(args[:len(args)-1:len(args)-1], append(labels, args[len(args)-1])...)
}

// localConfigPath resolves the config path convention loadConfig already
// follows: dbtools.toml in the current directory. Keeping the derivation
// here (rather than a parameter threaded through StartForWithTimeout)
// mirrors projectid.Resolve's own hardcoded "dbtools.toml" path.
func localConfigPath() string {
	abs, err := filepath.Abs("dbtools.toml")
	if err != nil {
		return ""
	}
	return abs
}

// ensureVolume creates the named data volume with the same labels as its
// container so Prune can judge it independently. image is recorded too, so
// a later start can prove which major the data directory belongs to.
//
// docker volume create is a no-op for an existing volume — labels are not
// retro-applied, which is what makes the image record a creation-time fact.
// The error is intentionally swallowed: a failed pre-create does not stop
// the container run — docker run itself auto-creates the volume
// (unlabeled) if needed.
func ensureVolume(name, engineName, configPath, image string) error {
	args := append([]string{"volume", "create"}, managedLabels(engineName, configPath)...)
	if image != "" {
		args = append(args, "--label", LabelImage+"="+image)
	}
	args = append(args, name)
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker volume create failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// PruneReport lists what Prune removed or would remove.
type PruneReport struct {
	Containers []string
	Volumes    []string
}

// labelsFor returns the label map of one docker object ("container" or
// "volume"). Containers keep labels under .Config.Labels, volumes under
// .Labels. A failed inspect reports an empty map, never an error — Prune
// treats missing labels as "not provably dead" and skips.
func labelsFor(kind, name string) map[string]string {
	format := "{{json .Labels}}"
	if kind == "container" {
		format = "{{json .Config.Labels}}"
	}
	out, err := exec.Command("docker", kind, "inspect", "-f", format, name).Output()
	if err != nil {
		return map[string]string{}
	}
	m := map[string]string{}
	if err := json.Unmarshal(out, &m); err != nil {
		return map[string]string{}
	}
	return m
}

// deadOwner reports whether the resource's configpath label points at a
// config file that no longer exists. Missing or unreadable labels and
// stat errors other than nonexistence all report false — the resource is
// never judged dead on ambiguous evidence.
func deadOwner(labels map[string]string) bool {
	if labels[LabelManaged] != "true" {
		return false
	}
	path := labels[LabelConfigPath]
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err != nil && os.IsNotExist(err)
}

// listManaged returns names of docker objects ("container" or "volume")
// carrying LabelManaged.
func listManaged(kind string) ([]string, error) {
	var out []byte
	var err error
	if kind == "volume" {
		out, err = exec.Command("docker", "volume", "ls", "--filter", "label="+LabelManaged+"=true", "--format", "{{.Name}}").Output()
	} else {
		out, err = exec.Command("docker", "ps", "-a", "--filter", "label="+LabelManaged+"=true", "--format", "{{.Names}}").Output()
	}
	if err != nil {
		return nil, fmt.Errorf("listing managed %ss: %w", kind, err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

// volumeMounts returns the names of docker volumes mounted by the container
// named name. Only volumes of type "volume" are returned; bind mounts are
// ignored. A failed inspect reports no volumes.
func volumeMounts(name string) []string {
	out, err := exec.Command("docker", "inspect", "-f", `{{range .Mounts}}{{if eq .Type "volume"}}{{.Name}}{{"\n"}}{{end}}{{end}}`, name).Output()
	if err != nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" && !seen[line] {
			seen[line] = true
			names = append(names, line)
		}
	}
	return names
}

// Prune removes tool-owned containers whose owning dbtools.toml no longer
// exists — the worktree-deleted leak, where nothing runs dbtools stop. With
// removeVolumes it also removes the data volumes those containers mounted
// (labeled or not), since a dead container's mounts are dead by the same
// evidence. dryRun reports without removing. Only labeled containers are
// considered; unlabeled legacy containers predate the labels and are left
// for manual cleanup because their owning path cannot be proven dead.
func Prune(removeVolumes, dryRun bool) (PruneReport, error) {
	var rep PruneReport
	if err := checkDocker(); err != nil {
		return rep, err
	}
	containers, err := listManaged("container")
	if err != nil {
		return rep, err
	}
	deadContainerVolumes := map[string]bool{}
	extraVolumes := []string{}
	for _, name := range containers {
		labels := labelsFor("container", name)
		if !deadOwner(labels) {
			continue
		}
		rep.Containers = append(rep.Containers, name)
		if removeVolumes {
			for _, v := range volumeMounts(name) {
				if !deadContainerVolumes[v] {
					deadContainerVolumes[v] = true
					extraVolumes = append(extraVolumes, v)
				}
			}
		}
		if !dryRun {
			if out, err := exec.Command("docker", "rm", "-f", name).CombinedOutput(); err != nil {
				return rep, fmt.Errorf("removing %s: %w: %s", name, err, strings.TrimSpace(string(out)))
			}
		}
	}
	if !removeVolumes {
		return rep, nil
	}
	// Sort extra volumes for deterministic output.
	sort.Strings(extraVolumes)

	volumes, err := listManaged("volume")
	if err != nil {
		return rep, err
	}
	seen := map[string]bool{}
	var candidates []string
	for _, v := range volumes {
		if !seen[v] {
			seen[v] = true
			candidates = append(candidates, v)
		}
	}
	for _, v := range extraVolumes {
		if !seen[v] {
			seen[v] = true
			candidates = append(candidates, v)
		}
	}
	for _, name := range candidates {
		if !deadContainerVolumes[name] {
			if !deadOwner(labelsFor("volume", name)) {
				continue
			}
		}
		rep.Volumes = append(rep.Volumes, name)
		if !dryRun {
			if out, err := exec.Command("docker", "volume", "rm", name).CombinedOutput(); err != nil {
				return rep, fmt.Errorf("removing volume %s: %w: %s", name, err, strings.TrimSpace(string(out)))
			}
		}
	}
	return rep, nil
}

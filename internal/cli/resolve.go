package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/zeroflow/esphome-sil/internal/apperr"
	"github.com/zeroflow/esphome-sil/internal/instance"
)

// envInstance names the default instance for the instance commands.
const envInstance = "SIL_INSTANCE"

// resolveInstance names the instance an instance command works on: the
// positional name, else $SIL_INSTANCE, else the single live instance under the
// runtime base. With none or several live instances it answers Failed
// instance_ambiguous whose detail.candidates lists the live ones sorted
// lexicographically (an empty list when there are none).
func resolveInstance(positional string) (string, error) {
	if positional != "" {
		return positional, nil
	}
	if v := os.Getenv(envInstance); v != "" {
		return v, nil
	}
	live, err := liveInstances()
	if err != nil {
		return "", err
	}
	if len(live) == 1 {
		return live[0], nil
	}
	msg := "no running instance found; pass <name> or set " + envInstance
	if len(live) > 1 {
		msg = fmt.Sprintf("several instances are running (%s); pass <name> or set %s", strings.Join(live, ", "), envInstance)
	}
	return "", apperr.Failed("instance_ambiguous", msg, map[string]any{"candidates": live})
}

// resolvePaths is resolveInstance followed by the name validation and the
// directory layout of PathsFor.
func resolvePaths(positional string) (instance.Paths, error) {
	name, err := resolveInstance(positional)
	if err != nil {
		return instance.Paths{}, err
	}
	return instance.PathsFor(name)
}

// liveInstances lists, sorted, the instances under the runtime base
// (<runtime>/sil/<name>) whose run.json names a live `sil serve <name>`
// process. It never returns nil, so an empty list encodes as [].
func liveInstances() ([]string, error) {
	base, err := runtimeBase()
	if err != nil {
		return nil, err
	}
	if err := checkPrivateDir(base); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(base)
	if err != nil && !os.IsNotExist(err) {
		return nil, apperr.Infra("runtime_dir_unreadable", "cannot list the instance runtime directory",
			map[string]any{"path": base, "error": err.Error()})
	}
	live := []string{}
	for _, e := range entries {
		if !e.IsDir() || instance.ValidateName(e.Name()) != nil {
			continue
		}
		p, err := instance.PathsFor(e.Name())
		if err != nil {
			continue
		}
		rf, err := instance.LoadRunFile(p)
		if err != nil || !instance.ProcessAlive(rf, p.Name) {
			continue
		}
		live = append(live, p.Name)
	}
	sort.Strings(live)
	return live, nil
}

// runtimeBase is the directory that holds one run dir per instance, derived
// from the layout PathsFor owns (so XDG_RUNTIME_DIR and its /tmp fallback stay
// defined in one place).
func runtimeBase() (string, error) {
	p, err := instance.PathsFor("x")
	if err != nil {
		return "", err
	}
	return filepath.Dir(p.RunDir), nil
}

// checkPrivateDir is the read-only counterpart of the check `sil up` makes when
// it creates the runtime base: a directory that exists must be a real
// directory (not a symlink) owned by this user with mode 0700. The /tmp/sil-$UID
// fallback is world-reachable, and a run.json planted there by another user
// must not steer the client. A missing directory is fine (nothing is running).
func checkPrivateDir(dir string) error {
	fi, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return apperr.Infra("runtime_dir_unreadable", "cannot inspect the instance runtime directory",
			map[string]any{"path": dir, "error": err.Error()})
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() || fi.Mode().Perm()&0o077 != 0 {
		return apperr.Infra("unsafe_runtime_dir",
			"the instance runtime directory must be a real directory owned by you with mode 0700; remove or fix it",
			map[string]any{"path": dir})
	}
	return nil
}

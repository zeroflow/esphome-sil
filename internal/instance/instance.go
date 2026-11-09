// Package instance owns the identity and on-disk layout of a sil instance:
// the validated name, the XDG state/run/cache directories, the persistent
// identity (API key, API port), and the run.json / spec.json files.
//
// Plan 01-09 hardens port allocation, validation and locking behind this
// same API.
package instance

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

// DefaultAPIPort is the first-time API port (6053 is the HIL supervisor port
// and stays free).
const DefaultAPIPort = 6054

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,25}[a-z0-9])?$`)

// ValidateName checks an instance name before it is used in any path, argv
// or substitution.
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return apperr.Failed("invalid_name",
			"instance names are 1-27 characters of a-z, 0-9 and '-', starting and ending with a letter or digit",
			map[string]any{"name": name})
	}
	return nil
}

// Paths are the directories of one instance.
type Paths struct {
	Name     string
	StateDir string // $XDG_STATE_HOME/sil/<name>
	RunDir   string // $XDG_RUNTIME_DIR/sil/<name>, fallback /tmp/sil-$UID/<name>
	CacheDir string // $XDG_CACHE_HOME/sil/build/<name>
}

// PathsFor resolves the directories of name. The name is validated first.
func PathsFor(name string) (Paths, error) {
	if err := ValidateName(name); err != nil {
		return Paths{}, err
	}
	home, _ := os.UserHomeDir()
	state := xdgDir("XDG_STATE_HOME", home, ".local/state")
	cache := xdgDir("XDG_CACHE_HOME", home, ".cache")
	if state == "" || cache == "" {
		return Paths{}, apperr.Infra("no_home", "cannot determine the home directory; set XDG_STATE_HOME and XDG_CACHE_HOME", nil)
	}
	var run string
	if rd := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(rd) {
		run = filepath.Join(rd, "sil", name)
	} else {
		run = filepath.Join("/tmp", "sil-"+strconv.Itoa(os.Getuid()), name)
	}
	return Paths{
		Name:     name,
		StateDir: filepath.Join(state, "sil", name),
		RunDir:   run,
		CacheDir: filepath.Join(cache, "sil", "build", name),
	}, nil
}

// xdgDir is $env when it is an absolute path (the XDG rule), else
// $HOME/fallback, else "".
func xdgDir(env, home, fallback string) string {
	if v := os.Getenv(env); filepath.IsAbs(v) {
		return v
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, fallback)
}

// File locations inside the run and state dirs.
func (p Paths) BusSock() string      { return filepath.Join(p.RunDir, "bus.sock") }
func (p Paths) APISock() string      { return filepath.Join(p.RunDir, "api.sock") }
func (p Paths) PrefsDir() string     { return filepath.Join(p.RunDir, "prefs") }
func (p Paths) RunJSON() string      { return filepath.Join(p.RunDir, "run.json") }
func (p Paths) SpecJSON() string     { return filepath.Join(p.RunDir, "spec.json") }
func (p Paths) BuildLog() string     { return filepath.Join(p.RunDir, "build.log") }
func (p Paths) FirmwareLog() string  { return filepath.Join(p.RunDir, "firmware.log") }
func (p Paths) SilLog() string       { return filepath.Join(p.RunDir, "sil.log") }
func (p Paths) IdentityJSON() string { return filepath.Join(p.StateDir, "identity.json") }

// Identity is the persistent part of an instance.
type Identity struct {
	Schema  int    `json:"schema"`
	Name    string `json:"name"`
	APIPort int    `json:"api_port"`
	Key     string `json:"key"`
}

// DeviceName is the ESPHome name of the firmware.
func (id Identity) DeviceName() string { return "sil-" + id.Name }

// FriendlyName is the ESPHome friendly name.
func (id Identity) FriendlyName() string { return "SiL " + id.Name }

// MAC derives the locally administered MAC of an instance: "02:" plus the
// first 5 bytes of sha256("sil-mac:"+name), lowercase, colon separated.
func MAC(name string) string {
	sum := sha256.Sum256([]byte("sil-mac:" + name))
	parts := []string{"02"}
	for _, b := range sum[:5] {
		parts = append(parts, fmt.Sprintf("%02x", b))
	}
	return strings.Join(parts, ":")
}

// LoadOrCreate reads identity.json or creates it on first use (32 random
// bytes as base64, mode 0600 in a 0700 dir). apiPort > 0 overrides and
// stores the port; 0 keeps the stored port, else DefaultAPIPort.
func LoadOrCreate(p Paths, apiPort int) (Identity, error) {
	if apiPort < 0 || apiPort > 65535 {
		return Identity{}, apperr.Failed("invalid_port", "the API port must be between 1 and 65535", map[string]any{"port": apiPort})
	}
	var id Identity
	raw, err := os.ReadFile(p.IdentityJSON())
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &id); err != nil || id.Key == "" || id.APIPort == 0 {
			return Identity{}, apperr.Infra("identity_invalid", "identity.json is unreadable; fix or delete it",
				map[string]any{"path": p.IdentityJSON()})
		}
		if apiPort == 0 || apiPort == id.APIPort {
			return id, nil
		}
		id.APIPort = apiPort
	case errors.Is(err, os.ErrNotExist):
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return Identity{}, apperr.Infra("identity_failed", "cannot generate the API key", map[string]any{"error": err.Error()})
		}
		id = Identity{Schema: 1, Name: p.Name, APIPort: DefaultAPIPort, Key: base64.StdEncoding.EncodeToString(key)}
		if apiPort > 0 {
			id.APIPort = apiPort
		}
	default:
		return Identity{}, apperr.Infra("identity_failed", "cannot read identity.json", map[string]any{"path": p.IdentityJSON(), "error": err.Error()})
	}
	if err := writeJSON(p.IdentityJSON(), id, 0o700); err != nil {
		return Identity{}, apperr.Infra("identity_failed", "cannot write identity.json", map[string]any{"path": p.IdentityJSON(), "error": err.Error()})
	}
	return id, nil
}

// writeJSON writes v to path through a temp file and rename, mode 0600, and
// makes sure the parent directory exists with dirMode.
func writeJSON(path string, v any, dirMode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(append(raw, '\n'))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(name)
		return errors.Join(werr, cerr)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// RunFile is run.json: how later commands find a running instance.
type RunFile struct {
	Schema   int    `json:"schema"`
	Instance string `json:"instance"`
	PID      int    `json:"pid"`
	RunDir   string `json:"run_dir"`
	APISock  string `json:"api_sock"`
	BusSock  string `json:"bus_sock"`
}

// WriteRunFile writes run.json atomically (tmp + rename, 0600).
func WriteRunFile(p Paths, rf RunFile) error {
	return writeJSON(p.RunJSON(), rf, 0o700)
}

// ReadRunFile reads run.json; a missing file means the instance is not
// running (Failed "not_running").
func ReadRunFile(p Paths) (RunFile, error) {
	raw, err := os.ReadFile(p.RunJSON())
	if errors.Is(err, os.ErrNotExist) {
		return RunFile{}, notRunning(p.Name)
	}
	if err != nil {
		return RunFile{}, apperr.Infra("run_file_unreadable", "cannot read run.json", map[string]any{"path": p.RunJSON(), "error": err.Error()})
	}
	var rf RunFile
	if err := json.Unmarshal(raw, &rf); err != nil || rf.APISock == "" {
		return RunFile{}, apperr.Infra("run_file_invalid", "run.json is unreadable", map[string]any{"path": p.RunJSON()})
	}
	return rf, nil
}

func notRunning(name string) error {
	return apperr.Failed("not_running", "instance "+name+" is not running", map[string]any{"instance": name})
}

// Alive reports whether a process with this PID exists.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Spec is spec.json: what the supervisor runs.
type Spec struct {
	Schema   int      `json:"schema"`
	Instance string   `json:"instance"`
	Program  string   `json:"program"`
	Fans     []string `json:"fans"`
}

// WriteSpec writes spec.json into the run dir.
func WriteSpec(p Paths, s Spec) error {
	return writeJSON(p.SpecJSON(), s, 0o700)
}

// ReadSpec reads spec.json.
func ReadSpec(p Paths) (Spec, error) {
	raw, err := os.ReadFile(p.SpecJSON())
	if err != nil {
		return Spec{}, apperr.Infra("spec_unreadable", "cannot read spec.json", map[string]any{"path": p.SpecJSON(), "error": err.Error()})
	}
	var s Spec
	if err := json.Unmarshal(raw, &s); err != nil || s.Program == "" || len(s.Fans) == 0 {
		return Spec{}, apperr.Infra("spec_invalid", "spec.json is unreadable", map[string]any{"path": p.SpecJSON()})
	}
	return s, nil
}

// ResetRunDir recreates the run dir empty (0700) with prefs/ (0700), so
// firmware preferences start fresh on every `sil up`.
func ResetRunDir(p Paths) error {
	if err := os.RemoveAll(p.RunDir); err != nil {
		return apperr.Infra("run_dir_failed", "cannot clear the run directory", map[string]any{"path": p.RunDir, "error": err.Error()})
	}
	for _, d := range []string{p.RunDir, p.PrefsDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return apperr.Infra("run_dir_failed", "cannot create the run directory", map[string]any{"path": d, "error": err.Error()})
		}
		if err := os.Chmod(d, 0o700); err != nil {
			return apperr.Infra("run_dir_failed", "cannot restrict the run directory", map[string]any{"path": d, "error": err.Error()})
		}
	}
	return nil
}

// Package instance owns the identity and on-disk layout of a sil instance:
// the validated name, the XDG state/run/cache directories, the persistent
// identity (API key, API port), and the run.json / spec.json files.
//
// Names are strict (never normalised), directories are disjoint per name and
// derived from absolute XDG variables only, the identity is created once and
// never regenerated silently, and the API port is sticky and collision-free
// across instances (port.go) under flock-based locks (lock.go).
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

// API port range. DefaultAPIPort is the first-time port (6053 is the HIL
// supervisor port and stays free); automatic allocation scans
// DefaultAPIPort..DefaultAPIPort+PortScanSpan-1.
const (
	DefaultAPIPort = 6054
	// DefaultWebPort is the first-time port of the web listener (D-09); its automatic scan covers
	// DefaultWebPort..DefaultWebPort+PortScanSpan-1, disjoint from the API port range.
	DefaultWebPort = 8054
	PortScanSpan   = 1000
	MinAPIPort     = 1024
	MaxAPIPort     = 65535
)

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,25}[a-z0-9])?$`)

// MaxNameLen is the longest instance name: "sil-" plus the name must stay
// within ESPHome's 31 characters.
const MaxNameLen = 27

// ValidateName checks an instance name before it is used in any path, argv
// or substitution. The name is never lower-cased or trimmed on the user's
// behalf; the message names the rule that was broken.
func ValidateName(name string) error {
	if nameRE.MatchString(name) {
		return nil
	}
	var why string
	switch {
	case name == "":
		why = "the name is empty"
	case len(name) > MaxNameLen:
		why = fmt.Sprintf("the name has %d characters, the maximum is %d", len(name), MaxNameLen)
	default:
		why = ""
		for _, r := range name {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				why = fmt.Sprintf("the name contains %q; only a-z, 0-9 and '-' are allowed (no upper case, no underscore)", r)
				break
			}
		}
		if why == "" {
			why = "the name must start and end with a letter or digit"
		}
	}
	return apperr.Failed("invalid_name",
		"invalid instance name: "+why+" (rule: 1-27 characters of a-z, 0-9 and '-', starting and ending with a letter or digit)",
		map[string]any{"name": name})
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

// StateRoot is the XDG state base that holds every instance's identity dir
// (<root>/sil/<name>); the port allocator scans it.
func (p Paths) StateRoot() string { return filepath.Dir(filepath.Dir(p.StateDir)) }

// maxSocketPath is the longest AF_UNIX path (sun_path is 108 bytes with the
// terminating NUL).
const maxSocketPath = 107

// CheckSocketPaths rejects a bus or control socket path longer than
// sun_path allows, counted in bytes, before anything is built.
func (p Paths) CheckSocketPaths() error {
	for _, sock := range []string{p.BusSock(), p.APISock()} {
		if n := len([]byte(sock)); n > maxSocketPath {
			return apperr.Infra("socket_path_too_long",
				fmt.Sprintf("socket path %s is %d bytes, the limit is %d; use a shorter XDG_RUNTIME_DIR or instance name", sock, n, maxSocketPath),
				map[string]any{"path": sock, "bytes": n, "max": maxSocketPath})
		}
	}
	return nil
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
	// WebPort is the sticky port of the web listener. Identities written before the web UI have none;
	// the next LoadOrCreate allocates one and leaves key and api_port unchanged.
	WebPort int    `json:"web_port,omitempty"`
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

// EqualMAC compares two MAC addresses ignoring case and colons, so ESPHome's
// upper-case "02:AA:..." output and the Noise hello's "02aa..." match the
// derived lower-case form.
func EqualMAC(a, b string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, ":", ""), "-", ""))
	}
	return norm(a) != "" && norm(a) == norm(b)
}

// LoadOrCreate returns the identity of p, creating it on first use (32
// random bytes as base64, mode 0600 in a 0700 dir). It runs under the global
// allocation lock so two instances created at the same time never receive the
// same port. apiPort > 0 overrides and stores the port (invalid_argument
// outside 1024-65535, port_claimed when another instance's identity holds
// it, port_in_use when it cannot be bound, in which case nothing is
// stored); 0 keeps the stored port, else the lowest free port from 6054. A
// stored identity that is malformed is identity_corrupt and is never
// regenerated silently.
func LoadOrCreate(p Paths, apiPort int) (Identity, error) {
	if apiPort != 0 && (apiPort < MinAPIPort || apiPort > MaxAPIPort) {
		return Identity{}, invalidPort(apiPort)
	}
	root := p.StateRoot()
	release, err := LockAlloc(root)
	if err != nil {
		return Identity{}, err
	}
	defer release()

	id, exists, err := readIdentity(p)
	if err != nil {
		return Identity{}, err
	}
	port, err := AllocatePort(root, p.Name, apiPort, func(port int) bool { return TestBind(port) == nil })
	if err != nil {
		return Identity{}, err
	}
	// The web port is sticky like the API port. An identity that has none (written before the web UI)
	// gets one here; the key and the API port stay as they are.
	webPort := id.WebPort
	if !exists || webPort == 0 {
		// The API port just decided is not stored yet on a first use; it must not become the web port.
		webPort, err = AllocateWebPort(root, p.Name, 0, func(wp int) bool { return wp != port && TestBindWeb("0.0.0.0", wp) == nil })
		if err != nil {
			return Identity{}, err
		}
	}
	if exists && port == id.APIPort && webPort == id.WebPort {
		return id, nil
	}
	// An explicit --api-port is the user's choice, but it becomes sticky once stored: a busy port
	// must fail here (port_in_use) without being persisted, or every later plain `sil up` would
	// re-use it and fail the same way. A port that is already the stored one changes nothing.
	if apiPort != 0 && !(exists && port == id.APIPort) {
		if err := TestBind(port); err != nil {
			return Identity{}, err
		}
	}
	if !exists {
		key := make([]byte, keyBytes)
		if _, err := rand.Read(key); err != nil {
			return Identity{}, apperr.Infra("identity_failed", "cannot generate the API key", map[string]any{"error": err.Error()})
		}
		id = Identity{Schema: 1, Name: p.Name, Key: base64.StdEncoding.EncodeToString(key)}
	}
	id.APIPort = port
	id.WebPort = webPort
	if err := writeJSON(p.IdentityJSON(), id, 0o700); err != nil {
		return Identity{}, apperr.Infra("identity_failed", "cannot write identity.json", map[string]any{"path": p.IdentityJSON(), "error": err.Error()})
	}
	return id, nil
}

// keyBytes is the length of the Noise pre-shared key.
const keyBytes = 32

// readIdentity loads and validates identity.json. exists is false when the
// file is absent (first use).
func readIdentity(p Paths) (id Identity, exists bool, err error) {
	raw, rerr := os.ReadFile(p.IdentityJSON())
	if errors.Is(rerr, os.ErrNotExist) {
		return Identity{}, false, nil
	}
	if rerr != nil {
		return Identity{}, false, apperr.Infra("identity_failed", "cannot read identity.json", map[string]any{"path": p.IdentityJSON(), "error": rerr.Error()})
	}
	corrupt := func(why string) error {
		return apperr.Infra("identity_corrupt",
			fmt.Sprintf("identity.json is unusable (%s); delete %s to reset (Home Assistant must then re-add the device)", why, p.StateDir),
			map[string]any{"path": p.IdentityJSON(), "hint": "delete " + p.StateDir + " to reset"})
	}
	if err := json.Unmarshal(raw, &id); err != nil {
		return Identity{}, true, corrupt("not valid JSON")
	}
	if id.Schema != 1 {
		return Identity{}, true, corrupt("unknown schema")
	}
	if id.Name != p.Name {
		return Identity{}, true, corrupt("name does not match the directory")
	}
	if key, kerr := base64.StdEncoding.DecodeString(id.Key); kerr != nil || len(key) != keyBytes {
		return Identity{}, true, corrupt("key is missing or not 32 base64-encoded bytes")
	}
	if id.APIPort < MinAPIPort || id.APIPort > MaxAPIPort {
		return Identity{}, true, corrupt("api_port is missing or out of range")
	}
	if id.WebPort != 0 && (id.WebPort < MinAPIPort || id.WebPort > MaxAPIPort) {
		return Identity{}, true, corrupt("web_port is out of range")
	}
	return id, true, nil
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
	// WebPort and WebListen are where the web listener serves (absent in run.json files of older
	// versions; readers decode without DisallowUnknownFields).
	WebPort   int    `json:"web_port,omitempty"`
	WebListen string `json:"web_listen,omitempty"`
}

// WriteRunFile writes run.json atomically (tmp + rename, 0600).
func WriteRunFile(p Paths, rf RunFile) error {
	return writeJSON(p.RunJSON(), rf, 0o700)
}

// ReadRunFile reads run.json of a running instance: a missing file, or one
// naming a dead or foreign PID (a supervisor that was SIGKILLed leaves its
// run.json behind), means the instance is not running (Failed "not_running").
func ReadRunFile(p Paths) (RunFile, error) {
	rf, err := LoadRunFile(p)
	if err != nil {
		return RunFile{}, err
	}
	if !ProcessAlive(rf, p.Name) {
		return RunFile{}, notRunning(p.Name)
	}
	return rf, nil
}

// LoadRunFile reads run.json without checking that its PID is alive; `sil
// down` uses it to find and clean up what a dead supervisor left behind.
func LoadRunFile(p Paths) (RunFile, error) {
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

// ProcessAlive reports whether rf names a live `sil serve <name>` process:
// the PID exists, /proc/<pid>/cmdline has the arguments "serve" and name, and
// the process is sil itself: argv[0] has the base name "sil" or
// /proc/<pid>/exe is the running sil executable (a renamed binary that
// re-executes itself as the supervisor). Any other process whose arguments
// merely contain `serve <name>` (a shell, `npx serve dev`) does not count
// (T-01-24). A run.json that names a dead or foreign (reused) PID is treated as
// not running.
func ProcessAlive(rf RunFile, name string) bool {
	if !Alive(rf.PID) {
		return false
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(rf.PID) + "/cmdline")
	if err != nil {
		return false
	}
	args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	serving := false
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "serve" && args[i+1] == name {
			serving = true
			break
		}
	}
	if !serving {
		return false
	}
	return filepath.Base(args[0]) == "sil" || sameExecutable(rf.PID)
}

// sameExecutable reports whether /proc/<pid>/exe is the executable of this
// process, which is how a `sil serve` supervisor started by startServe
// (os.Executable) is recognised even if the binary is not called sil.
func sameExecutable(pid int) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	other, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return false
	}
	return self == other
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
// firmware preferences start fresh on every `sil up`. The directory above it
// (the /tmp/sil-$UID fallback is world-reachable) must be a real directory
// owned by this user with no group or other permissions, else it is refused.
func ResetRunDir(p Paths) error {
	if err := EnsureRunParent(p); err != nil {
		return err
	}
	if err := os.RemoveAll(p.RunDir); err != nil {
		return apperr.Infra("run_dir_failed", "cannot clear the run directory", map[string]any{"path": p.RunDir, "error": err.Error()})
	}
	for _, d := range []string{p.RunDir, p.PrefsDir()} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return apperr.Infra("run_dir_failed", "cannot create the run directory", map[string]any{"path": d, "error": err.Error()})
		}
		if err := os.Chmod(d, 0o700); err != nil {
			return apperr.Infra("run_dir_failed", "cannot restrict the run directory", map[string]any{"path": d, "error": err.Error()})
		}
	}
	return nil
}

// EnsureRunParent creates the directory above the run dir (the
// /tmp/sil-$UID fallback or $XDG_RUNTIME_DIR/sil) if needed and insists that
// it is safe; `sil up` calls it before anything is built.
func EnsureRunParent(p Paths) error {
	return ensurePrivateDir(filepath.Dir(p.RunDir))
}

// ensurePrivateDir creates dir (0700) if missing and then insists that it is
// a directory (not a symlink) owned by the current user with mode 0700, so
// another local user cannot pre-create or redirect it (symlink/ownership race
// in /tmp).
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return apperr.Infra("run_dir_failed", "cannot create the run directory parent", map[string]any{"path": dir, "error": err.Error()})
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return apperr.Infra("run_dir_failed", "cannot create the run directory parent", map[string]any{"path": dir, "error": err.Error()})
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return apperr.Infra("run_dir_failed", "cannot inspect the run directory parent", map[string]any{"path": dir, "error": err.Error()})
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() || fi.Mode().Perm()&0o077 != 0 {
		return apperr.Infra("unsafe_runtime_dir",
			"the run directory parent must be a real directory owned by you with mode 0700; remove or fix it",
			map[string]any{"path": dir})
	}
	return nil
}

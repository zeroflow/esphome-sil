package instance

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/zeroflow/esphome-sil/internal/apperr"
)

func invalidPort(port int) error {
	return apperr.Failed("invalid_argument",
		fmt.Sprintf("--api-port %d is outside %d-%d", port, MinAPIPort, MaxAPIPort),
		map[string]any{"port": port, "min": MinAPIPort, "max": MaxAPIPort})
}

// claimedPorts maps every API port and every web port stored in an identity under
// <stateRoot>/sil/*/identity.json, except self's, to the owning instance.
// Unreadable or malformed files are skipped (they claim nothing).
func claimedPorts(stateRoot, self string) map[int]string {
	paths, _ := filepath.Glob(filepath.Join(stateRoot, "sil", "*", "identity.json"))
	sort.Strings(paths) // deterministic owner when two files claim the same port
	claimed := map[int]string{}
	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		if name == self {
			continue
		}
		api, web := storedPorts(path)
		for _, port := range []int{api, web} {
			if port == 0 {
				continue
			}
			if _, dup := claimed[port]; !dup {
				claimed[port] = name
			}
		}
	}
	return claimed
}

// storedPorts reads api_port and web_port from one identity.json; a missing, malformed or
// out-of-range value is 0.
func storedPorts(path string) (api, web int) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	var id struct {
		APIPort int `json:"api_port"`
		WebPort int `json:"web_port"`
	}
	if json.Unmarshal(raw, &id) != nil {
		return 0, 0
	}
	if id.APIPort >= MinAPIPort && id.APIPort <= MaxAPIPort {
		api = id.APIPort
	}
	if id.WebPort >= MinAPIPort && id.WebPort <= MaxAPIPort {
		web = id.WebPort
	}
	return api, web
}

// storedPort reads api_port from one identity.json.
func storedPort(path string) (int, bool) {
	api, _ := storedPorts(path)
	return api, api != 0
}

// storedWebPort reads web_port from one identity.json.
func storedWebPort(path string) (int, bool) {
	_, web := storedPorts(path)
	return web, web != 0
}

// AllocatePort decides the API port of instance self (D-07). The caller holds
// LockAlloc(stateRoot).
//
//   - requested > 0 (--api-port): must be within 1024-65535 (invalid_argument)
//     and not stored in another instance's identity (port_claimed, naming
//     that instance); it is returned as is, whether or not it is bindable.
//   - requested == 0 and self already has a stored port: that port, as is
//     (sticky; a taken sticky port is reported by TestBind, never moved).
//   - otherwise the lowest port from 6054 that no other identity claims and
//     for which bindable reports true; none in the range is no_free_port.
func AllocatePort(stateRoot, self string, requested int, bindable func(int) bool) (int, error) {
	claimed := claimedPorts(stateRoot, self)
	if requested != 0 {
		if requested < MinAPIPort || requested > MaxAPIPort {
			return 0, invalidPort(requested)
		}
		if owner, ok := claimed[requested]; ok {
			return 0, apperr.Failed("port_claimed",
				fmt.Sprintf("API port %d is already claimed by instance %s; stop using it there or choose another port", requested, owner),
				map[string]any{"port": requested, "instance": owner})
		}
		return requested, nil
	}
	if port, ok := storedPort(filepath.Join(stateRoot, "sil", self, "identity.json")); ok {
		return port, nil
	}
	for port := DefaultAPIPort; port < DefaultAPIPort+PortScanSpan; port++ {
		if _, taken := claimed[port]; taken {
			continue
		}
		if bindable != nil && !bindable(port) {
			continue
		}
		return port, nil
	}
	return 0, apperr.Infra("no_free_port",
		fmt.Sprintf("no free API port in %d-%d; stop an instance or pass --api-port", DefaultAPIPort, DefaultAPIPort+PortScanSpan-1),
		map[string]any{"from": DefaultAPIPort, "to": DefaultAPIPort + PortScanSpan - 1})
}

func invalidWebPort(port int) error {
	return apperr.Failed("invalid_argument",
		fmt.Sprintf("--web-port %d is outside %d-%d", port, MinAPIPort, MaxAPIPort),
		map[string]any{"port": port, "min": MinAPIPort, "max": MaxAPIPort})
}

// AllocateWebPort decides the web port of instance self (D-09). The caller holds
// LockAlloc(stateRoot). It mirrors AllocatePort:
//
//   - requested > 0 (--web-port): must be within 1024-65535 (invalid_argument)
//     and not stored (as api_port or web_port) in another instance's identity
//     (port_claimed, naming that instance); it is returned as is.
//   - requested == 0 and self already has a stored web port: that port, as is
//     (sticky; a taken sticky port is reported when the listener binds, never moved).
//   - otherwise the lowest port from 8054 that no other identity claims, that
//     is not self's own API port and for which bindable reports true; none in
//     the range is no_free_port.
func AllocateWebPort(stateRoot, self string, requested int, bindable func(int) bool) (int, error) {
	claimed := claimedPorts(stateRoot, self)
	if requested != 0 {
		if requested < MinAPIPort || requested > MaxAPIPort {
			return 0, invalidWebPort(requested)
		}
		if owner, ok := claimed[requested]; ok {
			return 0, apperr.Failed("port_claimed",
				fmt.Sprintf("web port %d is already claimed by instance %s; stop using it there or choose another port", requested, owner),
				map[string]any{"port": requested, "instance": owner})
		}
		return requested, nil
	}
	own := filepath.Join(stateRoot, "sil", self, "identity.json")
	if port, ok := storedWebPort(own); ok {
		return port, nil
	}
	ownAPI, _ := storedPort(own)
	for port := DefaultWebPort; port < DefaultWebPort+PortScanSpan; port++ {
		if _, taken := claimed[port]; taken || port == ownAPI {
			continue
		}
		if bindable != nil && !bindable(port) {
			continue
		}
		return port, nil
	}
	return 0, apperr.Infra("no_free_port",
		fmt.Sprintf("no free web port in %d-%d; stop an instance or pass --web-port", DefaultWebPort, DefaultWebPort+PortScanSpan-1),
		map[string]any{"from": DefaultWebPort, "to": DefaultWebPort + PortScanSpan - 1})
}

// ListenWeb binds the web listener on listen:port. A taken port is port_in_use (exit 2, naming
// the holder when visible), any other failure port_bind_failed; it never picks another port.
func ListenWeb(listen string, port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(listen, strconv.Itoa(port)))
	if err == nil {
		return ln, nil
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, apperr.Infra("port_bind_failed", fmt.Sprintf("cannot bind web port %d on %s: %v", port, listen, err),
			map[string]any{"port": port, "listen": listen, "error": err.Error()})
	}
	detail := map[string]any{"port": port, "listen": listen}
	msg := fmt.Sprintf("web port %d on %s is already in use", port, listen)
	if pid, cmd, ok := PortHolder(port); ok {
		detail["holder"] = map[string]any{"pid": pid, "cmd": cmd}
		msg += fmt.Sprintf(" by pid %d (%s)", pid, cmd)
	}
	return nil, apperr.Infra("port_in_use", msg, detail)
}

// TestBindWeb test-binds listen:port and closes it again.
func TestBindWeb(listen string, port int) error {
	ln, err := ListenWeb(listen, port)
	if err != nil {
		return err
	}
	return ln.Close()
}

// WebURL is the page URL printed for a web listener: the listen address, or for a wildcard
// listen ("", 0.0.0.0, ::) the LAN host, or 127.0.0.1 when that is unknown.
func WebURL(listen, lanHost string, port int) string {
	host := listen
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = lanHost
	}
	if host == "" {
		host = "127.0.0.1"
	}
	scheme := "http" + ":" + "//"
	return scheme + net.JoinHostPort(host, strconv.Itoa(port)) + "/"
}

// TestBind test-binds 0.0.0.0:port (where the firmware's API listens) and
// closes it again. A taken port is port_in_use (exit 2) naming the port and,
// when visible, the holder's pid and command; it never picks another port.
func TestBind(port int) error {
	ln, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
	if err == nil {
		return ln.Close()
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return apperr.Infra("port_bind_failed", fmt.Sprintf("cannot test-bind API port %d: %v", port, err),
			map[string]any{"port": port, "error": err.Error()})
	}
	detail := map[string]any{"port": port}
	msg := fmt.Sprintf("API port %d is already in use", port)
	if pid, cmd, ok := PortHolder(port); ok {
		detail["holder"] = map[string]any{"pid": pid, "cmd": cmd}
		msg += fmt.Sprintf(" by pid %d (%s)", pid, cmd)
	}
	return apperr.Infra("port_in_use", msg, detail)
}

// PortHolder finds the process that listens on TCP port: the LISTEN socket's
// inode in /proc/net/tcp{,6} is matched against /proc/*/fd links. Best
// effort (other users' processes are not visible); it never fails the command.
func PortHolder(port int) (pid int, cmd string, ok bool) {
	inodes := listenInodes(port)
	if len(inodes) == 0 {
		return 0, "", false
	}
	procs, _ := filepath.Glob("/proc/[0-9]*")
	for _, dir := range procs {
		fds, err := os.ReadDir(filepath.Join(dir, "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(dir, "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if !inodes[inode] {
				continue
			}
			n, err := strconv.Atoi(filepath.Base(dir))
			if err != nil {
				continue
			}
			return n, procCmd(dir), true
		}
	}
	return 0, "", false
}

// listenInodes returns the socket inodes in LISTEN state on port.
func listenInodes(port int) map[string]bool {
	inodes := map[string]bool{}
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Scan() // header
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			_, hexPort, found := strings.Cut(fields[1], ":")
			if !found {
				continue
			}
			if n, err := strconv.ParseUint(hexPort, 16, 32); err == nil && int(n) == port {
				inodes[fields[9]] = true
			}
		}
		f.Close()
	}
	return inodes
}

// procCmd is the command line of /proc/<pid>, falling back to comm.
func procCmd(dir string) string {
	if raw, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil && len(raw) > 0 {
		return strings.TrimSpace(strings.ReplaceAll(strings.TrimRight(string(raw), "\x00"), "\x00", " "))
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "comm")); err == nil {
		return strings.TrimSpace(string(raw))
	}
	return ""
}

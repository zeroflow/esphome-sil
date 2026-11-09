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

// claimedPorts maps every API port stored in an identity under
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
		if port, ok := storedPort(path); ok {
			if _, dup := claimed[port]; !dup {
				claimed[port] = name
			}
		}
	}
	return claimed
}

// storedPort reads api_port from one identity.json.
func storedPort(path string) (int, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var id struct {
		APIPort int `json:"api_port"`
	}
	if json.Unmarshal(raw, &id) != nil || id.APIPort < MinAPIPort || id.APIPort > MaxAPIPort {
		return 0, false
	}
	return id.APIPort, true
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

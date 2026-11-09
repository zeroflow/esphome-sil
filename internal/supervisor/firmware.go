package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

// StartSpec is everything needed to start the firmware once. The same spec
// (apart from Start) is used for every restart, so a restarted firmware gets
// the same program, preferences and environment.
type StartSpec struct {
	Program string
	Env     []string
	LogPath string
	// Dir is the working directory; Start numbers this launch (1 for the
	// first) and names the separator line written to the log.
	Dir   string
	Start int
}

// ExitInfo is how a firmware process ended. Code is nil when a signal ended
// it. Requested is set by the supervisor, not by the process: it is true when
// the supervisor asked for the stop.
type ExitInfo struct {
	Code      *int
	Signal    string
	Requested bool
}

// Process is one running firmware process.
type Process interface {
	PID() int
	// Wait blocks until the process has been reaped and reports how it ended.
	Wait() ExitInfo
	// SignalGroup signals the process group; it is a no-op once the process is gone.
	SignalGroup(sig syscall.Signal) error
}

// Starter launches firmware processes; tests inject a fake.
type Starter interface {
	Start(s StartSpec) (Process, error)
}

// ExecStarter is the real Starter. Each process is launched from a dedicated
// goroutine that calls runtime.LockOSThread and stays locked until the
// process has been reaped: Pdeathsig is tied to the OS thread that spawned
// the child, and Go may retire unlocked threads, which would kill the
// firmware while the supervisor is still running (A2). The child is its own
// process group (Setpgid) so a stop reaches everything it spawned, and gets
// SIGKILL when the supervisor dies for any reason (BUILD-07).
type ExecStarter struct{}

type execProcess struct {
	pid  int
	done chan struct{} // closed after info is set
	info ExitInfo
}

// Start implements Starter.
func (ExecStarter) Start(s StartSpec) (Process, error) {
	logf, err := os.OpenFile(s.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open firmware.log: %w", err)
	}
	p := &execProcess{done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // released only when this goroutine returns, after the child is reaped
		defer logf.Close()
		fmt.Fprintf(logf, "=== firmware start %d ===\n", s.Start)
		cmd := exec.Command(s.Program)
		cmd.Dir = s.Dir
		cmd.Env = s.Env
		cmd.Stdout, cmd.Stderr = logf, logf
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		p.pid = cmd.Process.Pid
		started <- nil
		err := cmd.Wait()
		p.info = exitInfo(cmd.ProcessState, err)
		close(p.done)
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return p, nil
}

// exitInfo converts a reaped process state.
func exitInfo(st *os.ProcessState, _ error) ExitInfo {
	var info ExitInfo
	if st == nil {
		return info
	}
	if ws, ok := st.Sys().(syscall.WaitStatus); ok {
		if ws.Signaled() {
			info.Signal = ws.Signal().String()
		} else {
			code := ws.ExitStatus()
			info.Code = &code
		}
	}
	return info
}

func (p *execProcess) PID() int { return p.pid }

func (p *execProcess) Wait() ExitInfo {
	<-p.done
	return p.info
}

func (p *execProcess) SignalGroup(sig syscall.Signal) error {
	select {
	case <-p.done:
		return nil // reaped: the pgid may be reused
	default:
	}
	if err := syscall.Kill(-p.pid, sig); err != nil && err != syscall.ESRCH {
		return err
	}
	return nil
}

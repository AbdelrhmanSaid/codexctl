// Package daemon passively detects the Codex app-server daemon, which caches
// auth.json until it is restarted.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"
)

type State int

const (
	NotRunning State = iota
	Running
	// Unknown means inconsistent evidence; such a daemon must not be
	// restarted.
	Unknown
)

type Status struct {
	State State
	PID   int
	// StartedAt is zero if it could not be determined.
	StartedAt time.Time
	Reason    string
}

// The second name is used by Codex's standalone package.
var pidFileNames = []string{"daemon.pid", "app-server.pid"}

func stateDir(codexHome string) string {
	return filepath.Join(codexHome, "app-server-daemon")
}

// SocketPath is, on Unix, a symlink to a socket with a short path.
func SocketPath(codexHome string) string {
	return filepath.Join(codexHome, "app-server-control", "app-server-control.sock")
}

// The start time fields are only written on macOS.
type pidFile struct {
	PID             int `json:"pid"`
	ProcessIdentity struct {
		StartSeconds      int64 `json:"startSeconds"`
		StartMicroseconds int64 `json:"startMicroseconds"`
	} `json:"processIdentity"`
}

func Detect(codexHome string) Status {
	result := Status{State: NotRunning}

	for _, name := range pidFileNames {
		status := detectPIDFile(filepath.Join(stateDir(codexHome), name), SocketPath(codexHome))

		switch {
		case status.State == Running:
			return status
		case status.State == Unknown && result.State != Unknown:
			result = status
		case status.State == NotRunning && result.State == NotRunning && result.PID == 0:
			// Keep the pid of an exited daemon for diagnostics.
			result = status
		}
	}

	return result
}

func detectPIDFile(path, socket string) Status {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Status{State: NotRunning}
	}

	if err != nil {
		return Status{State: Unknown, Reason: fmt.Sprintf("cannot read %s: %v", path, err)}
	}

	var record pidFile
	if err := json.Unmarshal(data, &record); err != nil || record.PID <= 0 {
		return Status{State: Unknown, Reason: path + " is not a valid daemon pid file"}
	}

	if !processAlive(record.PID) {
		return Status{State: NotRunning, PID: record.PID}
	}

	// The pid may have been reused, so the socket must answer too.
	if err := probeSocket(socket); err != nil {
		return Status{State: Unknown, PID: record.PID, Reason: fmt.Sprintf("process %d is alive but the daemon control socket did not answer: %v", record.PID, err)}
	}

	startedAt := time.Time{}
	if startSeconds := record.ProcessIdentity.StartSeconds; startSeconds > 0 {
		startedAt = time.Unix(startSeconds, record.ProcessIdentity.StartMicroseconds*int64(time.Microsecond))
	} else if info, err := os.Stat(path); err == nil {
		// Codex writes the pid file when the daemon starts.
		startedAt = info.ModTime()
	}

	return Status{State: Running, PID: record.PID, StartedAt: startedAt}
}

// Connects and hangs up without sending, as Codex's own doctor does.
func probeSocket(path string) error {
	// Dial the symlink's target to stay under the socket path limit.
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}

	conn, err := net.DialTimeout("unix", target, time.Second)
	if err != nil {
		return err
	}

	return conn.Close()
}

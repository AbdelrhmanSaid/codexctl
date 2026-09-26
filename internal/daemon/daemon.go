// Package daemon inspects the shared Codex app-server daemon that Codex
// v0.157.0 and later starts by default. The daemon loads auth.json once and
// caches the credentials, so it does not notice when codexctl replaces the
// file; only a restart makes it reload.
//
// Detection is passive: it reads the daemon's pid file and checks that its
// control socket accepts a connection. It never speaks the app-server
// protocol, never starts or stops a daemon, and never reads credentials.
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

// State is whether a daemon is running for a Codex home.
type State int

const (
	// NotRunning means no daemon pid file exists or its process has exited.
	NotRunning State = iota
	// Running means the recorded process is alive and the control socket
	// accepts connections.
	Running
	// Unknown means the evidence is inconsistent, for example a live pid with
	// no answering socket. A daemon must not be restarted in this state.
	Unknown
)

// Status describes the daemon of one Codex home.
type Status struct {
	State State
	PID   int
	// StartedAt is when the daemon process started, or zero if that could
	// not be determined. It is only set when the daemon is running.
	StartedAt time.Time
	// Reason explains an Unknown state.
	Reason string
}

// pidFileNames are the files in which Codex records the daemon process. The
// second is used by installations that still run Codex's standalone package.
var pidFileNames = []string{"daemon.pid", "app-server.pid"}

func stateDir(codexHome string) string {
	return filepath.Join(codexHome, "app-server-daemon")
}

// SocketPath is the daemon's control socket. On Unix it is a symlink to a
// socket with a short path, since socket paths are limited in length.
func SocketPath(codexHome string) string {
	return filepath.Join(codexHome, "app-server-control", "app-server-control.sock")
}

// pidFile is the part of Codex's pid record that detection needs. The start
// time fields are only written on macOS; elsewhere Codex records a
// platform-specific identity that cannot be turned into a time.
type pidFile struct {
	PID             int `json:"pid"`
	ProcessIdentity struct {
		StartSeconds      int64 `json:"startSeconds"`
		StartMicroseconds int64 `json:"startMicroseconds"`
	} `json:"processIdentity"`
}

// Detect reports whether a daemon is running for codexHome.
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
	var pf pidFile
	if err := json.Unmarshal(data, &pf); err != nil || pf.PID <= 0 {
		return Status{State: Unknown, Reason: path + " is not a valid daemon pid file"}
	}
	if !processAlive(pf.PID) {
		// A daemon that exited without cleaning up leaves a stale pid file.
		return Status{State: NotRunning, PID: pf.PID}
	}
	// The pid alone could belong to an unrelated process that reused the
	// number, so also require the control socket to accept a connection.
	if err := probeSocket(socket); err != nil {
		return Status{State: Unknown, PID: pf.PID, Reason: fmt.Sprintf("process %d is alive but the daemon control socket did not answer: %v", pf.PID, err)}
	}
	started := time.Time{}
	if s := pf.ProcessIdentity.StartSeconds; s > 0 {
		started = time.Unix(s, pf.ProcessIdentity.StartMicroseconds*int64(time.Microsecond))
	} else if info, err := os.Stat(path); err == nil {
		// Codex writes the pid file when the daemon starts.
		started = info.ModTime()
	}
	return Status{State: Running, PID: pf.PID, StartedAt: started}
}

// probeSocket connects to the control socket and hangs up without sending
// anything. Codex's own doctor probes the socket the same way, and the daemon
// treats the dropped connection as an abandoned client.
func probeSocket(path string) error {
	// Dial the symlink's target so a long Codex home path does not exceed the
	// socket path limit.
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

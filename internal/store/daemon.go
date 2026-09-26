package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/AbdelrhmanSaid/codexctl/internal/daemon"
)

// switchRecord notes the last time codexctl changed the active auth.json, so
// a daemon that started earlier can be recognized as still holding the
// previous account's credentials. It holds no credentials itself.
type switchRecord struct {
	CodexHome string    `json:"codex_home"`
	Profile   string    `json:"profile"`
	Time      time.Time `json:"time"`
}

// DaemonState describes the Codex app-server daemon relative to the active
// auth.json.
type DaemonState struct {
	daemon.Status
	// Stale means the daemon is running and started before codexctl last
	// changed auth.json, so it still uses the credentials it loaded then.
	Stale bool
	// Profile is the profile that change activated, or "" if it signed Codex
	// out. It is only set when Stale is true.
	Profile string
}

// AuthChanged reports whether this Store changed the active auth.json,
// including by finishing an interrupted switch.
func (s *Store) AuthChanged() bool { return s.authChanged }

// recordSwitch notes that the active auth.json now holds profile, or was
// removed when profile is "". The record only feeds daemon diagnostics, so
// failing to write it does not fail the switch.
func (s *Store) recordSwitch(profile string) {
	s.authChanged = true
	data, err := json.Marshal(switchRecord{CodexHome: s.CodexHome, Profile: profile, Time: time.Now()})
	if err != nil {
		return
	}
	_ = writeFile(s.switchedPath(), append(data, '\n'), 0o600)
}

// lastSwitch returns the last recorded switch for this Codex home.
func (s *Store) lastSwitch() (switchRecord, bool) {
	data, err := readFile(s.switchedPath())
	if err != nil {
		return switchRecord{}, false
	}
	var record switchRecord
	if json.Unmarshal(data, &record) != nil || record.Time.IsZero() {
		return switchRecord{}, false
	}
	// The state directory is shared by every CODEX_HOME; a switch made in
	// another one says nothing about this home's daemon.
	if filepath.Clean(record.CodexHome) != filepath.Clean(s.CodexHome) {
		return switchRecord{}, false
	}
	return record, true
}

// Daemon reports the app-server daemon of this Codex home and whether it
// predates the last account switch.
func (s *Store) Daemon() DaemonState {
	detect := s.DetectDaemon
	if detect == nil {
		detect = daemon.Detect
	}
	state := DaemonState{Status: detect(s.CodexHome)}
	if state.State != daemon.Running {
		return state
	}
	// A daemon whose start time is unknown is assumed to predate the switch:
	// a needless restart costs less than silently using the wrong account.
	if record, ok := s.lastSwitch(); ok && (state.StartedAt.IsZero() || state.StartedAt.Before(record.Time)) {
		state.Stale = true
		state.Profile = record.Profile
	}
	return state
}

func (s *Store) daemonCheck() Check {
	state := s.Daemon()
	switch {
	case state.State == daemon.NotRunning:
		return Check{"no Codex app-server daemon is running", false}
	case state.State == daemon.Unknown:
		return Check{"cannot tell whether a Codex app-server daemon is running: " + state.Reason, true}
	case state.Stale:
		account := fmt.Sprintf("profile %q", state.Profile)
		if state.Profile == "" {
			account = "a signed-out auth.json"
		}
		return Check{fmt.Sprintf("Codex app-server daemon (pid %d) started before codexctl switched to %s and still uses the previous credentials; run 'codexctl restart-daemon' to reload it (this interrupts active Codex sessions)", state.PID, account), true}
	default:
		return Check{fmt.Sprintf("Codex app-server daemon (pid %d) started after the last account switch", state.PID), false}
	}
}

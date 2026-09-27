package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/AbdelrhmanSaid/codexctl/internal/daemon"
)

// Lets a daemon that started before the last switch be recognized. Holds no
// credentials.
type switchRecord struct {
	CodexHome string    `json:"codex_home"`
	Profile   string    `json:"profile"`
	Time      time.Time `json:"time"`
}

type DaemonState struct {
	daemon.Status
	// Stale means running and started before auth.json last changed.
	Stale bool
	// Profile is "" if the change signed Codex out.
	Profile string
}

// Only feeds diagnostics, so a failed write does not fail the switch.
func (op *operation) recordSwitch(profile string) {
	op.result.AuthChanged = true
	data, err := json.Marshal(switchRecord{CodexHome: op.CodexHome, Profile: profile, Time: time.Now()})
	if err != nil {
		return
	}
	_ = writeFile(op.switchedPath(), append(data, '\n'), 0o600)
}

func (s *Store) lastSwitch() (switchRecord, bool) {
	data, err := readFile(s.switchedPath())
	if err != nil {
		return switchRecord{}, false
	}
	var record switchRecord
	if json.Unmarshal(data, &record) != nil || record.Time.IsZero() {
		return switchRecord{}, false
	}
	// The state directory is shared by every CODEX_HOME.
	if filepath.Clean(record.CodexHome) != filepath.Clean(s.CodexHome) {
		return switchRecord{}, false
	}
	return record, true
}

func (s *Store) Daemon() DaemonState {
	detect := s.DetectDaemon
	if detect == nil {
		detect = daemon.Detect
	}
	state := DaemonState{Status: detect(s.CodexHome)}
	if state.State != daemon.Running {
		return state
	}
	// An unknown start time counts as stale: a needless restart beats the
	// wrong account.
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

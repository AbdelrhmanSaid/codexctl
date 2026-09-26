package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdelrhmanSaid/codexctl/internal/daemon"
)

func fakeDaemon(status daemon.Status) func(string) daemon.Status {
	return func(string) daemon.Status { return status }
}

// reopen returns a fresh Store on the same directories, as a new codexctl
// process would see them.
func reopen(s *Store) *Store {
	return &Store{CodexHome: s.CodexHome, StateHome: s.StateHome, DetectDaemon: s.DetectDaemon}
}

func assertSwitch(t *testing.T, s *Store, profile string) {
	t.Helper()
	record, ok := s.lastSwitch()
	if !ok {
		t.Fatal("no switch was recorded")
	}
	if record.Profile != profile {
		t.Fatalf("recorded profile = %q, want %q", record.Profile, profile)
	}
	if filepath.Clean(record.CodexHome) != filepath.Clean(s.CodexHome) {
		t.Fatalf("recorded Codex home = %q, want %q", record.CodexHome, s.CodexHome)
	}
	if age := time.Since(record.Time); age < 0 || age > time.Minute {
		t.Fatalf("recorded time %v is not recent", record.Time)
	}
}

func TestLoginAndUseRecordSwitch(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	if !s.AuthChanged() {
		t.Fatal("login did not report an auth change")
	}
	assertSwitch(t, s, "a")

	mustLogin(t, s, "b", chatgptAuth(t, "acct-b", "r1"))
	s = reopen(s)
	if s.AuthChanged() {
		t.Fatal("a fresh store reports an auth change")
	}
	if _, err := s.Use("a"); err != nil {
		t.Fatal(err)
	}
	if !s.AuthChanged() {
		t.Fatal("use did not report an auth change")
	}
	assertSwitch(t, s, "a")
}

func TestCommandsThatKeepAuthDoNotRecordSwitch(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	mustLogin(t, s, "b", chatgptAuth(t, "acct-b", "r1"))

	s = reopen(s)
	if _, err := s.Rename("b", "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := s.Import("d"); err == nil {
		t.Fatal("import of a saved account succeeded")
	}
	if _, err := s.Logout("a", func(string) error { return nil }); err == nil {
		t.Fatal("logout of a removed profile succeeded")
	}
	if s.AuthChanged() {
		t.Fatal("auth was reported changed")
	}
	assertSwitch(t, s, "b")
}

func TestLogoutRecordsSignOut(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	mustLogin(t, s, "b", chatgptAuth(t, "acct-b", "r1"))

	// Logging out an unselected profile leaves auth.json alone.
	s = reopen(s)
	if _, err := s.Logout("a", func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if s.AuthChanged() {
		t.Fatal("logging out an unselected profile reported an auth change")
	}

	s = reopen(s)
	if _, err := s.Logout("b", func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !s.AuthChanged() {
		t.Fatal("removing auth.json did not report an auth change")
	}
	assertMissing(t, s.authPath())
	assertSwitch(t, s, "")
}

func TestFinishingInterruptedSwitchRecordsIt(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	mustLogin(t, s, "b", chatgptAuth(t, "acct-b", "r1"))
	writeBytes(t, s.pendingPath(), []byte("a\n"))

	s = reopen(s)
	if _, err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	if !s.AuthChanged() {
		t.Fatal("recovery did not report an auth change")
	}
	assertSwitch(t, s, "a")
}

func TestDaemonStaleness(t *testing.T) {
	before := daemon.Status{State: daemon.Running, PID: 7, StartedAt: time.Now().Add(-time.Hour)}
	after := daemon.Status{State: daemon.Running, PID: 7, StartedAt: time.Now().Add(time.Hour)}
	unknownStart := daemon.Status{State: daemon.Running, PID: 7}
	tests := []struct {
		name   string
		status daemon.Status
		stale  bool
	}{
		{"not running", daemon.Status{State: daemon.NotRunning}, false},
		{"unknown", daemon.Status{State: daemon.Unknown, Reason: "x"}, false},
		{"started after switch", after, false},
		{"started before switch", before, true},
		{"unknown start time", unknownStart, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
			s.DetectDaemon = fakeDaemon(tt.status)
			state := s.Daemon()
			if state.Status != tt.status {
				t.Fatalf("status = %+v, want %+v", state.Status, tt.status)
			}
			if state.Stale != tt.stale {
				t.Fatalf("stale = %v, want %v", state.Stale, tt.stale)
			}
			if tt.stale && state.Profile != "a" {
				t.Fatalf("profile = %q, want a", state.Profile)
			}
		})
	}
}

func TestDaemonIsNotStaleWithoutSwitch(t *testing.T) {
	s := newTestStore(t)
	s.DetectDaemon = fakeDaemon(daemon.Status{State: daemon.Running, PID: 7})
	if s.Daemon().Stale {
		t.Fatal("daemon is stale although codexctl never switched")
	}
}

func TestDaemonIgnoresSwitchInAnotherCodexHome(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	other := &Store{
		CodexHome:    filepath.Join(t.TempDir(), "other"),
		StateHome:    s.StateHome,
		DetectDaemon: fakeDaemon(daemon.Status{State: daemon.Running, PID: 7}),
	}
	if other.Daemon().Stale {
		t.Fatal("a switch in another CODEX_HOME made this home's daemon stale")
	}
}

func TestDoctorReportsDaemon(t *testing.T) {
	tests := []struct {
		name    string
		status  daemon.Status
		warning bool
		want    string
	}{
		{"not running", daemon.Status{State: daemon.NotRunning}, false, "no Codex app-server daemon is running"},
		{"unknown", daemon.Status{State: daemon.Unknown, Reason: "socket did not answer"}, true, "socket did not answer"},
		{"fresh", daemon.Status{State: daemon.Running, PID: 7, StartedAt: time.Now().Add(time.Hour)}, false, "pid 7"},
		{"stale", daemon.Status{State: daemon.Running, PID: 7, StartedAt: time.Now().Add(-time.Hour)}, true, "codexctl restart-daemon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
			s.DetectDaemon = fakeDaemon(tt.status)
			checks := s.Doctor()
			check := checks[len(checks)-1]
			if check.Warning != tt.warning || !strings.Contains(check.Message, tt.want) {
				t.Fatalf("check = %+v, want warning=%v containing %q", check, tt.warning, tt.want)
			}
			if tt.name == "stale" && !strings.Contains(check.Message, `profile "a"`) {
				t.Fatalf("stale check %q does not name the profile", check.Message)
			}
		})
	}
}

func TestDoctorNamesSignedOutSwitch(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "r1"))
	if _, err := s.Logout("a", func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	s.DetectDaemon = fakeDaemon(daemon.Status{State: daemon.Running, PID: 7, StartedAt: time.Now().Add(-time.Hour)})
	checks := s.Doctor()
	if msg := checks[len(checks)-1].Message; !strings.Contains(msg, "signed-out") {
		t.Fatalf("check %q does not mention the sign-out", msg)
	}
}

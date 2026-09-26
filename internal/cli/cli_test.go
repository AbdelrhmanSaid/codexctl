package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
	"github.com/AbdelrhmanSaid/codexctl/internal/daemon"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"
)

// fakeCodex stands in for the codex executable. Login writes auth for the
// named account; RestartDaemon records the Codex home it was asked to
// restart.
type fakeCodex struct {
	restarted  []string
	restartErr error
}

func authJSON(account string) []byte {
	return fmt.Appendf(nil, `{"OPENAI_API_KEY":"key-%s"}`, account)
}

func (f *fakeCodex) Login(home string, _ codex.LoginOptions, _ codex.Stdio) error {
	return os.WriteFile(filepath.Join(home, "auth.json"), authJSON("login"), 0o600)
}

func (f *fakeCodex) Logout(string, codex.Stdio) error { return nil }

func (f *fakeCodex) RestartDaemon(home string, stdio codex.Stdio) error {
	f.restarted = append(f.restarted, home)
	fmt.Fprintln(stdio.Out, `{"status":"restarted"}`)
	return f.restartErr
}

type harness struct {
	codexHome, stateHome string
	codex                *fakeCodex
	codexErr             error
	daemon               daemon.Status
	app                  *app
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{
		codexHome: filepath.Join(root, "codex"),
		stateHome: filepath.Join(root, "codexctl"),
		codex:     &fakeCodex{},
	}
	h.app = &app{
		openStore: func() (*store.Store, error) { return h.store(), nil },
		findCodex: func() (codexCLI, error) {
			if h.codexErr != nil {
				return nil, h.codexErr
			}
			return h.codex, nil
		},
		interactive: true,
	}
	return h
}

// store returns a fresh Store, as every codexctl invocation gets.
func (h *harness) store() *store.Store {
	return &store.Store{
		CodexHome:    h.codexHome,
		StateHome:    h.stateHome,
		DetectDaemon: func(string) daemon.Status { return h.daemon },
	}
}

func (h *harness) seed(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		data := authJSON(name)
		if _, err := h.store().Login(name, func(home string) error {
			return os.WriteFile(filepath.Join(home, "auth.json"), data, 0o600)
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func (h *harness) run(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := h.app.newRootCommand(strings.NewReader(stdin), &out, &errOut)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

func running(pid int) daemon.Status {
	return daemon.Status{State: daemon.Running, PID: pid, StartedAt: time.Now().Add(-time.Hour)}
}

func assertRestarted(t *testing.T, h *harness, times int) {
	t.Helper()
	if len(h.codex.restarted) != times {
		t.Fatalf("daemon was restarted %d time(s), want %d", len(h.codex.restarted), times)
	}
	for _, home := range h.codex.restarted {
		if home != h.codexHome {
			t.Fatalf("restarted daemon of %q, want %q", home, h.codexHome)
		}
	}
}

func assertContains(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("output does not contain %q:\n%s", want, text)
	}
}

func TestUseRestartsDaemonWhenConfirmed(t *testing.T) {
	for _, answer := range []string{"y\n", "Y\n", "yes\n", " Yes \n"} {
		h := newHarness(t)
		h.seed(t, "a", "b")
		h.daemon = running(42)
		stdout, stderr, err := h.run(t, answer, "use", "a")
		if err != nil {
			t.Fatal(err)
		}
		assertRestarted(t, h, 1)
		assertContains(t, stderr, "pid 42")
		assertContains(t, stderr, "interrupts active Codex sessions")
		assertContains(t, stderr, "Restart it now? [y/N]")
		assertContains(t, stdout, `Now using profile "a"`)
		assertContains(t, stdout, "Restarted the Codex app-server daemon")
	}
}

func TestUseLeavesDaemonUnlessConfirmed(t *testing.T) {
	for _, answer := range []string{"", "\n", "n\n", "no\n", "maybe\n"} {
		h := newHarness(t)
		h.seed(t, "a", "b")
		h.daemon = running(42)
		_, stderr, err := h.run(t, answer, "use", "a")
		if err != nil {
			t.Fatal(err)
		}
		assertRestarted(t, h, 0)
		assertContains(t, stderr, "Left the daemon running")
		assertContains(t, stderr, "codexctl restart-daemon")
	}
}

func TestNonInteractiveUseNeverRestarts(t *testing.T) {
	h := newHarness(t)
	h.app.interactive = false
	h.seed(t, "a", "b")
	h.daemon = running(42)
	_, stderr, err := h.run(t, "y\n", "use", "a")
	if err != nil {
		t.Fatal(err)
	}
	assertRestarted(t, h, 0)
	assertContains(t, stderr, "still uses the previous credentials")
	assertContains(t, stderr, "Run 'codexctl restart-daemon'")
	if strings.Contains(stderr, "[y/N]") {
		t.Fatalf("a non-interactive command asked a question:\n%s", stderr)
	}
}

func TestUseWithoutDaemonSaysNothingAboutIt(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "a", "b")
	_, stderr, err := h.run(t, "y\n", "use", "a")
	if err != nil {
		t.Fatal(err)
	}
	assertRestarted(t, h, 0)
	if stderr != "" {
		t.Fatalf("unexpected stderr:\n%s", stderr)
	}
}

func TestUseWithUnknownDaemonWarnsAndNeverRestarts(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "a", "b")
	h.daemon = daemon.Status{State: daemon.Unknown, Reason: "process 42 is alive but the daemon control socket did not answer"}
	_, stderr, err := h.run(t, "y\n", "use", "a")
	if err != nil {
		t.Fatal(err)
	}
	assertRestarted(t, h, 0)
	assertContains(t, stderr, "warning: cannot tell whether a Codex app-server daemon is running")
	assertContains(t, stderr, "socket did not answer")
}

func TestFailedRestartIsReportedAfterSuccessfulSwitch(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "a", "b")
	h.daemon = running(42)
	h.codex.restartErr = errors.New("exit status 1")
	stdout, stderr, err := h.run(t, "y\n", "use", "a")
	if err != nil {
		t.Fatal(err)
	}
	assertRestarted(t, h, 1)
	assertContains(t, stdout, `Now using profile "a"`)
	assertContains(t, stderr, "warning: restarting the Codex app-server daemon failed")
	assertContains(t, stderr, "exit status 1")
	if strings.Contains(stdout, "Restarted the Codex") {
		t.Fatalf("a failed restart was reported as a success:\n%s", stdout)
	}
	if name, _, _ := h.store().Current(); name != "a" {
		t.Fatalf("selected profile = %q, want a", name)
	}
}

func TestMissingCodexIsReportedWhenRestarting(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "a", "b")
	h.daemon = running(42)
	h.codexErr = errors.New("codex executable was not found in PATH")
	_, stderr, err := h.run(t, "y\n", "use", "a")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, stderr, "warning: cannot restart the daemon: codex executable was not found in PATH")
}

func TestLoginAndLogoutOfferRestart(t *testing.T) {
	h := newHarness(t)
	h.daemon = running(42)
	if _, stderr, err := h.run(t, "y\n", "login", "c"); err != nil {
		t.Fatal(err, stderr)
	}
	assertRestarted(t, h, 1)
	if _, stderr, err := h.run(t, "y\n", "logout", "c"); err != nil {
		t.Fatal(err, stderr)
	}
	assertRestarted(t, h, 2)
}

func TestCommandsThatKeepAuthDoNotOfferRestart(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "a", "b")
	h.daemon = running(42)
	for _, args := range [][]string{{"rename", "a", "z"}, {"remove", "z"}, {"sync"}, {"logout", "b"}} {
		// Logging out the selected profile "b" removes auth.json, which does
		// count as a change; log out an unselected one instead.
		if args[0] == "logout" {
			h.seed(t, "c")
			args = []string{"logout", "b"}
		}
		_, stderr, err := h.run(t, "y\n", args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(stderr, "daemon") {
			t.Fatalf("%v mentioned the daemon:\n%s", args, stderr)
		}
	}
	assertRestarted(t, h, 0)
}

func TestRecoveredSwitchOffersRestart(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "a", "b")
	h.daemon = running(42)
	if err := os.WriteFile(filepath.Join(h.stateHome, "pending"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.run(t, "y\n", "sync"); err != nil {
		t.Fatal(err)
	}
	assertRestarted(t, h, 1)
}

func TestRestartDaemonCommand(t *testing.T) {
	h := newHarness(t)
	h.daemon = running(42)
	stdout, stderr, err := h.run(t, "", "restart-daemon")
	if err != nil {
		t.Fatal(err)
	}
	assertRestarted(t, h, 1)
	assertContains(t, stderr, "pid 42")
	assertContains(t, stderr, "interrupts active Codex sessions")
	assertContains(t, stdout, "Restarted the Codex app-server daemon")
}

func TestRestartDaemonRefusesWithoutRunningDaemon(t *testing.T) {
	tests := []struct {
		name   string
		status daemon.Status
		want   string
	}{
		{"not running", daemon.Status{State: daemon.NotRunning}, "no Codex app-server daemon is running"},
		{"unknown", daemon.Status{State: daemon.Unknown, Reason: "pid file is garbage"}, "refusing to restart"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.daemon = tt.status
			_, _, err := h.run(t, "", "restart-daemon")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if tt.status.Reason != "" && !strings.Contains(err.Error(), tt.status.Reason) {
				t.Fatalf("err %q does not give the reason", err)
			}
			assertRestarted(t, h, 0)
		})
	}
}

func TestRestartDaemonFailures(t *testing.T) {
	h := newHarness(t)
	h.daemon = running(42)
	h.codex.restartErr = errors.New("exit status 1")
	if _, _, err := h.run(t, "", "restart-daemon"); err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("err = %v, want the codex failure", err)
	}
	h.codexErr = errors.New("codex executable was not found in PATH")
	if _, _, err := h.run(t, "", "restart-daemon"); err == nil || !strings.Contains(err.Error(), "not found in PATH") {
		t.Fatalf("err = %v, want the missing codex error", err)
	}
}

func TestCurrentWarnsAboutStaleDaemon(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "a")
	h.daemon = running(42)
	stdout, stderr, err := h.run(t, "", "current")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "a\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	assertContains(t, stderr, "warning: the Codex app-server daemon (pid 42) started before this profile was selected")
	assertContains(t, stderr, "codexctl restart-daemon")

	stdout, _, err = h.run(t, "", "current", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Name        string `json:"name"`
		DaemonStale bool   `json:"daemon_stale"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "a" || !got.DaemonStale {
		t.Fatalf("json = %s", stdout)
	}

	h.daemon = daemon.Status{State: daemon.Running, PID: 42, StartedAt: time.Now().Add(time.Hour)}
	if _, stderr, err = h.run(t, "", "current"); err != nil || stderr != "" {
		t.Fatalf("fresh daemon: err = %v, stderr = %q", err, stderr)
	}
}

func TestDoctorReportsStaleDaemon(t *testing.T) {
	h := newHarness(t)
	h.seed(t, "a")
	h.daemon = running(42)
	stdout, _, err := h.run(t, "", "doctor")
	if err == nil {
		t.Fatal("doctor passed with a stale daemon")
	}
	assertContains(t, stdout, "warn Codex app-server daemon (pid 42) started before codexctl switched to profile \"a\"")
	assertContains(t, stdout, "codexctl restart-daemon")

	h.daemon = daemon.Status{State: daemon.NotRunning}
	if stdout, _, err = h.run(t, "", "doctor"); err != nil {
		t.Fatalf("doctor failed without a daemon: %v\n%s", err, stdout)
	}
	assertContains(t, stdout, "ok   no Codex app-server daemon is running")
}

package daemon

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writePIDFile(t *testing.T, codexHome, name, contents string) string {
	t.Helper()
	path := filepath.Join(stateDir(codexHome), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func pidRecord(pid int, startSeconds int64) string {
	if startSeconds == 0 {
		return fmt.Sprintf(`{"pid":%d,"processStartTime":"Sat Sep 26 20:36:51 2026"}`, pid)
	}
	return fmt.Sprintf(`{"pid":%d,"processStartTime":"Sat Sep 26 20:36:51 2026","processIdentity":{"bootId":"b","uniqueId":1,"startSeconds":%d,"startMicroseconds":500000}}`, pid, startSeconds)
}

// serveControlSocket listens where Detect expects the daemon's control
// socket and accepts and drops connections, like an idle daemon would. It
// returns the listener so a test can shut it down early.
func serveControlSocket(t *testing.T, codexHome string) net.Listener {
	t.Helper()
	link := SocketPath(codexHome)
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	// Unix socket paths are short-limited, and a test's temporary directory
	// can exceed the limit, so mirror Codex: listen on a short path and
	// symlink to it. Windows has no such limit and does not need symlinks.
	path := link
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("", "cx")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		path = filepath.Join(dir, "s")
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("cannot listen on a unix socket: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	if path != link {
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	return l
}

func TestDetectWithoutPIDFile(t *testing.T) {
	if got := Detect(t.TempDir()); got.State != NotRunning {
		t.Fatalf("state = %v, want NotRunning", got)
	}
}

func TestDetectInvalidPIDFile(t *testing.T) {
	for _, contents := range []string{"", "not json", `{"pid":0}`, `{"pid":-3}`} {
		home := t.TempDir()
		writePIDFile(t, home, "daemon.pid", contents)
		got := Detect(home)
		if got.State != Unknown || !strings.Contains(got.Reason, "not a valid daemon pid file") {
			t.Fatalf("pid file %q: got %+v, want Unknown", contents, got)
		}
	}
}

func TestDetectExitedProcess(t *testing.T) {
	// Run and reap a short-lived process so its pid is known to be dead.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	writePIDFile(t, home, "daemon.pid", pidRecord(cmd.Process.Pid, 0))
	got := Detect(home)
	if got.State != NotRunning || got.PID != cmd.Process.Pid {
		t.Fatalf("got %+v, want NotRunning for exited pid %d", got, cmd.Process.Pid)
	}
}

func TestDetectLiveProcessWithoutSocketIsUnknown(t *testing.T) {
	home := t.TempDir()
	writePIDFile(t, home, "daemon.pid", pidRecord(os.Getpid(), 0))
	got := Detect(home)
	if got.State != Unknown || got.PID != os.Getpid() || !strings.Contains(got.Reason, "control socket did not answer") {
		t.Fatalf("got %+v, want Unknown with a socket reason", got)
	}
}

func TestDetectRunning(t *testing.T) {
	home := t.TempDir()
	serveControlSocket(t, home)
	writePIDFile(t, home, "daemon.pid", pidRecord(os.Getpid(), 1790444211))
	got := Detect(home)
	if got.State != Running || got.PID != os.Getpid() {
		t.Fatalf("got %+v, want Running", got)
	}
	if want := time.Unix(1790444211, 500000000); !got.StartedAt.Equal(want) {
		t.Fatalf("StartedAt = %v, want %v", got.StartedAt, want)
	}
}

func TestDetectFallsBackToPIDFileModTime(t *testing.T) {
	home := t.TempDir()
	serveControlSocket(t, home)
	path := writePIDFile(t, home, "daemon.pid", pidRecord(os.Getpid(), 0))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got := Detect(home)
	if got.State != Running || !got.StartedAt.Equal(info.ModTime()) {
		t.Fatalf("got %+v, want Running started at %v", got, info.ModTime())
	}
}

func TestDetectStaleSocketIsUnknown(t *testing.T) {
	home := t.TempDir()
	l := serveControlSocket(t, home)
	if ul, ok := l.(*net.UnixListener); ok {
		// Leave the socket file behind, as a crashed daemon would.
		ul.SetUnlinkOnClose(false)
	}
	l.Close()
	writePIDFile(t, home, "daemon.pid", pidRecord(os.Getpid(), 0))
	if got := Detect(home); got.State != Unknown {
		t.Fatalf("got %+v, want Unknown", got)
	}
}

func TestDetectReadsLegacyPIDFile(t *testing.T) {
	home := t.TempDir()
	serveControlSocket(t, home)
	writePIDFile(t, home, "app-server.pid", pidRecord(os.Getpid(), 0))
	if got := Detect(home); got.State != Running {
		t.Fatalf("got %+v, want Running from app-server.pid", got)
	}
	// A running daemon wins over an unreadable record of the other kind.
	writePIDFile(t, home, "daemon.pid", "garbage")
	if got := Detect(home); got.State != Running {
		t.Fatalf("got %+v, want Running despite invalid daemon.pid", got)
	}
}

func TestDetectPrefersUnknownOverNotRunning(t *testing.T) {
	home := t.TempDir()
	writePIDFile(t, home, "daemon.pid", "garbage")
	if got := Detect(home); got.State != Unknown {
		t.Fatalf("got %+v, want Unknown", got)
	}
}

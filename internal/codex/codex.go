// Package codex runs the official Codex CLI as a child process.
package codex

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

// CLI is a located codex executable.
type CLI struct {
	Path string
}

// LoginOptions selects the `codex login` method. At most one may be set; the
// zero value is the default browser login.
type LoginOptions struct {
	DeviceAuth  bool
	APIKey      bool
	AccessToken bool
}

// Stdio is what the child process is attached to.
type Stdio struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Find locates codex in PATH.
func Find() (*CLI, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return nil, errors.New("codex executable was not found in PATH")
	}
	return &CLI{Path: path}, nil
}

// Login runs `codex login` with home as its CODEX_HOME, so the login cannot
// touch the user's real Codex directory.
func (c *CLI) Login(home string, opts LoginOptions, stdio Stdio) error {
	child := exec.Command(c.Path, opts.args()...)
	child.Env = setEnv(os.Environ(), "CODEX_HOME", home)
	child.Env = setEnv(child.Env, "CODEX_SQLITE_HOME", home)
	child.Stdin, child.Stdout, child.Stderr = stdio.In, stdio.Out, stdio.Err
	return child.Run()
}

// Logout runs `codex logout` with home as its CODEX_HOME. The caller places
// the credentials to revoke in that directory, so the user's real Codex
// directory is never touched.
func (c *CLI) Logout(home string, stdio Stdio) error {
	child := exec.Command(c.Path, "logout")
	child.Env = setEnv(os.Environ(), "CODEX_HOME", home)
	child.Env = setEnv(child.Env, "CODEX_SQLITE_HOME", home)
	child.Stdin, child.Stdout, child.Stderr = stdio.In, stdio.Out, stdio.Err
	return child.Run()
}

// RestartDaemon runs `codex app-server daemon restart` for the daemon of the
// Codex home at home, which is the only supported way to make it reload
// auth.json. It interrupts every session running on the daemon. Codex starts
// a daemon if none is running, so the caller must first check that one is.
func (c *CLI) RestartDaemon(home string, stdio Stdio) error {
	child := exec.Command(c.Path, "app-server", "daemon", "restart")
	child.Env = setEnv(os.Environ(), "CODEX_HOME", home)
	child.Stdin, child.Stdout, child.Stderr = stdio.In, stdio.Out, stdio.Err
	return child.Run()
}

func (o LoginOptions) args() []string {
	args := []string{"login"}
	if o.DeviceAuth {
		args = append(args, "--device-auth")
	}
	if o.APIKey {
		args = append(args, "--with-api-key")
	}
	if o.AccessToken {
		args = append(args, "--with-access-token")
	}
	return args
}

// setEnv returns env with every existing assignment of key replaced by a
// single key=value entry.
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

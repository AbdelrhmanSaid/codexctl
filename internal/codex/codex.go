// Package codex runs the official Codex CLI as a child process.
package codex

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

type CLI struct {
	Path string
}

// LoginOptions may set at most one method; the zero value is a browser login.
type LoginOptions struct {
	DeviceAuth  bool
	APIKey      bool
	AccessToken bool
}

type Stdio struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

func Find() (*CLI, error) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return nil, errors.New("codex executable was not found in PATH")
	}
	return &CLI{Path: path}, nil
}

// Login uses home as CODEX_HOME, so the real Codex directory is not touched.
func (c *CLI) Login(home string, opts LoginOptions, stdio Stdio) error {
	return c.run(stdio, isolated(home), opts.args()...)
}

func (c *CLI) Logout(home string, stdio Stdio) error {
	return c.run(stdio, isolated(home), "logout")
}

// RestartDaemon interrupts every session. Codex starts a daemon if none is
// running, so the caller must check first.
func (c *CLI) RestartDaemon(home string, stdio Stdio) error {
	return c.run(stdio, setEnv(os.Environ(), "CODEX_HOME", home), "app-server", "daemon", "restart")
}

func isolated(home string) []string {
	return setEnv(setEnv(os.Environ(), "CODEX_HOME", home), "CODEX_SQLITE_HOME", home)
}

func (c *CLI) run(stdio Stdio, env []string, args ...string) error {
	child := exec.Command(c.Path, args...)
	child.Env = env
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

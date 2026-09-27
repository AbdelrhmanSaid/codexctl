package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Usage struct {
	AccountID string  `json:"account_id,omitempty"`
	Primary   *Window `json:"primary,omitempty"`
	Secondary *Window `json:"secondary,omitempty"`
}

type Window struct {
	UsedPercent float64   `json:"used_percent"`
	Minutes     int       `json:"window_minutes,omitempty"`
	ResetsAt    time.Time `json:"resets_at,omitzero"`
}

// Includes refreshing an expired login.
const usageTimeout = 45 * time.Second

// Usage asks a short-lived app server running on home.
func (c *CLI) Usage(ctx context.Context, home string, isolatedHome bool) (Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, usageTimeout)
	defer cancel()

	env := setEnv(os.Environ(), "CODEX_HOME", home)
	if isolatedHome {
		env = isolated(home)
	}

	child := exec.CommandContext(ctx, c.Path, "app-server")
	child.Env = env
	// Codex may be writing refreshed credentials, so it is interrupted
	// before it is killed.
	if runtime.GOOS != "windows" {
		child.Cancel = func() error { return child.Process.Signal(os.Interrupt) }
	}

	child.WaitDelay = 5 * time.Second

	var stderr bytes.Buffer
	child.Stderr = &stderr

	stdin, err := child.StdinPipe()
	if err != nil {
		return Usage{}, err
	}

	stdout, err := child.StdoutPipe()
	if err != nil {
		return Usage{}, err
	}

	if err := child.Start(); err != nil {
		return Usage{}, fmt.Errorf("start codex app-server: %w", err)
	}

	usage, err := readUsage(stdin, stdout)
	stdin.Close() // asks the app server to exit
	_ = child.Wait()

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Usage{}, fmt.Errorf("codex app-server did not answer within %s", usageTimeout)
		}

		if lastStderr := lastLine(stderr.String()); lastStderr != "" {
			return Usage{}, fmt.Errorf("%w: %s", err, lastStderr)
		}

		return Usage{}, err
	}

	return usage, nil
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}

type rpcMessage struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type rateLimitsResult struct {
	AccountID  string `json:"accountId"`
	RateLimits struct {
		Primary   *rpcWindow `json:"primary"`
		Secondary *rpcWindow `json:"secondary"`
	} `json:"rateLimits"`
}

type rpcWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins *int    `json:"windowDurationMins"`
	ResetsAt           *int64  `json:"resetsAt"`
}

func (w *rpcWindow) window() *Window {
	if w == nil {
		return nil
	}

	window := &Window{UsedPercent: w.UsedPercent}

	if w.WindowDurationMins != nil {
		window.Minutes = *w.WindowDurationMins
	}

	if w.ResetsAt != nil {
		window.ResetsAt = time.Unix(*w.ResetsAt, 0)
	}

	return window
}

// JSON-RPC, one message per line, with notifications between responses.
func readUsage(stdin io.Writer, stdout io.Reader) (Usage, error) {
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	call := func(id int, method string, params any) (json.RawMessage, error) {
		request := map[string]any{"id": id, "method": method}
		if params != nil {
			request["params"] = params
		}

		if err := writeLine(stdin, request); err != nil {
			return nil, fmt.Errorf("codex app-server: %w", err)
		}

		for lines.Scan() {
			var message rpcMessage
			if json.Unmarshal(lines.Bytes(), &message) != nil || message.ID == nil || *message.ID != id {
				continue
			}

			if message.Error != nil {
				return nil, fmt.Errorf("codex app-server: %s: %s", method, message.Error.Message)
			}

			return message.Result, nil
		}

		if err := lines.Err(); err != nil {
			return nil, fmt.Errorf("codex app-server: %w", err)
		}

		return nil, errors.New("codex app-server exited without answering")
	}

	clientInfo := map[string]any{"name": "codexctl", "title": "codexctl", "version": "1"}
	if _, err := call(1, "initialize", map[string]any{"clientInfo": clientInfo}); err != nil {
		return Usage{}, err
	}

	if err := writeLine(stdin, map[string]any{"method": "initialized"}); err != nil {
		return Usage{}, fmt.Errorf("codex app-server: %w", err)
	}

	rawResult, err := call(2, "account/rateLimits/read", nil)
	if err != nil {
		return Usage{}, err
	}

	var result rateLimitsResult
	if err := json.Unmarshal(rawResult, &result); err != nil {
		return Usage{}, fmt.Errorf("codex app-server sent unreadable rate limits: %w", err)
	}

	return Usage{
		AccountID: result.AccountID,
		Primary:   result.RateLimits.Primary.window(),
		Secondary: result.RateLimits.Secondary.window(),
	}, nil
}

func writeLine(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	_, err = w.Write(append(data, '\n'))
	return err
}

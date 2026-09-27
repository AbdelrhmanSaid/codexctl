package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/AbdelrhmanSaid/codexctl/internal/codex"
	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/spf13/cobra"
)

// Secrets are read with a masked input, so they stay out of shell history.
func (a *app) chooseLoginMethod(cmd *cobra.Command) (codex.LoginOptions, string, error) {
	methods := []tui.Item{
		{Label: "Browser", Detail: "sign in with ChatGPT in your web browser"},
		{Label: "Device code", Detail: "sign in with ChatGPT from another device"},
		{Label: "API key", Detail: "paste an OpenAI API key"},
		{Label: "Access token", Detail: "paste an access token"},
	}
	i, err := tui.Select(env(cmd), tui.SelectOptions{Title: "How do you want to log in?", Items: methods})
	if err != nil {
		return codex.LoginOptions{}, "", err
	}
	var opts codex.LoginOptions
	switch i {
	case 0:
		return opts, "", nil
	case 1:
		opts.DeviceAuth = true
		return opts, "", nil
	case 2:
		opts.APIKey = true
	default:
		opts.AccessToken = true
	}
	secret, err := tui.Input(env(cmd), tui.InputOptions{Title: methods[i].Label, Secret: true})
	if err != nil {
		return codex.LoginOptions{}, "", err
	}
	return opts, secret, nil
}

// A browser or device login keeps the terminal; a pasted secret is fed out of
// sight.
func (a *app) runLogin(cmd *cobra.Command, s *store.Store, c codexCLI, name string, opts codex.LoginOptions, secret string) (store.Result, error) {
	login := func(stdio codex.Stdio) (store.Result, error) {
		return s.Login(name, func(home string) error { return c.Login(home, opts, stdio) })
	}
	if a.tui && secret == "" {
		fmt.Fprint(cmd.ErrOrStderr(), errTheme(cmd).Hint(fmt.Sprintf("Starting 'codex login' for profile %s…", name)))
		return login(commandStdio(cmd))
	}
	var result store.Result
	err := a.quietly(cmd, fmt.Sprintf("Logging in profile %s", name), secret+"\n", func(stdio codex.Stdio) error {
		var err error
		result, err = login(stdio)
		return err
	})
	return result, err
}

func (a *app) runLogout(cmd *cobra.Command, s *store.Store, c codexCLI, name string) (store.Result, error) {
	var result store.Result
	err := a.quietly(cmd, fmt.Sprintf("Logging out of %s", name), "", func(stdio codex.Stdio) error {
		var err error
		result, err = s.Logout(name, func(home string) error { return c.Logout(home, stdio) })
		return err
	})
	return result, err
}

func commandStdio(cmd *cobra.Command) codex.Stdio {
	return codex.Stdio{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
}

// On a terminal the process runs behind a spinner and its output is added to
// the error if it fails.
func (a *app) quietly(cmd *cobra.Command, title, input string, run func(stdio codex.Stdio) error) error {
	if !a.tui {
		return run(commandStdio(cmd))
	}
	var output bytes.Buffer
	err := tui.Spin(env(cmd), title, func() error {
		return run(codex.Stdio{In: strings.NewReader(input), Out: &output, Err: &output})
	})
	return withOutput(err, &output)
}

// After a cancel the step may still be running, so its output is left alone.
func withOutput(err error, output io.Reader) error {
	if err == nil || errors.Is(err, tui.ErrCancelled) {
		return err
	}
	text, _ := io.ReadAll(output)
	if trimmed := strings.TrimSpace(string(text)); trimmed != "" {
		return fmt.Errorf("%w\n%s", err, trimmed)
	}
	return err
}

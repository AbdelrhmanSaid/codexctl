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

// chooseLoginMethod asks how to log in and, for a key or token, reads it
// with a masked input so it never lands in shell history.
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

// runLogin runs codex login for a profile. A browser or device login keeps
// the terminal, since Codex prints a link or code to follow; a pasted
// secret is fed to Codex behind a spinner.
func (a *app) runLogin(cmd *cobra.Command, s *store.Store, c codexCLI, name string, opts codex.LoginOptions, secret string) (string, error) {
	stdio := codex.Stdio{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
	if !a.tui {
		return s.Login(name, func(home string) error { return c.Login(home, opts, stdio) })
	}
	if secret == "" {
		fmt.Fprint(cmd.ErrOrStderr(), errTheme(cmd).Hint(fmt.Sprintf("Starting 'codex login' for profile %s…", name)))
		return s.Login(name, func(home string) error { return c.Login(home, opts, stdio) })
	}
	var output bytes.Buffer
	stdio = codex.Stdio{In: strings.NewReader(secret + "\n"), Out: &output, Err: &output}
	var warning string
	err := tui.Spin(env(cmd), fmt.Sprintf("Logging in profile %s", name), func() error {
		var err error
		warning, err = s.Login(name, func(home string) error { return c.Login(home, opts, stdio) })
		return err
	})
	if err != nil {
		return "", withOutput(err, &output)
	}
	return warning, nil
}

// runLogout runs codex logout for a profile, behind a spinner on a
// terminal.
func (a *app) runLogout(cmd *cobra.Command, s *store.Store, c codexCLI, name string) (string, error) {
	if !a.tui {
		stdio := codex.Stdio{In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()}
		return s.Logout(name, func(home string) error { return c.Logout(home, stdio) })
	}
	var output bytes.Buffer
	stdio := codex.Stdio{In: strings.NewReader(""), Out: &output, Err: &output}
	var warning string
	err := tui.Spin(env(cmd), fmt.Sprintf("Logging out of %s", name), func() error {
		var err error
		warning, err = s.Logout(name, func(home string) error { return c.Logout(home, stdio) })
		return err
	})
	if err != nil {
		return "", withOutput(err, &output)
	}
	return warning, nil
}

// withOutput adds what Codex printed to an error, since it was hidden
// behind a spinner. After a cancel the step may still be running, so its
// output is left alone.
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

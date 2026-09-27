package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AbdelrhmanSaid/codexctl/internal/store"
	"github.com/AbdelrhmanSaid/codexctl/internal/tui"

	"github.com/spf13/cobra"
)

var errNoProfiles = errors.New("no profiles are saved yet; run 'codexctl login PROFILE_NAME' or 'codexctl import PROFILE_NAME' first")

func profileDetail(p store.Profile) string {
	if !p.Valid {
		return "unreadable snapshot"
	}
	parts := []string{}
	switch {
	case p.Email != "":
		parts = append(parts, p.Email)
	case p.AuthMode == "apikey":
		parts = append(parts, "API key")
	case p.AuthMode != "":
		parts = append(parts, p.AuthMode)
	}
	if p.Plan != "" {
		parts = append(parts, p.Plan)
	}
	return strings.Join(parts, " · ")
}

// Unreadable profiles are shown but not selectable unless allowInvalid.
func profileItems(profiles []store.Profile, allowInvalid bool) ([]tui.Item, int) {
	items := make([]tui.Item, len(profiles))
	cursor := 0
	for i, p := range profiles {
		items[i] = tui.Item{Label: p.Name, Detail: profileDetail(p)}
		if p.Selected {
			items[i].Badge = "active"
			cursor = i
		}
		if !p.Valid && !allowInvalid {
			items[i].Disabled = "unreadable snapshot"
		}
	}
	return items, cursor
}

func (a *app) profileArg(cmd *cobra.Command, s *store.Store, args []string, title string, allowInvalid bool) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	profiles, err := a.profilesToPick(cmd, s)
	if err != nil {
		return "", err
	}
	items, cursor := profileItems(profiles, allowInvalid)
	i, err := tui.Select(env(cmd), tui.SelectOptions{Title: title, Items: items, Cursor: cursor})
	if err != nil {
		return "", err
	}
	return profiles[i].Name, nil
}

func (a *app) profileArgs(cmd *cobra.Command, s *store.Store, args []string, title string) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}
	profiles, err := a.profilesToPick(cmd, s)
	if err != nil {
		return nil, err
	}
	items, _ := profileItems(profiles, true)
	chosen, err := tui.MultiSelect(env(cmd), tui.MultiSelectOptions{Title: title, Items: items, Min: 1})
	if err != nil {
		return nil, err
	}
	names := make([]string, len(chosen))
	for i, index := range chosen {
		names[i] = profiles[index].Name
	}
	return names, nil
}

func (a *app) profilesToPick(cmd *cobra.Command, s *store.Store) ([]store.Profile, error) {
	if !a.tui {
		return nil, fmt.Errorf("missing PROFILE_NAME; usage: %s", cmd.UseLine())
	}
	profiles, err := s.Profiles()
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, errNoProfiles
	}
	return profiles, nil
}

func (a *app) newNameArg(cmd *cobra.Command, s *store.Store, args []string, index int, argName string, opts tui.InputOptions, allowExisting bool) (string, error) {
	if len(args) > index {
		return args[index], nil
	}
	if !a.tui {
		return "", fmt.Errorf("missing %s; usage: %s", argName, cmd.UseLine())
	}
	names, _, err := s.List()
	if err != nil {
		return "", err
	}
	opts.Validate = func(name string) error {
		if err := store.ValidateName(name); err != nil {
			return err
		}
		for _, existing := range names {
			if existing == name && !allowExisting {
				return fmt.Errorf("profile %q already exists", name)
			}
		}
		return nil
	}
	return tui.Input(env(cmd), opts)
}

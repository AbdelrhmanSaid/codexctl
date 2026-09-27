package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrNotChatGPT = errors.New("not a ChatGPT login")

type ProfileHome struct {
	Path string
	// Isolated is a temporary copy; otherwise Path is the real Codex home.
	Isolated  bool
	AccountID string
}

const inspectWorkers = 4

// Inspect runs inspect in parallel with a Codex home holding each profile's
// login, and returns one error per name. The selected profile uses the real
// Codex home so it does not compete with running clients for its tokens; the
// others get isolated copies. A refresh retires the old refresh token, so
// refreshed copies are saved back, under the lock.
func (s *Store) Inspect(names []string, inspect func(name string, home ProfileHome) error) ([]error, Result, error) {
	op, err := s.begin()
	if err != nil {
		return nil, Result{}, err
	}
	defer op.release()

	s.removeAbandonedLogins()

	current, matches, _ := s.Current()
	errs := make([]error, len(names))
	warnings := make([]string, len(names))

	// Two copies of one login would both refresh it, and the second would
	// present a retired token.
	owners := map[string]string{}
	if matches {
		for _, path := range []string{s.AuthPath(), s.profilePath(current)} {
			if info, err := readAuth(path); err == nil && info.Tokens.RefreshToken != "" {
				owners[info.Tokens.RefreshToken] = current
			}
		}
	}

	usedActive := false
	var wg sync.WaitGroup
	slots := make(chan struct{}, inspectWorkers)

	for i, name := range names {
		data, err := s.loadProfile(name)
		if err != nil {
			errs[i] = err
			continue
		}

		info, _ := parseAuth(data)
		identity := info.identity()
		if identity.AuthMode == "apikey" {
			errs[i] = ErrNotChatGPT
			continue
		}

		home := ProfileHome{Path: s.CodexHome, AccountID: identity.AccountID}
		if name == current && matches {
			usedActive = true
		} else {
			token := info.Tokens.RefreshToken
			if owner, ok := owners[token]; ok && token != "" {
				errs[i] = fmt.Errorf("holds the same login as profile %q; skipped", owner)
				continue
			}

			owners[token] = name
			if home.Path, err = s.isolatedLogin(data); err != nil {
				errs[i] = err
				continue
			}

			home.Isolated = true
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()

			errs[i] = inspect(name, home)
			if home.Isolated {
				warnings[i] = s.saveRefreshed(name, data, home.Path)
				os.RemoveAll(home.Path)
			}
		}()
	}

	wg.Wait()

	for _, warning := range warnings {
		op.warn(warning)
	}

	if usedActive {
		op.warn(s.syncCurrentProfile())
	}

	return errs, op.result, nil
}

func (s *Store) saveRefreshed(name string, data []byte, home string) string {
	refreshed, err := readFile(filepath.Join(home, "auth.json"))
	if err != nil {
		return fmt.Sprintf("could not read back the credentials of profile %q; if Codex refreshed them, log in to it again", name)
	}

	if bytes.Equal(refreshed, data) {
		return ""
	}

	if match, err := compareAccounts(refreshed, data); err != nil || match != accountSame {
		return fmt.Sprintf("Codex left another account's credentials in the copy of profile %q; kept the saved profile", name)
	}

	if err := writeFile(s.profilePath(name), refreshed, 0o600); err != nil {
		return fmt.Sprintf("could not save refreshed credentials for profile %q: %v", name, err)
	}

	return ""
}

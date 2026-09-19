package store

import "path/filepath"

// Files owned by Codex.

func (s *Store) authPath() string   { return filepath.Join(s.CodexHome, "auth.json") }
func (s *Store) configPath() string { return filepath.Join(s.CodexHome, "config.toml") }

// Files owned by codexctl.

func (s *Store) currentPath() string { return filepath.Join(s.StateHome, "current") }
func (s *Store) lockPath() string    { return filepath.Join(s.StateHome, "lock") }
func (s *Store) profilesDir() string { return filepath.Join(s.StateHome, "profiles") }

func (s *Store) profilePath(name string) string {
	return filepath.Join(s.profilesDir(), name+".json")
}

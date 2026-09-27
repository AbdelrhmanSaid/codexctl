package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

type Result struct {
	// Profile is the new name for a rename.
	Profile  string
	Warnings []string
	// AuthChanged includes finishing an interrupted switch.
	AuthChanged bool
}

// Holds the store lock and collects what the change did.
type operation struct {
	*Store
	release func()
	result  Result
}

// Leaves an interrupted switch alone, for operations that must not touch the
// Codex home yet.
func (s *Store) open() (*operation, error) {
	release, err := s.lock()
	if err != nil {
		return nil, err
	}

	if err := s.ensureStateLayout(); err != nil {
		release()
		return nil, err
	}

	return &operation{Store: s, release: release}, nil
}

// open, then finish an interrupted switch.
func (s *Store) begin() (*operation, error) {
	op, err := s.open()
	if err != nil {
		return nil, err
	}

	if err := op.recoverPendingActivation(); err != nil {
		op.release()
		return nil, err
	}

	return op, nil
}

func (op *operation) warn(warning string) {
	if warning != "" {
		op.result.Warnings = append(op.result.Warnings, warning)
	}
}

func (op *operation) done(profile string) (Result, error) {
	op.result.Profile = profile

	return op.result, nil
}

func (op *operation) prepareCodexHome() error {
	if err := op.ensureCodexLayout(); err != nil {
		return err
	}

	if err := op.recoverPendingActivation(); err != nil {
		return err
	}

	return op.ensureFileCredentials()
}

// A marker written first lets the next operation finish an interrupted
// switch.
func (op *operation) writeActive(name string, data []byte) error {
	if err := writeFile(op.pendingPath(), []byte(name+"\n"), 0o600); err != nil {
		return fmt.Errorf("record pending activation: %w", err)
	}

	if err := op.finishActivation(name, data); err != nil {
		return fmt.Errorf("activation is incomplete and will be resumed by the next login or use: %w", err)
	}

	return op.clearPendingActivation()
}

func (op *operation) finishActivation(name string, data []byte) error {
	if err := writeFile(op.AuthPath(), data, 0o600); err != nil {
		return fmt.Errorf("activate profile: %w", err)
	}

	op.recordSwitch(name)
	if err := op.selectProfile(name); err != nil {
		return err
	}

	return nil
}

func (op *operation) clearPendingActivation() error {
	if err := refuseSymlink(op.pendingPath()); err != nil {
		return err
	}

	if err := os.Remove(op.pendingPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("activation completed but its recovery marker could not be removed: %w", err)
	}

	return nil
}

func (op *operation) recoverPendingActivation() error {
	data, err := readFile(op.pendingPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending activation: %w", err)
	}

	name := strings.TrimSpace(string(data))
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("pending activation marker is invalid: %w", err)
	}

	profileData, err := op.loadProfile(name)
	if err != nil {
		return fmt.Errorf("recover pending activation: %w", err)
	}

	if err := op.finishActivation(name, profileData); err != nil {
		return fmt.Errorf("recover pending activation: %w", err)
	}

	return op.clearPendingActivation()
}

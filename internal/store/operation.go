package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Result describes what an operation on the store did.
type Result struct {
	// Profile is the profile the operation saved, selected or removed; for
	// a rename it is the new name.
	Profile string
	// Warnings are problems that did not stop the operation.
	Warnings []string
	// AuthChanged is whether the active auth.json was changed, including by
	// finishing an interrupted switch. A running Codex app-server daemon
	// keeps the credentials it loaded before such a change.
	AuthChanged bool
}

// operation is a change to the store in progress. It holds the store lock
// and collects what the change did.
type operation struct {
	*Store
	// release gives up the lock. The caller must call it.
	release func()
	result  Result
}

// open takes the store lock and prepares the state directory. It leaves an
// interrupted switch alone, for operations that must not touch the Codex
// home until something else has succeeded.
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

// begin is open followed by finishing an interrupted switch.
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

// warn adds a warning to the result, if there is one.
func (op *operation) warn(warning string) {
	if warning != "" {
		op.result.Warnings = append(op.result.Warnings, warning)
	}
}

// done returns the result of an operation that succeeded on profile.
func (op *operation) done(profile string) (Result, error) {
	op.result.Profile = profile
	return op.result, nil
}

// prepareCodexHome readies the real Codex home for a new active auth.json:
// it finishes an interrupted switch and makes Codex keep credentials in
// files.
func (op *operation) prepareCodexHome() error {
	if err := op.ensureCodexLayout(); err != nil {
		return err
	}
	if err := op.recoverPendingActivation(); err != nil {
		return err
	}
	return op.ensureFileCredentials()
}

// writeActive makes data the active auth.json and name the selected
// profile. A marker written first lets the next operation finish the switch
// if this one is interrupted.
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

// recoverPendingActivation completes a switch interrupted after its durable
// marker was written. The caller holds the store lock, so the profile cannot
// be changed concurrently by another codexctl process.
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
	profile, err := op.loadProfile(name)
	if err != nil {
		return fmt.Errorf("recover pending activation: %w", err)
	}
	if err := op.finishActivation(name, profile); err != nil {
		return fmt.Errorf("recover pending activation: %w", err)
	}
	return op.clearPendingActivation()
}

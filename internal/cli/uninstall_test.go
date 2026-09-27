package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/AbdelrhmanSaid/codexctl/internal/update"
)

func (h *harness) installBinary(t *testing.T) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(h.exe), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(h.exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertExists(t *testing.T, path string, want bool) {
	t.Helper()

	_, err := os.Stat(path)
	if exists := !errors.Is(err, fs.ErrNotExist); exists != want {
		t.Fatalf("%s exists = %v, want %v", path, exists, want)
	}
}

func TestUninstallRemovesBinaryAndKeepsProfiles(t *testing.T) {
	h := newHarness(t)
	h.installBinary(t)
	h.seed(t, "work")

	stdout, _, err := h.run(t, "", "uninstall", "--yes")
	if err != nil {
		t.Fatal(err)
	}

	assertContains(t, stdout, "Removed "+h.exe)
	assertContains(t, stdout, "Kept saved profiles")
	assertExists(t, h.exe, false)
	assertExists(t, filepath.Join(h.stateHome, "profiles", "work.json"), true)
}

func TestUninstallPurgeRemovesState(t *testing.T) {
	h := newHarness(t)
	h.installBinary(t)
	h.seed(t, "work")
	authPath := filepath.Join(h.codexHome, "auth.json")

	_, stderr, err := h.run(t, "y\n", "uninstall", "--purge")
	if err != nil {
		t.Fatal(err)
	}

	assertContains(t, stderr, "Continue? [y/N]")
	assertExists(t, h.exe, false)
	assertExists(t, h.stateHome, false)
	assertExists(t, authPath, true)
}

func TestUninstallPurgeKeepBinary(t *testing.T) {
	h := newHarness(t)
	h.installBinary(t)
	h.method = update.MethodPackage
	h.seed(t, "work")

	if _, _, err := h.run(t, "", "uninstall", "--purge", "--keep-binary", "--yes"); err != nil {
		t.Fatal(err)
	}

	assertExists(t, h.exe, true)
	assertExists(t, h.stateHome, false)
}

func TestUninstallRemovesNothingUnlessConfirmed(t *testing.T) {
	h := newHarness(t)
	h.installBinary(t)
	h.seed(t, "work")

	_, stderr, err := h.run(t, "n\n", "uninstall", "--purge")
	if err != nil {
		t.Fatal(err)
	}

	assertContains(t, stderr, "Nothing was removed.")

	h.app.interactive = false
	if _, _, err := h.run(t, "y\n", "uninstall", "--purge"); err == nil {
		t.Fatal("non-interactive uninstall without --yes succeeded")
	}

	assertExists(t, h.exe, true)
	assertExists(t, filepath.Join(h.stateHome, "profiles", "work.json"), true)
}

func TestUninstallRefusals(t *testing.T) {
	h := newHarness(t)
	h.installBinary(t)
	h.seed(t, "work")

	if _, _, err := h.run(t, "", "uninstall", "--keep-binary", "--yes"); err == nil {
		t.Fatal("--keep-binary without --purge succeeded")
	}

	h.method = update.MethodPackage
	_, _, err := h.run(t, "", "uninstall", "--purge", "--yes")
	if err == nil {
		t.Fatal("uninstall of a package-managed binary succeeded")
	}

	assertContains(t, err.Error(), "--keep-binary")
	assertExists(t, h.exe, true)
	assertExists(t, filepath.Join(h.stateHome, "profiles", "work.json"), true)
}

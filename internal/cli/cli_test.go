package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpUsesCobraCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"--help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"login", "use", "list", "current", "doctor", "completion"} {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("help does not contain %q:\n%s", command, stdout.String())
		}
	}
}

func TestLoginAcceptsFlagsAfterProfile(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cmd := newLoginCommand()
	cmd.SetArgs([]string{"work", "--device-auth"})
	// Parsing reaches RunE; it fails only because this unit test does not put a
	// Codex executable on PATH.
	err := cmd.Execute()
	if err == nil || strings.Contains(err.Error(), "accepts 1 arg") || strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("flags after profile were not parsed correctly: %v", err)
	}
}

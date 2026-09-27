package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/AbdelrhmanSaid/codexctl/internal/tui"
)

func TestByteSize(t *testing.T) {
	for size, want := range map[int]string{512: "512 B", 2048: "2.0 KB", 4_404_019: "4.2 MB"} {
		if got := byteSize(size); got != want {
			t.Fatalf("byteSize(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestRelativeTime(t *testing.T) {
	if got := relativeTime("not a time"); got != "not a time" {
		t.Fatalf("unparsable stamp became %q", got)
	}

	stamp := time.Now().Add(-49 * time.Hour).UTC().Format(time.RFC3339Nano)
	if got := relativeTime(stamp); !strings.HasSuffix(got, "(2 days ago)") {
		t.Fatalf("relativeTime = %q", got)
	}

	for elapsed, want := range map[time.Duration]string{
		10 * time.Second: "just now",
		time.Minute:      "1 minute ago",
		3 * time.Hour:    "3 hours ago",
	} {
		if got := ago(elapsed); got != want {
			t.Fatalf("ago(%v) = %q, want %q", elapsed, got, want)
		}
	}
}

func TestMessageIsWrittenOnce(t *testing.T) {
	msg := say("Renamed profile %s to %s", profileName("work"), profileName("job"))
	if got, want := msg.plain(), `Renamed profile "work" to "job".`; got != want {
		t.Fatalf("plain = %q, want %q", got, want)
	}

	msg = say("Kept saved profiles in %s", filePath("/srv/state")).withHint("Delete that directory to remove them.")
	if got, want := msg.plain(), "Kept saved profiles in /srv/state. Delete that directory to remove them."; got != want {
		t.Fatalf("plain = %q, want %q", got, want)
	}

	if got, want := msg.styled(tui.NewTheme(&strings.Builder{})), "Kept saved profiles in /srv/state"; got != want {
		t.Fatalf("styled = %q, want %q", got, want)
	}
}

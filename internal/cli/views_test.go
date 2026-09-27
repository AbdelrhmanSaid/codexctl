package cli

import (
	"strings"
	"testing"
	"time"
)

func TestByteSize(t *testing.T) {
	for n, want := range map[int]string{512: "512 B", 2048: "2.0 KB", 4_404_019: "4.2 MB"} {
		if got := byteSize(n); got != want {
			t.Fatalf("byteSize(%d) = %q, want %q", n, got, want)
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
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now",
		time.Minute:      "1 minute ago",
		3 * time.Hour:    "3 hours ago",
	} {
		if got := ago(d); got != want {
			t.Fatalf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}

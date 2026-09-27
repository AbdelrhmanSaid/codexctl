package codex

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestReadUsage(t *testing.T) {
	stdout := strings.Join([]string{
		`{"id":1,"result":{"userAgent":"codex"}}`,
		`{"method":"account/updated","params":{"authMode":"chatgpt"}}`,
		`not json`,
		`{"id":2,"result":{"accountId":"acct","rateLimits":{"primary":{"usedPercent":15,"windowDurationMins":300,"resetsAt":1790551805},"secondary":null}}}`,
	}, "\n") + "\n"
	var stdin bytes.Buffer
	u, err := readUsage(&stdin, strings.NewReader(stdout))
	if err != nil {
		t.Fatal(err)
	}
	if u.AccountID != "acct" || u.Secondary != nil || u.Primary.UsedPercent != 15 || u.Primary.Minutes != 300 || !u.Primary.ResetsAt.Equal(time.Unix(1790551805, 0)) {
		t.Fatalf("usage = %+v, primary = %+v", u, u.Primary)
	}
	for _, method := range []string{`"initialize"`, `"initialized"`, `"account/rateLimits/read"`} {
		if !strings.Contains(stdin.String(), method) {
			t.Fatalf("requests %q do not include %s", stdin.String(), method)
		}
	}
}

func TestReadUsageErrors(t *testing.T) {
	for stdout, want := range map[string]string{
		`{"id":1,"result":{}}` + "\n" + `{"id":2,"error":{"message":"not logged in"}}` + "\n": "not logged in",
		`{"id":1,"result":{}}` + "\n": "exited without answering",
	} {
		_, err := readUsage(&bytes.Buffer{}, strings.NewReader(stdout))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want %q", err, want)
		}
	}
}

package store

import (
	"strings"
	"testing"
)

func TestCompareAccounts(t *testing.T) {
	a1 := chatgptAuth(t, "acct-a", "r1")
	a2 := chatgptAuth(t, "acct-a", "r2")
	b := chatgptAuth(t, "acct-b", "r1")
	key1 := apiKeyAuth(t, "sk-1")
	key2 := apiKeyAuth(t, "sk-2")
	tests := []struct {
		name          string
		active, saved []byte
		want          accountMatch
		wantErr       bool
	}{
		{"same account after refresh", a2, a1, accountSame, false},
		{"different accounts", a1, b, accountDifferent, false},
		{"identical files without account ID", key1, key1, accountSame, false},
		{"different files without account ID", key1, key2, accountUnverified, false},
		{"only one has an account ID", a1, key1, accountUnverified, false},
		{"invalid active file", []byte("{"), a1, accountUnverified, true},
		{"invalid saved file", a1, []byte(""), accountUnverified, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compareAccounts(tt.active, tt.saved)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("compareAccounts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAuthRejectsNonObjects(t *testing.T) {
	for _, data := range []string{"", "  ", "null", "[]", `"text"`, "42", "{"} {
		if _, err := parseAuth([]byte(data)); err == nil {
			t.Errorf("parseAuth(%q) accepted a non-object", data)
		}
	}
	if _, err := parseAuth([]byte("{}")); err != nil {
		t.Errorf("parseAuth({}) = %v", err)
	}
}

func TestIdentity(t *testing.T) {
	info, err := parseAuth(chatgptAuth(t, "acct-a", "r1"))
	if err != nil {
		t.Fatal(err)
	}
	id := info.identity()
	want := Identity{AuthMode: "chatgpt", AccountID: "acct-a", Email: "acct-a@example.com", Plan: "plus", LastRefresh: "2026-09-01T00:00:00Z"}
	if id != want {
		t.Fatalf("identity = %+v, want %+v", id, want)
	}

	info, err = parseAuth(apiKeyAuth(t, "sk-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if id := info.identity(); id != (Identity{AuthMode: "apikey"}) {
		t.Fatalf("identity = %+v, want only the auth mode", id)
	}
}

func TestIdentityFallsBackToTokenAccountID(t *testing.T) {
	data := marshalAuth(t, map[string]any{
		"tokens": map[string]any{"id_token": fakeIDToken(t, "x@example.com", "pro", "acct-jwt")},
	})
	info, err := parseAuth(data)
	if err != nil {
		t.Fatal(err)
	}
	if id := info.identity(); id.AccountID != "acct-jwt" || id.Plan != "pro" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestIdentityIgnoresMalformedToken(t *testing.T) {
	for _, token := range []string{"", "one.two", "a.!!!.c", "a." + "bm90IGpzb24" + ".c"} {
		data := marshalAuth(t, map[string]any{"tokens": map[string]any{"id_token": token, "account_id": "acct-a"}})
		info, err := parseAuth(data)
		if err != nil {
			t.Fatal(err)
		}
		if id := info.identity(); id.AccountID != "acct-a" || id.Email != "" {
			t.Fatalf("identity for token %q = %+v", token, id)
		}
	}
}

func TestProfilesNeverExposeSecrets(t *testing.T) {
	s := newTestStore(t)
	mustLogin(t, s, "a", chatgptAuth(t, "acct-a", "refresh-secret"))
	writeBytes(t, s.profilePath("key"), apiKeyAuth(t, "sk-secret"))
	writeBytes(t, s.profilePath("broken"), []byte("nope"))

	profiles, err := s.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 3 {
		t.Fatalf("got %d profiles, want 3", len(profiles))
	}
	for _, p := range profiles {
		dump := strings.Join([]string{p.Name, p.AuthMode, p.AccountID, p.Email, p.Plan, p.LastRefresh}, " ")
		if strings.Contains(dump, "secret") {
			t.Fatalf("profile %q exposes a secret: %s", p.Name, dump)
		}
		if p.Name == "broken" && p.Valid {
			t.Fatal("invalid profile reported as valid")
		}
		if p.Selected != (p.Name == "a") {
			t.Fatalf("profile %q selected = %v", p.Name, p.Selected)
		}
	}
}

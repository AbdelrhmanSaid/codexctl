package store

import (
	"strings"
	"testing"
)

func TestCompareAccounts(t *testing.T) {
	accountA := chatgptAuth(t, "acct-a", "r1")
	accountARefreshed := chatgptAuth(t, "acct-a", "r2")
	accountB := chatgptAuth(t, "acct-b", "r1")
	apiKey1 := apiKeyAuth(t, "sk-1")
	apiKey2 := apiKeyAuth(t, "sk-2")

	tests := []struct {
		name          string
		active, saved []byte
		want          accountMatch
		wantErr       bool
	}{
		{"same account after refresh", accountARefreshed, accountA, accountSame, false},
		{"different accounts", accountA, accountB, accountDifferent, false},
		{"identical files without account ID", apiKey1, apiKey1, accountSame, false},
		{"different files without account ID", apiKey1, apiKey2, accountUnverified, false},
		{"only one has an account ID", accountA, apiKey1, accountUnverified, false},
		{"invalid active file", []byte("{"), accountA, accountUnverified, true},
		{"invalid saved file", accountA, []byte(""), accountUnverified, true},
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

	identity := info.identity()
	want := Identity{AuthMode: "chatgpt", AccountID: "acct-a", Email: "acct-a@example.com", Plan: "plus", LastRefresh: "2026-09-01T00:00:00Z"}
	if identity != want {
		t.Fatalf("identity = %+v, want %+v", identity, want)
	}

	info, err = parseAuth(apiKeyAuth(t, "sk-secret"))
	if err != nil {
		t.Fatal(err)
	}

	if identity := info.identity(); identity != (Identity{AuthMode: "apikey"}) {
		t.Fatalf("identity = %+v, want only the auth mode", identity)
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

	if identity := info.identity(); identity.AccountID != "acct-jwt" || identity.Plan != "pro" {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestIdentityIgnoresMalformedToken(t *testing.T) {
	for _, token := range []string{"", "one.two", "a.!!!.c", "a." + "bm90IGpzb24" + ".c"} {
		data := marshalAuth(t, map[string]any{"tokens": map[string]any{"id_token": token, "account_id": "acct-a"}})
		info, err := parseAuth(data)
		if err != nil {
			t.Fatal(err)
		}

		if identity := info.identity(); identity.AccountID != "acct-a" || identity.Email != "" {
			t.Fatalf("identity for token %q = %+v", token, identity)
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

	for _, profile := range profiles {
		dump := strings.Join([]string{profile.Name, profile.AuthMode, profile.AccountID, profile.Email, profile.Plan, profile.LastRefresh}, " ")
		if strings.Contains(dump, "secret") {
			t.Fatalf("profile %q exposes a secret: %s", profile.Name, dump)
		}

		if profile.Name == "broken" && profile.Valid {
			t.Fatal("invalid profile reported as valid")
		}

		if profile.Selected != (profile.Name == "a") {
			t.Fatalf("profile %q selected = %v", profile.Name, profile.Selected)
		}
	}
}

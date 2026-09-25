package store

import "testing"

func TestRootCredentialStoreIsFile(t *testing.T) {
	tests := []struct {
		config string
		want   bool
	}{
		{`cli_auth_credentials_store = "file"`, true},
		{`cli_auth_credentials_store = 'file'`, true},
		{`  cli_auth_credentials_store="file"  # keep`, true},
		{`cli_auth_credentials_store = "keyring"`, false},
		{`cli_auth_credentials_store = "auto"`, false},
		{`# cli_auth_credentials_store = "file"`, false},
		{"", false},
		{"[profiles.work]\ncli_auth_credentials_store = \"file\"", false},
		{"model = \"x\"\ncli_auth_credentials_store = \"file\"\n[tui]\n", true},
	}
	for _, tt := range tests {
		if got := rootCredentialStoreIsFile([]byte(tt.config)); got != tt.want {
			t.Errorf("rootCredentialStoreIsFile(%q) = %v, want %v", tt.config, got, tt.want)
		}
	}
}

func TestSetRootCredentialStore(t *testing.T) {
	tests := []struct {
		name, config, want string
	}{
		{
			"replaces an existing root value",
			"model = \"x\"\ncli_auth_credentials_store = \"keyring\"\n[tui]\ntheme = \"dark\"\n",
			"model = \"x\"\ncli_auth_credentials_store = \"file\"\n[tui]\ntheme = \"dark\"\n",
		},
		{
			"adds the key before any table",
			"[profiles.work]\ncli_auth_credentials_store = \"keyring\"\n",
			"# Managed by codexctl so named auth.json profiles are effective.\ncli_auth_credentials_store = \"file\"\n[profiles.work]\ncli_auth_credentials_store = \"keyring\"\n",
		},
		{
			"creates an empty config",
			"",
			"# Managed by codexctl so named auth.json profiles are effective.\ncli_auth_credentials_store = \"file\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(setRootCredentialStore([]byte(tt.config)))
			if got != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			if !rootCredentialStoreIsFile([]byte(got)) {
				t.Fatal("result is not recognised as file-backed")
			}
		})
	}
}

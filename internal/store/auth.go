package store

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type authInfo struct {
	AuthMode    string `json:"auth_mode"`
	APIKey      string `json:"OPENAI_API_KEY"`
	LastRefresh string `json:"last_refresh"`
	Tokens      struct {
		IDToken   string `json:"id_token"`
		AccountID string `json:"account_id"`
	} `json:"tokens"`
}

// Identity is the non-secret description of a credential file. It never
// carries tokens or API keys, so it is safe to print.
type Identity struct {
	AuthMode    string `json:"auth_mode,omitempty"`
	AccountID   string `json:"account_id,omitempty"`
	Email       string `json:"email,omitempty"`
	Plan        string `json:"plan,omitempty"`
	LastRefresh string `json:"last_refresh,omitempty"`
}

func readAuth(path string) (authInfo, error) {
	data, err := readFile(path)
	if err != nil {
		return authInfo{}, err
	}
	return parseAuth(data)
}

func parseAuth(data []byte) (authInfo, error) {
	var info authInfo
	if len(bytes.TrimSpace(data)) == 0 || json.Unmarshal(data, &info) != nil {
		return info, errors.New("credential file is not valid JSON")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return info, errors.New("credential file must be a JSON object")
	}
	return info, nil
}

func (a authInfo) identity() Identity {
	id := Identity{AuthMode: a.AuthMode, AccountID: a.Tokens.AccountID, LastRefresh: a.LastRefresh}
	if id.AuthMode == "" {
		switch {
		case a.Tokens.IDToken != "" || a.Tokens.AccountID != "":
			id.AuthMode = "chatgpt"
		case a.APIKey != "":
			id.AuthMode = "apikey"
		}
	}
	claims := idTokenClaims(a.Tokens.IDToken)
	id.Email = claims.Email
	id.Plan = claims.Auth.PlanType
	if id.AccountID == "" {
		id.AccountID = claims.Auth.AccountID
	}
	return id
}

type idClaims struct {
	Email string `json:"email"`
	Auth  struct {
		AccountID string `json:"chatgpt_account_id"`
		PlanType  string `json:"chatgpt_plan_type"`
	} `json:"https://api.openai.com/auth"`
}

// idTokenClaims decodes the payload of a JWT without verifying it. The token
// comes from a local credential file, so it is trusted exactly as much as
// that file; the claims are only used for display.
func idTokenClaims(token string) idClaims {
	var claims idClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return claims
	}
	_ = json.Unmarshal(payload, &claims)
	return claims
}

// accountMatch is the result of comparing two credential files.
type accountMatch int

const (
	// accountUnverified means the files differ and at least one has no
	// account ID, so nothing proves they belong to the same account.
	accountUnverified accountMatch = iota
	accountSame
	accountDifferent
)

// compareAccounts is the single rule for deciding whether the active auth.json
// still belongs to a saved profile. Account IDs decide when both files have
// one; otherwise only byte-identical files count as the same account.
func compareAccounts(active, saved []byte) (accountMatch, error) {
	a, err := parseAuth(active)
	if err != nil {
		return accountUnverified, err
	}
	b, err := parseAuth(saved)
	if err != nil {
		return accountUnverified, err
	}
	if a.Tokens.AccountID != "" && b.Tokens.AccountID != "" {
		if a.Tokens.AccountID == b.Tokens.AccountID {
			return accountSame, nil
		}
		return accountDifferent, nil
	}
	if bytes.Equal(active, saved) {
		return accountSame, nil
	}
	return accountUnverified, nil
}

package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
)

type authInfo struct {
	Tokens struct {
		AccountID string `json:"account_id"`
	} `json:"tokens"`
}

func readAuth(path string) (authInfo, error) {
	if err := refuseSymlink(path); err != nil {
		return authInfo{}, err
	}
	data, err := os.ReadFile(path)
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

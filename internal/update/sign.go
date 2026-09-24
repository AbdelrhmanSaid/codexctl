package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// releasePublicKey verifies the signature on every release's checksums.txt.
// A binary trusts only the key it was built with, so rotating the key means
// publishing one release signed with the old key that ships the new one.
const releasePublicKey = "FWY509VI6moVjoFy29LLqGE2NhmG7/gwlxXWxt1VoN0="

// PublicKey returns the embedded release verification key.
func PublicKey() (ed25519.PublicKey, error) {
	return DecodePublicKey(releasePublicKey)
}

// GenerateKey creates a new signing key pair. Both halves are base64 strings:
// the public key is embedded in the binary and the private key is stored as a
// CI secret.
func GenerateKey() (public, private string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv.Seed()), nil
}

func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, errors.New("invalid release public key")
	}
	return ed25519.PublicKey(key), nil
}

func DecodePrivateKey(s string) (ed25519.PrivateKey, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("invalid signing key: expected a base64 32-byte Ed25519 seed")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Sign returns a detached signature for data as a single base64 line.
func Sign(key ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)) + "\n")
}

// Verify checks a detached signature produced by Sign.
func Verify(key ed25519.PublicKey, data, signature []byte) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("signature is malformed")
	}
	if !ed25519.Verify(key, data, sig) {
		return fmt.Errorf("signature does not match the release public key")
	}
	return nil
}

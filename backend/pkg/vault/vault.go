// Package vault is the generic credential store (P1-AGT-004 / D8.4).
//
// One row per (tenant, secret name) in `vault_secrets`; the value is sealed as
// an AES-256-GCM envelope (`enc:v1:<iv-b64>:<ct-b64>`) before it reaches D1.
//
// The construction is deliberately the same one worker/light/tokenVault.ts
// already uses for the GitHub token: SHA-256 of a domain-separated secret, a
// fresh 12-byte IV per seal, and additional authenticated data that binds the
// ciphertext to the row it belongs to. Here the AAD is `vault:<userId>:<name>`,
// so a row copied to another user (or renamed) fails to open instead of handing
// out a credential. One review therefore covers both planes, and a future Edge
// reader can open what this writes.
//
// What the store refuses by design:
//   - no key material → ErrNotConfigured (never a plaintext fallback);
//   - a name that is not a safe ref body (whitespace/control characters);
//   - resolving a ref that is not `vault:<name>`, or a ref the tenant does not
//     own (ErrNotFound — the same answer for unknown and foreign).
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// EnvelopePrefix marks a sealed value; the Edge vault writes the same marker.
const EnvelopePrefix = "enc:v1:"

// keyContext domain-separates the data key, exactly like tokenVault.ts.
const keyContext = "vibesdk.v2.d1-credential-vault.v1|"

// RefPrefix is the only accepted reference scheme (`vault:<name>`).
const RefPrefix = "vault:"

var (
	// ErrNotConfigured means no key material was provided. The store fails
	// closed: nothing is written and nothing is resolved.
	ErrNotConfigured = errors.New("vault: no key material (set VAULT_SECRET or JWT_SECRET)")
	// ErrNotFound covers both "no such secret" and "another tenant's secret".
	ErrNotFound = errors.New("vault: secret not found")
	// ErrInvalidName is a name that cannot be a ref body.
	ErrInvalidName = errors.New("vault: secret name must match [A-Za-z0-9][A-Za-z0-9._/-]{0,127}")
	// ErrInvalidRef is a reference that is not `vault:<name>`.
	ErrInvalidRef = errors.New("vault: ref must be 'vault:' followed by a valid name")
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

// Sealer seals and opens values for one process.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer derives the data key from the deployment secret. An empty secret is
// ErrNotConfigured — the caller must answer 503, never store plaintext.
func NewSealer(secret string) (*Sealer, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, ErrNotConfigured
	}
	sum := sha256.Sum256([]byte(keyContext + secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("vault: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: gcm: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

func aad(userID, name string) []byte {
	return []byte("vault:" + userID + ":" + name)
}

// Seal returns `enc:v1:<iv>:<ciphertext>` for one tenant's named secret.
func (s *Sealer) Seal(userID, name, value string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", ErrInvalidName
	}
	iv := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("vault: iv: %w", err)
	}
	sealed := s.aead.Seal(nil, iv, []byte(value), aad(userID, name))
	return EnvelopePrefix +
		base64.StdEncoding.EncodeToString(iv) + ":" +
		base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts an envelope for one tenant's named secret. Anything that does
// not verify (tampered, wrong tenant, wrong name, not an envelope) is an error,
// never a partial value.
func (s *Sealer) Open(userID, name, envelope string) (string, error) {
	if !strings.HasPrefix(envelope, EnvelopePrefix) {
		return "", fmt.Errorf("vault: value is not a %q envelope", EnvelopePrefix)
	}
	body := strings.TrimPrefix(envelope, EnvelopePrefix)
	sep := strings.Index(body, ":")
	if sep <= 0 {
		return "", errors.New("vault: malformed envelope")
	}
	iv, err := base64.StdEncoding.DecodeString(body[:sep])
	if err != nil {
		return "", fmt.Errorf("vault: malformed envelope iv: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(body[sep+1:])
	if err != nil {
		return "", fmt.Errorf("vault: malformed envelope body: %w", err)
	}
	if len(iv) != s.aead.NonceSize() {
		return "", errors.New("vault: malformed envelope nonce")
	}
	plaintext, err := s.aead.Open(nil, iv, ciphertext, aad(userID, name))
	if err != nil {
		return "", errors.New("vault: envelope does not verify for this tenant and name")
	}
	return string(plaintext), nil
}

// ParseRef validates a `vault:<name>` reference and returns the name.
func ParseRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, RefPrefix) {
		return "", ErrInvalidRef
	}
	name := strings.TrimPrefix(ref, RefPrefix)
	if !nameRe.MatchString(name) {
		return "", ErrInvalidRef
	}
	return name, nil
}

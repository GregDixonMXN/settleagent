package keys

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

// Provider abstracts receipt signing today and envelope encryption tomorrow
// (M10). Local dev uses an env/file key; production plugs a KMS-compatible
// backend behind this interface. Never invent a custom cryptosystem here.
type Provider interface {
	KeyID() string
	Sign(message []byte) []byte
	Verify(message, signature []byte) bool
}

type localProvider struct {
	id  string
	key ed25519.PrivateKey
}

func (p *localProvider) KeyID() string { return p.id }

func (p *localProvider) Sign(message []byte) []byte {
	return ed25519.Sign(p.key, message)
}

func (p *localProvider) Verify(message, signature []byte) bool {
	return ed25519.Verify(p.key.Public().(ed25519.PublicKey), message, signature)
}

// Load returns the signing provider. AG_SIGNING_KEY holds a 64-byte ed25519
// seed+key hex (or 32-byte seed hex); unset means an ephemeral key with a
// loud log line — signatures from this boot verify only against this boot.
func Load() (Provider, bool, error) {
	raw := os.Getenv("AG_SIGNING_KEY")
	if raw == "" {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, false, err
		}
		fmt.Println("keys: no AG_SIGNING_KEY set — ephemeral signing key (dev only)")
		return &localProvider{id: "ephemeral-dev", key: key}, false, nil
	}
	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, false, fmt.Errorf("AG_SIGNING_KEY must be hex: %w", err)
	}
	var key ed25519.PrivateKey
	switch len(b) {
	case ed25519.SeedSize:
		key = ed25519.NewKeyFromSeed(b)
	case ed25519.PrivateKeySize:
		key = ed25519.PrivateKey(b)
	default:
		return nil, false, fmt.Errorf("AG_SIGNING_KEY must be %d or %d bytes hex", ed25519.SeedSize, ed25519.PrivateKeySize)
	}
	pub := key.Public().(ed25519.PublicKey)
	return &localProvider{id: "local-" + hex.EncodeToString(pub[:8]), key: key}, true, nil
}

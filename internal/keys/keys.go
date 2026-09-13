package keys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// Provider abstracts receipt signing today and envelope encryption tomorrow
// (M10). Local dev uses an env/file key; production plugs a KMS-compatible
// backend behind this interface. Never invent a custom cryptosystem here.
type Provider interface {
	KeyID() string
	Sign(message []byte) []byte
	Verify(message, signature []byte) bool
	// Seal encrypts third-party credentials for storage (AES-256-GCM,
	// "v1:" + base64(nonce|ciphertext)). Plaintext legacy values (no
	// prefix) decrypt as-is so migration 009/010 never locks data out.
	Seal(plaintext string) (string, error)
	Open(sealed string) (string, error)
}

type localProvider struct {
	id      string
	key     ed25519.PrivateKey
	dataKey []byte
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
// Seal/Open implement envelope encryption for stored third-party secrets.
func (p *localProvider) Seal(plaintext string) (string, error) {
	block, err := aes.NewCipher(p.dataKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), []byte(p.id))
	return "v1:" + base64.StdEncoding.EncodeToString(ct), nil
}

func (p *localProvider) Open(sealed string) (string, error) {
	if !strings.HasPrefix(sealed, "v1:") {
		return sealed, nil // plaintext legacy value
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, "v1:"))
	if err != nil {
		return "", fmt.Errorf("malformed sealed value: %w", err)
	}
	block, err := aes.NewCipher(p.dataKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("sealed value too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, []byte(p.id))
	if err != nil {
		return "", fmt.Errorf("decrypt failed (wrong key?): %w", err)
	}
	return string(pt), nil
}

// dataKeyFor derives the encryption key: explicit AG_DATA_KEY (32 bytes
// hex) wins; otherwise SHA-256 over the signing public key (persists iff
// the signing key persists). Ephemeral signing key ⇒ ephemeral data key,
// with the same loud warning.
func dataKeyFor(key ed25519.PrivateKey) ([]byte, bool) {
	if raw := os.Getenv("AG_DATA_KEY"); raw != "" {
		if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
			return b, true
		}
		fmt.Println("keys: AG_DATA_KEY must be 32 bytes hex — ignoring")
	}
	sum := sha256.Sum256(key.Public().(ed25519.PublicKey))
	return sum[:], false
}

func Load() (Provider, bool, error) {
	raw := os.Getenv("AG_SIGNING_KEY")
	if raw == "" {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, false, err
		}
		fmt.Println("keys: no AG_SIGNING_KEY set — ephemeral signing key (dev only)")
		dk, _ := dataKeyFor(key)
		fmt.Println("keys: ephemeral data key — stored third-party credentials will not survive restart (dev only)")
		return &localProvider{id: "ephemeral-dev", key: key, dataKey: dk}, false, nil
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
	dk, explicit := dataKeyFor(key)
	if explicit {
		fmt.Println("keys: explicit AG_DATA_KEY in use for stored credentials")
	}
	return &localProvider{id: "local-" + hex.EncodeToString(pub[:8]), key: key, dataKey: dk}, true, nil
}

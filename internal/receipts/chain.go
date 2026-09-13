package receipts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func Canonical(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ChainHash binds prev receipt hash + canonical payload.
func ChainHash(prevHash string, payload any) string {
	b, _ := json.Marshal(payload)
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

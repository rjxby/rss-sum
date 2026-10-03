package hasher

import (
	"crypto/sha256"
	"encoding/hex"
)

type HasherProc struct {
}

func New() *HasherProc {
	return &HasherProc{}
}

func (p HasherProc) HashString(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	hashBytes := h.Sum(nil)
	return hex.EncodeToString(hashBytes)
}

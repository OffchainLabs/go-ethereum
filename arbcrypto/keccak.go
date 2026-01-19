//go:build !wasm

package arbcrypto

import (
	"hash"

	"golang.org/x/crypto/sha3"
)

func NewLegacyKeccak256() hash.Hash {
	return sha3.NewLegacyKeccak256()
}

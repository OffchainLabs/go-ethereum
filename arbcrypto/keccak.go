//go:build !wasm

package arbcrypto

import (
	"hash"

	"github.com/ethereum/go-ethereum/crypto/keccak"
)

func NewLegacyKeccak256() hash.Hash {
	return keccak.NewLegacyKeccak256()
}

//go:build !ziren && !wasm

package crypto

import "golang.org/x/crypto/sha3"

func NewLegacyKeccak256() KeccakState {
	return sha3.NewLegacyKeccak256().(KeccakState)
}

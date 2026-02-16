//go:build (nacl || wasip1 || !cgo || gofuzz || tinygo) && !wasm

package arbcrypto

import (
	"errors"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// signatureLength indicates the byte length required to carry a signature with recovery id.
const signatureLength = 64 + 1 // 64 bytes ECDSA signature + 1 byte recovery id

// recoveryIDOffset points to the byte offset within the signature that contains the recovery id.
const recoveryIDOffset = 64

func SigToPub(hash, sig []byte) (*secp256k1.PublicKey, error) {
	if len(sig) != signatureLength {
		return nil, errors.New("invalid signature")
	}
	// Convert to secp256k1 input format with 'recovery id' v at the beginning.
	btcsig := make([]byte, signatureLength)
	btcsig[0] = sig[recoveryIDOffset] + 27
	copy(btcsig[1:], sig)

	pub, _, err := ecdsa.RecoverCompact(btcsig, hash)
	return pub, err
}

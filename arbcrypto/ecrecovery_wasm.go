//go:build (nacl || wasip1 || !cgo || gofuzz || tinygo) && wasm

package arbcrypto

import (
	"errors"
	"unsafe"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func SigToPub(hash, sig []byte) (*secp256k1.PublicKey, error) {
	if len(hash) == 0 {
		return nil, errors.New("hash is empty")
	} else if len(sig) == 0 {
		return nil, errors.New("signature is empty")
	}

	pubkeyBytes := make([]byte, 65)
	switch outsourcedECRecovery(unsafe.Pointer(&hash[0]), unsafe.Pointer(&sig[0]), unsafe.Pointer(&pubkeyBytes[0])) {
	case 0:
		return secp256k1.ParsePubKey(pubkeyBytes)
	default:
		return nil, errors.New("ecrecovery failed")
	}
}

//go:wasmimport arbcrypto ecrecovery
func outsourcedECRecovery(hash, sig, pub unsafe.Pointer) uint32

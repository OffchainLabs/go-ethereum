//go:build (nacl || wasip1 || !cgo || gofuzz || tinygo) && wasm

package arbcrypto

import (
	"errors"
	"unsafe"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func SigToPub(hash, sig []byte) (*secp256k1.PublicKey, error) {
	pubkeyBytes := make([]byte, 65)
	switch outsourcedECRecovery(sliceToPointer(hash), uint32(len(hash)), sliceToPointer(sig), uint32(len(sig)), sliceToPointer(pubkeyBytes)) {
	case 0:
		return secp256k1.ParsePubKey(pubkeyBytes)
	default:
		return nil, errors.New("ecrecovery failed")
	}
}

func sliceToPointer(slice []byte) unsafe.Pointer {
	if len(slice) == 0 {
		return unsafe.Pointer(nil)
	}
	return unsafe.Pointer(&slice[0])
}

//go:wasmimport arbcrypto ecrecovery
func outsourcedECRecovery(hash unsafe.Pointer, hashLen uint32, sig unsafe.Pointer, sigLen uint32, pub unsafe.Pointer) uint32

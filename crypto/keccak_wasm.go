// Copyright 2025, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

//go:build wasm && !ziren

package crypto

import (
	"unsafe"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/sha3"
)

// NewKeccakState creates a new KeccakState
func NewKeccakState() KeccakState {
	return sha3.NewLegacyKeccak256().(KeccakState)
}

// Keccak256 calculates and returns the Keccak256 hash of the input data.
func Keccak256(data ...[]byte) []byte {
	b := make([]byte, 32)
	keccak256Digest(b, data...)
	return b
}

// Keccak256Hash calculates and returns the Keccak256 hash of the input data,
// converting it to an internal Hash data structure.
func Keccak256Hash(data ...[]byte) (h common.Hash) {
	keccak256Digest(h[:], data...)
	return h
}

func keccak256Digest(out []byte, data ...[]byte) {
	if len(out) != 32 {
		panic("output buffer must be 32 bytes")
	}

	flattenedInput := make([]byte, 0)
	for _, b := range data {
		flattenedInput = append(flattenedInput, b...)
	}

	var inputPtr unsafe.Pointer
	if len(flattenedInput) > 0 {
		inputPtr = unsafe.Pointer(&flattenedInput[0])
	} else {
		inputPtr = unsafe.Pointer(nil)
	}

	outsourcedKeccak(inputPtr, uint32(len(flattenedInput)), unsafe.Pointer(&out[0]))
}

//go:wasmimport arbkeccak keccak256
func outsourcedKeccak(inBuf unsafe.Pointer, inLen uint32, outBuf unsafe.Pointer)

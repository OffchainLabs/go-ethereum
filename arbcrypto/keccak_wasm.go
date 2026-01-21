//go:build wasm

package arbcrypto

import (
	"hash"
	"io"
	"unsafe"
)

func NewLegacyKeccak256() hash.Hash {
	return &simpleHashBuffer{}
}

type simpleHashBuffer struct {
	buffer []byte
}

func (s *simpleHashBuffer) Write(p []byte) (n int, err error) {
	s.buffer = append(s.buffer, p...)
	return len(p), nil
}

func (s *simpleHashBuffer) Sum(b []byte) []byte {
	currentBufferHash := make([]byte, 32)
	s.Read(currentBufferHash)
	return append(b, currentBufferHash...)
}

func (s *simpleHashBuffer) Reset() {
	s.buffer = nil // Simply forget previous data.
}

func (s *simpleHashBuffer) Size() int {
	return 32 // Keccak256 produces 32-byte hashes
}

func (s *simpleHashBuffer) BlockSize() int {
	return (1600 - 512) / 8 // Keccak256 rate in bytes: sponge size 1600 bits - capacity 512 bits. Copied from sha3.
}

func (s *simpleHashBuffer) Read(bytes []byte) (int, error) {
	if len(bytes) < 32 {
		return 0, io.ErrShortBuffer
	}

	inputLen := len(s.buffer)
	inputPtr := unsafe.Pointer(nil)
	if inputLen > 0 {
		inputPtr = unsafe.Pointer(&s.buffer[0])
	}

	outsourcedKeccak(inputPtr, uint32(inputLen), unsafe.Pointer(&bytes[0]))
	return 32, nil
}

//go:wasmimport arbkeccak keccak256
func outsourcedKeccak(inBuf unsafe.Pointer, inLen uint32, outBuf unsafe.Pointer)

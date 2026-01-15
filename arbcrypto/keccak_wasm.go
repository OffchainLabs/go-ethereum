//go:build wasm

package arbcrypto

import (
	"hash"
	"io"
	"sync"

	"golang.org/x/crypto/sha3"
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

var realHasherPool = sync.Pool{
	New: func() any {
		return sha3.NewLegacyKeccak256()
	},
}

func (s *simpleHashBuffer) Read(bytes []byte) (int, error) {
	d := realHasherPool.Get().(hash.Hash)
	defer realHasherPool.Put(d)

	d.Reset()
	d.Write(s.buffer)
	return d.(io.Reader).Read(bytes)
}

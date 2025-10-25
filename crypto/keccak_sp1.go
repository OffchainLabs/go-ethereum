// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.
//
//go:build sp1

package crypto

import (
	"unsafe"

	"github.com/ethereum/go-ethereum/common"
)

// Keccak implementation using SP1 precompile

//go:wasmimport sp1 keccak256
func doKeccak256(data unsafe.Pointer, dataLength uint32, output unsafe.Pointer)

const (
	rateK512  = (1600 - 512) / 8
	rateK1024 = (1600 - 1024) / 8
)

type precompileKeccak256State struct {
	d []byte
}

func (d *precompileKeccak256State) BlockSize() int { return rateK512 }

func (d *precompileKeccak256State) Size() int { return 32 }

func (d *precompileKeccak256State) Reset() {
	d.d = nil
}

func (d *precompileKeccak256State) Read(out []byte) (n int, err error) {
	output := make([]byte, 32)
	input := unsafe.Pointer(uintptr(0))
	if (len(d.d) > 0) {
		input = unsafe.Pointer(&d.d[0])
	}
	doKeccak256(input, uint32(len(d.d)), unsafe.Pointer(&output[0]))
	n = copy(out, output)
	return
}

func (d *precompileKeccak256State) Sum(in []byte) []byte {
	hash := make([]byte, 32)
	d.Read(hash)
	return append(in, hash...)
}

// NOTE: different from a native go implementation, here our precompile-based
// hasher simply gathers the preimage data to a slice in the `Write` function.
// The actual hashing work is done inside `Read` function, where all the gathered
// input data are passed to the precompile function all together.
// This might not be an optimal solution when the preimage data can be quite
// big. But it provides a good tradeoff, since we don't need to keep Rust data
// structure inside a go struct across FFI boundaries. If this implementation
// really turns out to be a problem, we could revisit this design again.
func (d *precompileKeccak256State) Write(p []byte) (n int, err error) {
	n = len(p)
	d.d = append(d.d, p...)
	return 
}

func (d *precompileKeccak256State) clone() *precompileKeccak256State {
	ret := *d
	return &ret
}

func NewKeccakState() KeccakState {
	return &precompileKeccak256State{ d: nil }
}

func Keccak256(data ...[]byte) []byte {
	b := make([]byte, 32)
	d := NewKeccakState()
	d.Reset()
	for _, b := range data {
		d.Write(b)
	}
	d.Read(b)
	return b
}

func Keccak256Hash(data ...[]byte) (h common.Hash) {
	d := NewKeccakState()
	d.Reset()
	for _, b := range data {
		d.Write(b)
	}
	d.Read(h[:])
	return h
}

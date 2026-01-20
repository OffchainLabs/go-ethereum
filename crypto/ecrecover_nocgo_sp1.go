// Copyright 2017 The go-ethereum Authors
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

//go:build wasm && sp1

package crypto

import (
	"fmt"
	"unsafe"
)

// Ecrecover implementation using SP1 precompile

//go:wasmimport sp1 ecrecover
func innerEcrecover(hash, sig, output unsafe.Pointer) uint32

// Ecrecover returns the uncompressed public key that created the given signature.
func Ecrecover(hash, sig []byte) ([]byte, error) {
	pub := make([]byte, 65)
	ret := innerEcrecover(unsafe.Pointer(&hash[0]), unsafe.Pointer(&sig[0]), unsafe.Pointer(&pub[0]))
	if ret != 0 {
		return nil, fmt.Errorf("recovery failed with code: %d", ret)
	}
	return pub, nil
}

// Copyright 2015 The go-ethereum Authors
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

package light

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
)

func (db *odrDatabase) ActivateWasm(moduleHash common.Hash, asm, module []byte) error {
	return errors.New("setting compiled wasm not supported in light client")
}

func (db *odrDatabase) ActivatedAsm(moduleHash common.Hash) ([]byte, error) {
	return nil, errors.New("retreiving compiled wasm not supported in light client")
}

func (db *odrDatabase) ActivatedModule(moduleHash common.Hash) ([]byte, error) {
	return nil, errors.New("retreiving compiled wasm not supported in light client")
}

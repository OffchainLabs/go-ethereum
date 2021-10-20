// Copyright 2021 The go-ethereum Authors
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

// Package core implements the Ethereum consensus protocol.
package core

import "github.com/ethereum/go-ethereum/core/types"

func (bc *BlockChain) Reorg(newBlock *types.Block) error {
	bc.wg.Add(1)
	bc.chainmu.Lock()
	defer bc.wg.Done()
	defer bc.chainmu.Unlock()
	err := bc.reorg(bc.CurrentBlock(), newBlock)
	if err != nil {
		return err
	}
	bc.writeHeadBlockImpl(newBlock, true)
	return nil
}

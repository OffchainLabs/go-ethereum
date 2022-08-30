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

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

func (bc *BlockChain) ReorgToOldBlock(newHead *types.Block) error {
	bc.wg.Add(1)
	defer bc.wg.Done()
	bc.chainmu.MustLock()
	defer bc.chainmu.Unlock()
	oldHead := bc.CurrentBlock()
	if oldHead.Hash() == newHead.Hash() {
		return nil
	}
	bc.writeHeadBlock(newHead)
	err := bc.reorg(oldHead, newHead)
	if err != nil {
		return err
	}
	bc.chainHeadFeed.Send(ChainHeadEvent{Block: newHead})
	return nil
}

func (bc *BlockChain) ClipToPostNitroGenesis(blockNum rpc.BlockNumber) (rpc.BlockNumber, rpc.BlockNumber) {
	currentBlock := rpc.BlockNumber(bc.CurrentBlock().NumberU64())
	nitroGenesis := rpc.BlockNumber(bc.Config().ArbitrumChainParams.GenesisBlockNum)
	if blockNum == rpc.LatestBlockNumber || blockNum == rpc.PendingBlockNumber {
		blockNum = currentBlock
	}
	if blockNum > currentBlock {
		blockNum = currentBlock
	}
	if blockNum < nitroGenesis {
		blockNum = nitroGenesis
	}
	return blockNum, currentBlock
}

// finds the number of blocks that aren't prunable
func (bc *BlockChain) FindRetentionBound() uint64 {
	minimumSpan := bc.cacheConfig.TriesInMemory
	minimumAge := uint64(bc.cacheConfig.TrieRetention.Seconds())

	saturatingCast := func(value int64) uint64 {
		if value < 0 {
			return 0
		}
		return uint64(value)
	}

	// enforce that the block be sufficiently deep
	current := bc.CurrentBlock()
	heightBound := saturatingCast(int64(current.NumberU64()) - int64(minimumSpan) + 1)

	// find the left bound to our subsequent binary search
	timeBound := heightBound
	leap := int64(1)
	for timeBound > 0 {
		age := current.Time() - bc.GetBlockByNumber(uint64(timeBound)).Time()
		if age > minimumAge {
			break
		}
		timeBound = saturatingCast(int64(timeBound) - leap)
		leap *= 2
	}
	if timeBound == heightBound {
		return current.NumberU64() - timeBound + 1
	}

	// Algo: binary search on the interval [a, b] for the first prunable block
	//   Timebound is a prunable block, if one exists.
	//   We want to find the first block that's not prunable.
	//
	a := timeBound   // a prunable block, if possible
	b := heightBound // not prunable
	for a+2 < b {
		mid := a/2 + b/2 // a < mid < b
		age := current.Time() - bc.GetBlockByNumber(mid).Time()
		if age <= minimumAge {
			b = mid // mid is not prunable and less than b
		} else {
			a = mid // mid is prunable, but might equal a
		}
	}
	return current.NumberU64() - a
}

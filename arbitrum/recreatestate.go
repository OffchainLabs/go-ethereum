package arbitrum

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/pkg/errors"
)

var (
	ErrDepthLimitExceeded = errors.New("state recreation l2 gas depth limit exceeded")
)

type StateBuildingLogFunction func(targetHeader, header *types.Header, hasState bool)
type StateForHeaderFunction func(header *types.Header) (*state.StateDB, error)

func FindLastAvailableState(ctx context.Context, bc *core.BlockChain, stateFor StateForHeaderFunction, header *types.Header, logFunc StateBuildingLogFunction, maxDepthInL2Gas uint64) (*state.StateDB, *types.Header, error) {
	genesis := bc.Config().ArbitrumChainParams.GenesisBlockNum
	currentHeader := header
	var stateDb *state.StateDB
	var err error
	var l2GasUsed uint64
	for ctx.Err() == nil {
		lastHeader := currentHeader
		stateDb, err = stateFor(currentHeader)
		if err == nil {
			break
		}
		if maxDepthInL2Gas > 0 {
			receipts := bc.GetReceiptsByHash(currentHeader.Hash())
			if receipts == nil {
				return nil, lastHeader, fmt.Errorf("failed to get receipts for hash %v", currentHeader.Hash())
			}
			for _, receipt := range receipts {
				l2GasUsed += receipt.GasUsed - receipt.GasUsedForL1
			}
			if l2GasUsed > maxDepthInL2Gas {
				return nil, lastHeader, ErrDepthLimitExceeded
			}
		}
		if logFunc != nil {
			logFunc(header, currentHeader, false)
		}
		if currentHeader.Number.Uint64() <= genesis {
			return nil, lastHeader, errors.Wrap(err, fmt.Sprintf("moved beyond genesis looking for state %d, genesis %d", header.Number.Uint64(), genesis))
		}
		currentHeader = bc.GetHeader(currentHeader.ParentHash, currentHeader.Number.Uint64()-1)
		if currentHeader == nil {
			return nil, lastHeader, fmt.Errorf("chain doesn't contain parent of block %d hash %v", lastHeader.Number, lastHeader.Hash())
		}
	}
	return stateDb, currentHeader, ctx.Err()
}

func RecreateBlock(ctx context.Context, bc *core.BlockChain, header *types.Header, stateDb *state.StateDB, blockToRecreate uint64, prevBlockHash common.Hash, logFunc StateBuildingLogFunction) (*types.Block, error) {
	block := bc.GetBlockByNumber(blockToRecreate)
	if block == nil {
		return nil, fmt.Errorf("block not found while recreating: %d", blockToRecreate)
	}
	if block.ParentHash() != prevBlockHash {
		return nil, fmt.Errorf("reorg detected: number %d expectedPrev: %v foundPrev: %v", blockToRecreate, prevBlockHash, block.ParentHash())
	}
	if logFunc != nil {
		logFunc(header, block.Header(), true)
	}
	_, _, _, err := bc.Processor().Process(block, stateDb, vm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed recreating state for block %d : %w", blockToRecreate, err)
	}
	return block, nil
}

func RecreateBlocks(ctx context.Context, bc *core.BlockChain, header *types.Header, lastAvailableHeader *types.Header, stateDb *state.StateDB, logFunc StateBuildingLogFunction) (*state.StateDB, error) {
	returnedBlockNumber := header.Number.Uint64()
	blockToRecreate := lastAvailableHeader.Number.Uint64() + 1
	prevHash := lastAvailableHeader.Hash()
	for ctx.Err() == nil {
		block, err := RecreateBlock(ctx, bc, header, stateDb, blockToRecreate, prevHash, logFunc)
		if err != nil {
			return nil, err
		}
		prevHash = block.Hash()
		if blockToRecreate >= returnedBlockNumber {
			if block.Hash() != header.Hash() {
				return nil, fmt.Errorf("blockHash doesn't match when recreating number: %d expected: %v got: %v", blockToRecreate, header.Hash(), block.Hash())
			}
			return stateDb, nil
		}
		blockToRecreate++
	}
	return nil, ctx.Err()
}

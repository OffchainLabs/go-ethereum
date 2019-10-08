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

package core

import (
	"runtime/debug"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/deepmind"
	"github.com/ethereum/go-ethereum/params"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *StateProcessor {
	return &StateProcessor{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Process processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receipts types.Receipts
		usedGas  = new(uint64)
		header   = block.Header()
		allLogs  []*types.Log
		gp       = new(GasPool).AddGas(block.GasLimit())
	)

	if deepmind.Enabled {
		deepmind.EnterBlock()
		deepmind.Print("BEGIN_BLOCK", deepmind.Uint64(block.NumberU64()))
	}

	// Mutate the block and state according to any hard-fork specs
	if p.config.DAOForkSupport && p.config.DAOForkBlock != nil && p.config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(statedb)
	}

	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		statedb.Prepare(tx.Hash(), block.Hash(), i)

		if deepmind.Enabled {
			v, r, s := tx.RawSignatureValues()

			// We start assuming the "null" value (i.e. a dot character), and update if `to` is set
			toAsString := "."
			if tx.To() != nil {
				toAsString = deepmind.Addr(*tx.To())
			}

			deepmind.EnterTransaction()
			deepmind.Print("BEGIN_APPLY_TRX",
				deepmind.Hash(tx.Hash()),
				toAsString,
				deepmind.Hex(tx.Value().Bytes()),
				deepmind.Hex(v.Bytes()),
				deepmind.Hex(r.Bytes()),
				deepmind.Hex(s.Bytes()),
				deepmind.Uint64(tx.Gas()),
				deepmind.Hex(tx.GasPrice().Bytes()),
				deepmind.Uint64(tx.Nonce()),
				deepmind.Hex(tx.Data()),
			)
		}

		receipt, err := ApplyTransaction(p.config, p.bc, nil, gp, statedb, header, tx, usedGas, cfg)
		if err != nil {
			if deepmind.Enabled {
				deepmind.Print("FAILED_APPLY_TRX", err.Error())
				deepmind.EndTransaction()
				deepmind.EndBlock()
			}

			return nil, nil, 0, err
		}

		if deepmind.Enabled {
			type Log = map[string]interface{}

			logs := make([]Log, len(receipt.Logs))
			for i, log := range receipt.Logs {
				logs[i] = Log{
					"address": log.Address,
					"topics":  log.Topics,
					"data":    hexutil.Bytes(log.Data),
				}
			}

			deepmind.EndTransaction()
			deepmind.Print("END_APPLY_TRX", deepmind.Uint64(*usedGas), deepmind.Hex(receipt.PostState), deepmind.Uint64(receipt.CumulativeGasUsed), deepmind.Hex(receipt.Bloom[:]), deepmind.JSON(logs))
		}

		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}

	if deepmind.Enabled || deepmind.BlockProgressEnabled {
		deepmind.Print("FINALIZE_BLOCK", deepmind.Uint64(block.NumberU64()))
	}

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.engine.Finalize(p.bc, header, statedb, block.Transactions(), block.Uncles())

	if deepmind.Enabled {
		deepmind.EndBlock()
		deepmind.Print("END_BLOCK",
			deepmind.Uint64(block.NumberU64()),
			deepmind.Uint64(uint64(block.Size())),
			deepmind.JSON(map[string]interface{}{
				"header": block.Header(),
				"uncles": block.Body().Uncles,
			}),
		)
	}

	return receipts, allLogs, *usedGas, nil
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config) (*types.Receipt, error) {
	msg, err := tx.AsMessage(types.MakeSigner(config, header.Number))
	if err != nil {
		return nil, err
	}

	if deepmind.Enabled {
		if !deepmind.IsInTransaction() {
			debug.PrintStack()
			panic("the ApplyTransaction should have been call within a transaction, something is deeply wrong")
		}

		deepmind.Print("TRX_FROM", deepmind.Addr(msg.From()))
	}

	// Create a new context to be used in the EVM environment
	context := NewEVMContext(msg, header, bc, author)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	vmenv := vm.NewEVM(context, statedb, config, cfg)
	// Apply the transaction to the current state (included in the env)
	_, gas, failed, err := ApplyMessage(vmenv, msg, gp)
	if err != nil {
		return nil, err
	}
	// Update the state with pending changes
	var root []byte
	if config.IsByzantium(header.Number) {
		statedb.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(config.IsEIP158(header.Number)).Bytes()
	}
	*usedGas += gas

	// Create a new receipt for the transaction, storing the intermediate root and gas used by the tx
	// based on the eip phase, we're passing whether the root touch-delete accounts.
	receipt := types.NewReceipt(root, failed, *usedGas)
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = gas
	// if the transaction created a contract, store the creation address in the receipt.
	if msg.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(vmenv.Context.Origin, tx.Nonce())
	}
	// Set the receipt logs and create a bloom for filtering
	receipt.Logs = statedb.GetLogs(tx.Hash())
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
	receipt.BlockHash = statedb.BlockHash()
	receipt.BlockNumber = header.Number
	receipt.TransactionIndex = uint(statedb.TxIndex())

	return receipt, err
}

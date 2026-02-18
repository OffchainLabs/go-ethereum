// Copyright 2025, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package core

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// shouldCommit mirrors the sparse archive commit logic in writeBlockWithState (lines 1749-1773).
// It returns a per-block prediction of whether that block's state root gets committed to disk.
func shouldCommit(numBlocks int, skipBlocks uint32, skipGas uint64, gasPerBlock []uint64) []bool {
	committed := make([]bool, numBlocks)
	var blockCounter uint32
	var gasCounter uint64
	for i := 0; i < numBlocks; i++ {
		var maySkip, blockLimitReached, gasLimitReached bool
		if skipBlocks != 0 {
			maySkip = true
			if blockCounter > 0 {
				blockCounter--
			} else {
				blockLimitReached = true
			}
		}
		if skipGas != 0 {
			maySkip = true
			if gasCounter >= gasPerBlock[i] {
				gasCounter -= gasPerBlock[i]
			} else {
				gasLimitReached = true
			}
		}
		if !maySkip || blockLimitReached || gasLimitReached {
			blockCounter = skipBlocks
			gasCounter = skipGas
			committed[i] = true
		}
	}
	return committed
}

func testSparseArchiveCommit(t *testing.T, numBlocks int, skipBlocks uint32, skipGas uint64, emptyBlocks bool) {
	t.Helper()

	var (
		key, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		address = crypto.PubkeyToAddress(key.PublicKey)
		funds   = big.NewInt(1000000000000000)
		gspec   = &Genesis{
			Config:  params.TestChainConfig,
			Alloc:   types.GenesisAlloc{address: {Balance: funds}},
			BaseFee: big.NewInt(params.InitialBaseFee),
		}
		signer = types.LatestSigner(gspec.Config)
	)

	_, blocks, _ := GenerateChainWithGenesis(gspec, ethash.NewFaker(), numBlocks, func(i int, gen *BlockGen) {
		if !emptyBlocks {
			tx, err := types.SignTx(types.NewTransaction(gen.TxNonce(address), common.Address{0x01}, big.NewInt(1), params.TxGas, gen.header.BaseFee, nil), signer, key)
			if err != nil {
				panic(err)
			}
			gen.AddTx(tx)
		}
	})

	gasPerBlock := make([]uint64, numBlocks)
	for i, block := range blocks {
		gasPerBlock[i] = block.GasUsed()
	}
	expected := shouldCommit(numBlocks, skipBlocks, skipGas, gasPerBlock)

	cfg := DefaultConfig()
	cfg.ArchiveMode = true
	cfg.TrieCleanLimit = 0 // disable clean cache so committed roots show as "disk" not "clean"
	cfg.SnapshotLimit = 0
	cfg.MaxNumberOfBlocksToSkipStateSaving = skipBlocks
	cfg.MaxAmountOfGasToSkipStateSaving = skipGas

	db := rawdb.NewMemoryDatabase()
	chain, err := NewBlockChain(db, nil, gspec, ethash.NewFaker(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Stop()

	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert block %d: %v", n, err)
	}

	for i, block := range blocks {
		source := chain.TrieDB().NodeSource(block.Root())
		if expected[i] {
			if source != "disk" {
				t.Errorf("block %d: expected committed (disk), got %q", i+1, source)
			}
		} else {
			if source != "dirty" {
				t.Errorf("block %d: expected skipped (dirty), got %q", i+1, source)
			}
		}
	}
}

func TestSparseArchiveCommit(t *testing.T) {
	t.Parallel()

	numBlocks := 110
	skipBlockValues := []uint32{0, 1, 2, 3, 5, 21, 51, 100, 101}

	// Combined matrix: 9 gas values × 7 block values = 63 cases.
	// Includes blocks-only (skipGas=0), gas-only (skipBlocks=0), and both-zero.
	for _, skipGasMultiplier := range skipBlockValues {
		skipGas := uint64(skipGasMultiplier) * params.TxGas
		for _, skipBlocks := range skipBlockValues[:7] {
			t.Run(fmt.Sprintf("blocks-%d-gas-%d", skipBlocks, skipGas), func(t *testing.T) {
				t.Parallel()
				testSparseArchiveCommit(t, numBlocks, skipBlocks, skipGas, false)
			})
		}
	}

	// Empty blocks: block-skip and gas-skip behave differently with zero-gas blocks.
	t.Run("empty-blocks-skip-3", func(t *testing.T) {
		t.Parallel()
		testSparseArchiveCommit(t, numBlocks, 3, 0, true)
	})
	t.Run("empty-blocks-gas-skip", func(t *testing.T) {
		t.Parallel()
		testSparseArchiveCommit(t, numBlocks, 0, 3*params.TxGas, true)
	})
}

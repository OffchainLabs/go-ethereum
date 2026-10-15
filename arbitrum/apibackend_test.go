// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbitrum

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

type mockArb struct {
	chain *core.BlockChain
}

func (m *mockArb) PublishTransaction(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	return nil
}

func (m *mockArb) BlockChain() *core.BlockChain {
	return m.chain
}

func (m *mockArb) ArbNode() interface{} {
	return nil
}

func TestFeeHistoryRewardPercentileLimit(t *testing.T) {
	backend := &APIBackend{}

	tooMany := make([]float64, maxQueryLimit+1)
	_, _, _, _, _, _, err := backend.FeeHistory(context.Background(), 1, rpc.LatestBlockNumber, tooMany)
	if !errors.Is(err, errInvalidPercentile) {
		t.Fatalf("FeeHistory with %d percentiles: got err %v, want errInvalidPercentile", len(tooMany), err)
	}

	atLimit := make([]float64, maxQueryLimit)
	_, _, _, _, _, _, err = backend.FeeHistory(context.Background(), 1, rpc.LatestBlockNumber, atLimit)
	if errors.Is(err, errInvalidPercentile) {
		t.Fatalf("FeeHistory with %d percentiles: got errInvalidPercentile, want it to pass the bound", len(atLimit))
	}
	if err == nil {
		t.Fatal("FeeHistory on a zero-value backend should still error (ArbOS not installed)")
	}
}

func TestArbitrumHistoryRetention(t *testing.T) {
	// Zero-value backend returns zero HistoryRetention safely without panicking.
	zeroBackend := &APIBackend{}
	zeroRet := zeroBackend.HistoryRetention()
	if zeroRet.StateArchive {
		t.Fatal("expected zero backend to have StateArchive false")
	}

	db := rawdb.NewMemoryDatabase()
	gspec := &core.Genesis{Config: params.AllEthashProtocolChanges}

	// Archive node with custom StateHistory and TxIndexer.
	chainArchive, err := core.NewBlockChain(db, nil, gspec, ethash.NewFaker(), &core.BlockChainConfig{
		ArchiveMode:     true,
		StateHistory:    1000,
		TrienodeHistory: 500,
		TxIndexer: &core.TxIndexerConfig{
			Limit: 5000,
		},
	})
	if err != nil {
		t.Fatalf("failed to create archive blockchain: %v", err)
	}
	defer chainArchive.Stop()

	archiveBackend := &APIBackend{
		b: &Backend{
			arb: &mockArb{chain: chainArchive},
			config: &Config{
				LogHistory:   200,
				LogNoHistory: false,
			},
		},
	}

	ret := archiveBackend.HistoryRetention()
	if !ret.StateArchive {
		t.Fatal("expected StateArchive to be true for archive node")
	}
	if ret.StateScheme != rawdb.HashScheme {
		t.Fatalf("expected StateScheme %s, got %s", rawdb.HashScheme, ret.StateScheme)
	}
	if ret.StateHistory != 1000 {
		t.Fatalf("expected StateHistory 1000, got %d", ret.StateHistory)
	}
	if ret.TrienodeHistory != 500 {
		t.Fatalf("expected TrienodeHistory 500, got %d", ret.TrienodeHistory)
	}
	if ret.TxIndexHistory != 5000 {
		t.Fatalf("expected TxIndexHistory 5000, got %d", ret.TxIndexHistory)
	}
	if ret.LogIndexHistory != 200 {
		t.Fatalf("expected LogIndexHistory 200, got %d", ret.LogIndexHistory)
	}
	if ret.LogIndexDisabled {
		t.Fatal("expected LogIndexDisabled to be false")
	}

	// Non-archive node with disabled log index.
	db2 := rawdb.NewMemoryDatabase()
	chainFull, err := core.NewBlockChain(db2, nil, gspec, ethash.NewFaker(), &core.BlockChainConfig{
		ArchiveMode: false,
	})
	if err != nil {
		t.Fatalf("failed to create full blockchain: %v", err)
	}
	defer chainFull.Stop()

	fullBackend := &APIBackend{
		b: &Backend{
			arb: &mockArb{chain: chainFull},
			config: &Config{
				LogNoHistory: true,
			},
		},
	}

	fullRet := fullBackend.HistoryRetention()
	if fullRet.StateArchive {
		t.Fatal("expected StateArchive to be false for non-archive node")
	}
	if fullRet.StateHistory != 0 {
		t.Fatalf("expected StateHistory 0 for full node, got %d", fullRet.StateHistory)
	}
	if fullRet.TxIndexHistory != 0 {
		t.Fatalf("expected TxIndexHistory 0 for full node with unconfigured TxIndexer, got %d", fullRet.TxIndexHistory)
	}
	if !fullRet.LogIndexDisabled {
		t.Fatal("expected LogIndexDisabled to be true")
	}

	// Node with TxIndexer disabled (nil).
	db3 := rawdb.NewMemoryDatabase()
	chainNoIndexer, err := core.NewBlockChain(db3, nil, gspec, ethash.NewFaker(), &core.BlockChainConfig{
		ArchiveMode: true,
		TxIndexer:   nil,
	})
	if err != nil {
		t.Fatalf("failed to create blockchain with nil TxIndexer: %v", err)
	}
	defer chainNoIndexer.Stop()

	noIndexerBackend := &APIBackend{
		b: &Backend{
			arb: &mockArb{chain: chainNoIndexer},
		},
	}
	noIndexerRet := noIndexerBackend.HistoryRetention()
	if noIndexerRet.TxIndexHistory != 0 {
		t.Fatalf("expected TxIndexHistory 0 when TxIndexer is nil, got %d", noIndexerRet.TxIndexHistory)
	}
}

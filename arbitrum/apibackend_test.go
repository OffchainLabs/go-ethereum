// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbitrum

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
)

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

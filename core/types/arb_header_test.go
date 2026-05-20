// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package types

import (
	"encoding/binary"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

// arbHeader builds a minimally-valid arbitrum header for
// DeserializeHeaderExtraInformation: Difficulty == 1, len(Extra) == 32,
// MixDigest encoding the supplied ArbOS version. baseFee may be a zero,
// positive, or nil *big.Int (the last to test the initial guard).
func arbHeader(t *testing.T, arbosVersion uint64, baseFee *big.Int, time uint64) *Header {
	t.Helper()
	mix := common.Hash{}
	binary.BigEndian.PutUint64(mix[16:24], arbosVersion)
	return &Header{
		Difficulty: new(big.Int).Set(common.Big1),
		Extra:      make([]byte, 32),
		MixDigest:  mix,
		BaseFee:    baseFee,
		Time:       time,
	}
}

// Mutates package-global legacyZeroBaseFeeUntil and restores it on cleanup;
// callers must not use t.Parallel() (concurrent prev/Store/Cleanup would race).
func withLegacyZeroBaseFeeUntil(t *testing.T, ts uint64) {
	t.Helper()
	prev := legacyZeroBaseFeeUntil.Load()
	legacyZeroBaseFeeUntil.Store(ts)
	t.Cleanup(func() { legacyZeroBaseFeeUntil.Store(prev) })
}

func TestDeserializeHeaderExtraInformation(t *testing.T) {
	cases := []struct {
		name         string
		legacyUntil  uint64
		arbosVersion uint64
		baseFee      *big.Int
		time         uint64
		wantEmpty    bool // true => expect HeaderInfo{}; false => expect ArbOSFormatVersion == arbosVersion
	}{
		{
			name:         "default (guard disabled): zero basefee at v40 parses normally",
			legacyUntil:  0,
			arbosVersion: params.ArbosVersion_40,
			baseFee:      big.NewInt(0),
			time:         1_000,
			wantEmpty:    false,
		},
		{
			name:         "legacy mode: zero basefee at v40 below cutoff is non-arbitrum",
			legacyUntil:  1_000_000,
			arbosVersion: params.ArbosVersion_40,
			baseFee:      big.NewInt(0),
			time:         500_000,
			wantEmpty:    true,
		},
		{
			name:         "legacy mode: zero basefee at pre-v40 below cutoff is non-arbitrum",
			legacyUntil:  1_000_000,
			arbosVersion: params.ArbosVersion_11,
			baseFee:      big.NewInt(0),
			time:         500_000,
			wantEmpty:    true,
		},
		{
			name:         "legacy mode boundary: time == cutoff parses normally (strict <)",
			legacyUntil:  500,
			arbosVersion: params.ArbosVersion_40,
			baseFee:      big.NewInt(0),
			time:         500,
			wantEmpty:    false,
		},
		{
			name:         "legacy mode: ArbOS>40 bypasses the gate",
			legacyUntil:  math.MaxUint64,
			arbosVersion: params.ArbosVersion_41,
			baseFee:      big.NewInt(0),
			time:         500,
			wantEmpty:    false,
		},
		{
			name:         "legacy mode: non-zero basefee bypasses the gate",
			legacyUntil:  math.MaxUint64,
			arbosVersion: params.ArbosVersion_40,
			baseFee:      big.NewInt(1),
			time:         500,
			wantEmpty:    false,
		},
		{
			name:         "legacy mode: post-upgrade timestamp parses normally",
			legacyUntil:  1_000_000,
			arbosVersion: params.ArbosVersion_40,
			baseFee:      big.NewInt(0),
			time:         1_500_000,
			wantEmpty:    false,
		},
		{
			name:         "nil basefee always returns empty HeaderInfo",
			legacyUntil:  math.MaxUint64,
			arbosVersion: params.ArbosVersion_40,
			baseFee:      nil,
			time:         500,
			wantEmpty:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withLegacyZeroBaseFeeUntil(t, tc.legacyUntil)
			h := arbHeader(t, tc.arbosVersion, tc.baseFee, tc.time)
			got := DeserializeHeaderExtraInformation(h)
			if tc.wantEmpty {
				if got != (HeaderInfo{}) {
					t.Fatalf("want empty HeaderInfo, got %+v", got)
				}
				return
			}
			if got.ArbOSFormatVersion != tc.arbosVersion {
				t.Fatalf("want ArbOSFormatVersion=%d, got %+v", tc.arbosVersion, got)
			}
		})
	}
}

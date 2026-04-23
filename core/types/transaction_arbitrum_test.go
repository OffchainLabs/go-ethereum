// Copyright 2014 The go-ethereum Authors
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

package types

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Decoder rules for ArbitrumUnsignedTx (0x65) JSON.
func TestArbitrumUnsignedTxJSONDecode(t *testing.T) {
	t.Parallel()
	const (
		mainnetFee  = "0x3b9aca00"
		mainnetHash = "0x5618044241dade84af6c41b7d84496dc9823700f98b79751e257608dac570f6b"
	)
	mainnet := map[string]string{
		"type":    "0x65",
		"chainId": "0xa4b1",
		"from":    "0x5d3919f12bcc35c26eee5f8226a9bee90c257ccc",
		"to":      "0x0000000000000000000000000000000000000da0",
		"nonce":   "0x0",
		"gas":     "0x186a0",
		"value":   "0x683cf676a5cc88b3b50",
		"input":   "0x",
	}
	for _, tc := range []struct {
		name                   string
		gasPrice, maxFeePerGas string
		wantErr, wantFeeCap    string
		checkMainnetHash       bool
	}{
		{name: "mainnet legacy shape", gasPrice: mainnetFee, wantFeeCap: mainnetFee, checkMainnetHash: true},
		{name: "maxFeePerGas only", maxFeePerGas: mainnetFee, wantFeeCap: mainnetFee},
		{name: "both equal", gasPrice: mainnetFee, maxFeePerGas: mainnetFee, wantFeeCap: mainnetFee},
		{name: "precedence gasPrice=0", gasPrice: "0x0", maxFeePerGas: "0x2", wantFeeCap: "0x2"},
		{name: "both zero", gasPrice: "0x0", maxFeePerGas: "0x0", wantFeeCap: "0x0"},
		{name: "conflict", gasPrice: "0x1", maxFeePerGas: "0x2", wantErr: "conflicting gasPrice and maxFeePerGas"},
		{name: "gasPrice=0 alone", gasPrice: "0x0", wantErr: "(or 'gasPrice')"},
		{name: "missing both", wantErr: "(or 'gasPrice')"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := maps.Clone(mainnet)
			if tc.gasPrice != "" {
				m["gasPrice"] = tc.gasPrice
			}
			if tc.maxFeePerGas != "" {
				m["maxFeePerGas"] = tc.maxFeePerGas
			}
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var tx Transaction
			err = json.Unmarshal(data, &tx)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err: got %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got := fmt.Sprintf("0x%x", tx.GasFeeCap()); got != tc.wantFeeCap {
				t.Fatalf("GasFeeCap: got %s, want %s", got, tc.wantFeeCap)
			}
			if tc.checkMainnetHash && tx.Hash() != common.HexToHash(mainnetHash) {
				t.Fatalf("hash: got %s, want %s", tx.Hash(), mainnetHash)
			}
		})
	}
}

func TestTxCalldataUnitsCache(t *testing.T) {
	tx := &Transaction{}
	units := tx.GetCachedCalldataUnits(0)
	if units != nil {
		t.Errorf("unexpected initial cache present %v for compression 0", units)
	}
	units = tx.GetCachedCalldataUnits(1)
	if units != nil {
		t.Errorf("unexpected initial cache present %v for compression 1", units)
	}
	tx.SetCachedCalldataUnits(200, 1000)
	units = tx.GetCachedCalldataUnits(100)
	if units != nil {
		t.Errorf("unexpected cached units %v present for incorrect compression 100", units)
	}
	units = tx.GetCachedCalldataUnits(0)
	if units != nil {
		t.Errorf("unexpected cached units %v present for incorrect compression 0", units)
	}
	units = tx.GetCachedCalldataUnits(200)
	if units == nil || *units != 1000 {
		t.Errorf("unexpected cached units %v for correct compression 200", units)
	}
	tx.SetCachedCalldataUnits(1, 1<<60)
	units = tx.GetCachedCalldataUnits(1)
	if units != nil {
		t.Errorf("unexpected cache value %v present after reset", units)
	}
}

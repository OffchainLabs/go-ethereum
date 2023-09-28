// Copyright 2017 The go-ethereum Authors
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

package params

import (
	"encoding/json"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/math"
)

func TestCheckCompatible(t *testing.T) {
	type test struct {
		stored, new   *ChainConfig
		headBlock     uint64
		headTimestamp uint64
		wantErr       *ConfigCompatError
	}
	tests := []test{
		{stored: AllEthashProtocolChanges, new: AllEthashProtocolChanges, headBlock: 0, headTimestamp: 0, wantErr: nil},
		{stored: AllEthashProtocolChanges, new: AllEthashProtocolChanges, headBlock: 0, headTimestamp: uint64(time.Now().Unix()), wantErr: nil},
		{stored: AllEthashProtocolChanges, new: AllEthashProtocolChanges, headBlock: 100, wantErr: nil},
		{
			stored:    &ChainConfig{EIP150Block: big.NewInt(10)},
			new:       &ChainConfig{EIP150Block: big.NewInt(20)},
			headBlock: 9,
			wantErr:   nil,
		},
		{
			stored:    AllEthashProtocolChanges,
			new:       &ChainConfig{HomesteadBlock: nil},
			headBlock: 3,
			wantErr: &ConfigCompatError{
				What:          "Homestead fork block",
				StoredBlock:   big.NewInt(0),
				NewBlock:      nil,
				RewindToBlock: 0,
			},
		},
		{
			stored:    AllEthashProtocolChanges,
			new:       &ChainConfig{HomesteadBlock: big.NewInt(1)},
			headBlock: 3,
			wantErr: &ConfigCompatError{
				What:          "Homestead fork block",
				StoredBlock:   big.NewInt(0),
				NewBlock:      big.NewInt(1),
				RewindToBlock: 0,
			},
		},
		{
			stored:    &ChainConfig{HomesteadBlock: big.NewInt(30), EIP150Block: big.NewInt(10)},
			new:       &ChainConfig{HomesteadBlock: big.NewInt(25), EIP150Block: big.NewInt(20)},
			headBlock: 25,
			wantErr: &ConfigCompatError{
				What:          "EIP150 fork block",
				StoredBlock:   big.NewInt(10),
				NewBlock:      big.NewInt(20),
				RewindToBlock: 9,
			},
		},
		{
			stored:    &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:       &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(30)},
			headBlock: 40,
			wantErr:   nil,
		},
		{
			stored:    &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:       &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(31)},
			headBlock: 40,
			wantErr: &ConfigCompatError{
				What:          "Petersburg fork block",
				StoredBlock:   nil,
				NewBlock:      big.NewInt(31),
				RewindToBlock: 30,
			},
		},
		{
			stored:        &ChainConfig{ShanghaiTime: newUint64(10)},
			new:           &ChainConfig{ShanghaiTime: newUint64(20)},
			headTimestamp: 9,
			wantErr:       nil,
		},
		{
			stored:        &ChainConfig{ShanghaiTime: newUint64(10)},
			new:           &ChainConfig{ShanghaiTime: newUint64(20)},
			headTimestamp: 25,
			wantErr: &ConfigCompatError{
				What:         "Shanghai fork timestamp",
				StoredTime:   newUint64(10),
				NewTime:      newUint64(20),
				RewindToTime: 9,
			},
		},
	}

	for _, test := range tests {
		err := test.stored.CheckCompatible(test.new, test.headBlock, test.headTimestamp)
		if !reflect.DeepEqual(err, test.wantErr) {
			t.Errorf("error mismatch:\nstored: %v\nnew: %v\nheadBlock: %v\nheadTimestamp: %v\nerr: %v\nwant: %v", test.stored, test.new, test.headBlock, test.headTimestamp, err, test.wantErr)
		}
	}
}

func TestConfigRules(t *testing.T) {
	c := &ChainConfig{
		LondonBlock:  new(big.Int),
		ShanghaiTime: newUint64(500),
	}
	var stamp uint64
	var currentArbosVersion uint64
	if r := c.Rules(big.NewInt(0), true, stamp, currentArbosVersion); r.IsShanghai {
		t.Errorf("expected %v to not be shanghai", currentArbosVersion)
	}
	stamp = 500
	currentArbosVersion = 11
	if r := c.Rules(big.NewInt(0), true, stamp, currentArbosVersion); !r.IsShanghai {
		t.Errorf("expected %v to be shanghai", currentArbosVersion)
	}
	stamp = math.MaxInt64
	currentArbosVersion = math.MaxInt64
	if r := c.Rules(big.NewInt(0), true, stamp, currentArbosVersion); !r.IsShanghai {
		t.Errorf("expected %v to be shanghai", currentArbosVersion)
	}
}

type marshalUnMarshalTest struct {
	input interface{}
	want  interface{}
}

var unmarshalChainConfigTests = []marshalUnMarshalTest{
	{input: `{"arbitrum": {"maxCodeSize": 10, "maxInitCodeSize": 10} }`,
		want: [2]uint64{10, 10}},
	{input: `{"arbitrum": {"maxCodeSize": 10} }`,
		want: [2]uint64{10, MaxInitCodeSize}},
	{input: `{"arbitrum": {"maxInitCodeSize": 10} }`,
		want: [2]uint64{MaxCodeSize, 10}},
	{input: `{"arbitrum": {} }`,
		want: [2]uint64{MaxCodeSize, MaxInitCodeSize}},
	{input: `{"arbitrum": {"maxCodeSize": 0, "maxInitCodeSize": 0} }`,
		want: [2]uint64{MaxCodeSize, MaxInitCodeSize}},
}

func TestUnmarshalChainConfig(t *testing.T) {
	var c ChainConfig
	for _, test := range unmarshalChainConfigTests {
		if err := json.Unmarshal([]byte(test.input.(string)), &c); err != nil {
			t.Errorf("failed to unmarshal. Error: %q", err)
		}
		expected := test.want.([2]uint64)
		if c.ArbitrumChainParams.MaxCodeSize != expected[0] || c.ArbitrumChainParams.MaxInitCodeSize != expected[1] {
			t.Errorf("failed to unmarshal MaxCodeSize and MaxInitCodeSize correctly. unmarshalled as (%d %d) want (%d %d))",
				c.ArbitrumChainParams.MaxCodeSize, c.ArbitrumChainParams.MaxInitCodeSize, expected[0], expected[1])
		}
	}
}

// Copyright 2025 The go-ethereum Authors
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

package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
)

func TestPrecompiledMultigas(t *testing.T) {
	p := &sha256hash{}
	input := "hello world"
	cases := []struct {
		name     string
		supplied uint64
		expected uint64
		wantErr  bool
	}{
		{
			name:     "Success",
			supplied: 100,
			expected: p.RequiredGas([]byte(input)),
		},
		{
			name:     "OutOfGas",
			supplied: 50,
			expected: 50,
			wantErr:  true,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, remainingGas, usedMultiGas, err := RunPrecompiledContract(p, []byte(input), test.supplied, nil, nil)
			if (err == nil) == test.wantErr {
				t.Fatalf("wrong run precompile result: wantErr=%v, got %v", test.wantErr, err)
			}
			usedGas := test.supplied - remainingGas
			if got, want := usedGas, test.expected; got != want {
				t.Errorf("wronge used gas: got %v, want %v", got, want)
			}
			if got, want := usedMultiGas.SingleGas(), usedGas; got != want {
				t.Errorf("used multi-gas does not match single-gas: got %v, want %v", usedMultiGas.SingleGas(), usedGas)
			}
			if got, want := usedMultiGas.Get(multigas.ResourceKindComputation), usedGas; got != want {
				t.Errorf("computation gas does not match single-gas: got %v, want %v", got, want)
			}
		})
	}
}

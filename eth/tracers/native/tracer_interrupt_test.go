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

package native_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

// TestTracerInterruptBeforeTopFrame guards against the empty-callstack panic when a trace
// is interrupted before its top-level frame is captured. Calling Stop before OnEnter makes
// that window deterministic, unlike the scheduler-dependent system-test repro.
func TestTracerInterruptBeforeTopFrame(t *testing.T) {
	stopError := errors.New("execution timeout")

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		To:       &common.Address{},
		Value:    big.NewInt(0),
		Gas:      0,
		GasPrice: big.NewInt(0),
		Data:     nil,
	})

	for _, name := range []string{"callTracer", "flatCallTracer", "erc7562Tracer"} {
		t.Run(name, func(t *testing.T) {
			tracer, err := tracers.DefaultDirectory.New(name, &tracers.Context{}, nil, params.MainnetChainConfig)
			require.NoError(t, err)

			tracer.OnTxStart(&tracing.VMContext{}, tx, common.Address{})

			// Interrupt before the top-level frame is captured; OnEnter then no-ops on the
			// interrupt flag, leaving the callstack empty.
			tracer.Stop(stopError)
			tracer.OnEnter(0, byte(vm.CALL), common.Address{}, common.Address{}, nil, 0, big.NewInt(0))

			var res []byte
			require.NotPanics(t, func() {
				tracer.OnTxEnd(&types.Receipt{GasUsed: 0}, nil)
				var tracerErr error
				res, tracerErr = tracer.GetResult()
				require.Equal(t, stopError, tracerErr)
			})
			require.Nil(t, res)
		})
	}
}

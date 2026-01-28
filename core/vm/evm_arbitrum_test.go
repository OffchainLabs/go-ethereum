package vm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func initCodeForReturn(code []byte) []byte {
	codeLen := len(code)
	offset := 12
	init := []byte{
		byte(PUSH1), byte(codeLen),
		byte(PUSH1), byte(offset),
		byte(PUSH1), byte(0),
		byte(CODECOPY),
		byte(PUSH1), byte(codeLen),
		byte(PUSH1), byte(0),
		byte(RETURN),
	}
	return append(init, code...)
}

func TestCreateStylusComponentPrefixArbosVersion(t *testing.T) {
	creator := common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	rootPrefix := state.NewStylusRootPrefix(0x02)
	classicPrefix := state.NewStylusPrefix(0x01)

	tests := []struct {
		name         string
		arbosVersion uint64
		code         []byte
		wantErr      error
	}{
		{
			name:         "classic-prefix-pre-stylus",
			arbosVersion: params.ArbosVersion_Stylus - 1,
			code:         classicPrefix,
			wantErr:      ErrInvalidCode,
		},
		{
			name:         "root-prefix-pre-contract-limit",
			arbosVersion: params.ArbosVersion_Stylus,
			code:         rootPrefix,
			wantErr:      ErrInvalidCode,
		},
		{
			name:         "root-prefix-contract-limit",
			arbosVersion: params.ArbosVersion_StylusContractLimit,
			code:         rootPrefix,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			statedb.CreateAccount(creator)

			blockCtx := BlockContext{
				CanTransfer:  func(StateDB, common.Address, *uint256.Int) bool { return true },
				Transfer:     func(StateDB, common.Address, common.Address, *uint256.Int) {},
				BlockNumber:  big.NewInt(0),
				ArbOSVersion: tt.arbosVersion,
			}

			chainConfig := *params.TestChainConfig
			chainConfig.ArbitrumChainParams = params.ArbitrumChainParams{
				EnableArbOS:         true,
				InitialArbOSVersion: tt.arbosVersion,
			}

			evm := NewEVM(blockCtx, statedb, &chainConfig, Config{})
			ret, _, _, _, err := evm.Create(creator, initCodeForReturn(tt.code), 1_000_000, uint256.NewInt(0))
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("expected %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("create failed: %v", err)
			}
			if !bytes.Equal(ret, tt.code) {
				t.Fatalf("unexpected created code: %x", ret)
			}
		})
	}
}

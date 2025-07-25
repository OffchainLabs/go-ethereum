package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
)

func TestConstantMultiGas(t *testing.T) {
	for _, tc := range []struct {
		name string
		cost uint64
		op   OpCode
		want *multigas.MultiGas
	}{
		{
			name: "SelfdestructEIP150",
			cost: 5000,
			op:   SELFDESTRUCT,
			want: multigas.MultiGasFromMap(map[multigas.ResourceKind]uint64{
				multigas.ResourceKindComputation:   100,
				multigas.ResourceKindStorageAccess: 4900,
			}),
		},
		{
			name: "SelfdestructLegacy",
			cost: 0,
			op:   SELFDESTRUCT,
			want: multigas.ZeroGas(),
		},
		{
			name: "OtherOpcodes",
			cost: 3,
			op:   ADD, // this covers all other opcodes
			want: multigas.ComputationGas(3),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := constantMultiGas(tc.cost, tc.op); *got != *tc.want {
				t.Errorf("wrong constant multigas: got %v, want %v", got, tc.want)
			}
		})
	}
}

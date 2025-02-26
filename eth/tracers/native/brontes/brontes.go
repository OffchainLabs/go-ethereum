package brontes

import (
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/tracers"
)

func init() {
	tracers.DefaultDirectory.Register("brontesTracer", newBrontesTracer, false)
}

type brontesTracer struct {
	ctx       *tracers.Context
	inspector *BrontesInspector
}

func newBrontesTracerObject(ctx *tracers.Context, _ json.RawMessage) (*brontesTracer, error) {
	return &brontesTracer{
		ctx: ctx,
	}, nil
}

func newBrontesTracer(ctx *tracers.Context, cfg json.RawMessage) (*tracers.Tracer, error) {
	t, err := newBrontesTracerObject(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &tracers.Tracer{
		Hooks: &tracing.Hooks{
			OnTxStart:       t.OnTxStart,
			OnTxEnd:         t.OnTxEnd,
			OnEnter:         t.OnEnter,
			OnExit:          t.OnExit,
			OnOpcode:        t.OnOpcode,
			OnFault:         t.OnFault,
			OnGasChange:     t.OnGasChange,
			OnBalanceChange: t.OnBalanceChange,
			OnNonceChange:   t.OnNonceChange,
			OnCodeChange:    t.OnCodeChange,
			OnStorageChange: t.OnStorageChange,
			OnLog:           t.OnLog,
		},
		GetResult: t.GetResult,
		Stop:      t.Stop,
	}, nil
}

func (t *brontesTracer) OnOpcode(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
}

func (t *brontesTracer) OnFault(pc uint64, op byte, gas, cost uint64, _ tracing.OpContext, depth int, err error) {
}

func (t *brontesTracer) OnGasChange(old, new uint64, reason tracing.GasChangeReason) {}

func (t *brontesTracer) OnEnter(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
}

func (t *brontesTracer) OnExit(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
}

func (t *brontesTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
	// Initialize the BrontesInspector
	config := DefaultTracingInspectorConfig
	t.inspector = NewBrontesInspector(config, env, tx, from)
}

func (*brontesTracer) OnTxEnd(receipt *types.Receipt, err error) {}

func (*brontesTracer) OnBalanceChange(a common.Address, prev, new *big.Int, reason tracing.BalanceChangeReason) {
}

func (*brontesTracer) OnNonceChange(a common.Address, prev, new uint64) {}

func (*brontesTracer) OnCodeChange(a common.Address, prevCodeHash common.Hash, prev []byte, codeHash common.Hash, code []byte) {
}

func (*brontesTracer) OnStorageChange(a common.Address, k, prev, new common.Hash) {}

func (*brontesTracer) OnLog(log *types.Log) {

}

// GetResult returns an empty json object.
func (t *brontesTracer) GetResult() (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// Stop terminates execution of the tracer at the first opportune moment.
func (t *brontesTracer) Stop(err error) {
}

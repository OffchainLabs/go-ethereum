package brontes

import (
	"encoding/json"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/tracers"
	ethlog "github.com/ethereum/go-ethereum/log"
)

func init() {
	tracers.DefaultDirectory.Register("brontesTracer", newBrontesTracer, false)
}

type brontesTracer struct {
	ctx       *tracers.Context
	inspector *BrontesInspector

	// for stopping the tracer
	interrupt atomic.Bool
	reason    error
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
			OnTxStart: t.OnTxStart,
			OnTxEnd:   t.OnTxEnd,
			OnEnter:   t.OnEnter,
			OnExit:    t.OnExit,
			OnOpcode:  t.OnOpcode,
			OnLog:     t.OnLog,
		},
		GetResult: t.GetResult,
		Stop:      t.Stop,
	}, nil
}

// step
func (t *brontesTracer) OnOpcode(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	if t.interrupt.Load() {
		return
	}
	ethlog.Debug("BrontesTracer: OnOpcode", "pc", pc, "op", op, "gas", gas, "cost", cost, "scope", scope, "rData", rData, "depth", depth, "err", err)
	t.inspector.OnOpcode(pc, op, gas, cost, scope, rData, depth, err)
}

func (*brontesTracer) OnFault(pc uint64, op byte, gas, cost uint64, _ tracing.OpContext, depth int, err error) {
}

// Step in
func (t *brontesTracer) OnEnter(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
	if t.interrupt.Load() {
		return
	}
	ethlog.Info("BrontesTracer: OnEnter", "depth", depth, "typ", typ, "from", from.Hex(), "to", to.Hex(), "input", input, "gas", gas, "value", value)
	t.inspector.OnEnter(depth, typ, from, to, input, gas, value)
}

// Step out
func (t *brontesTracer) OnExit(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
	if t.interrupt.Load() {
		return
	}
	ethlog.Info("BrontesTracer: OnExit", "depth", depth, "output", output, "gasUsed", gasUsed, "err", err, "reverted", reverted)
	t.inspector.OnExit(depth, output, gasUsed, err, reverted)
}

func (t *brontesTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
	ethlog.Info("BrontesTracer: Transaction started", "txHash", tx.Hash().Hex(), "from", from.Hex(), "to", tx.To().Hex(), "value", tx.Value(), "gas", tx.Gas(), "blockNumber", env.BlockNumber)
	// Initialize the BrontesInspector
	t.inspector = NewBrontesInspector(DefaultTracingInspectorConfig, env, tx, from)
}

func (t *brontesTracer) OnTxEnd(receipt *types.Receipt, err error) {
	ethlog.Info("BrontesTracer: Transaction ended", "txHash", receipt.TxHash.Hex(), "err", err)
}

func (t *brontesTracer) OnLog(log *types.Log) {
	ethlog.Info("BrontesTracer: Log", "address", log.Address.Hex(), "topics", log.Topics, "data", log.Data)
	if t.interrupt.Load() {
		return
	}
}

// GetResult returns an empty json object.
func (t *brontesTracer) GetResult() (json.RawMessage, error) {
	ethlog.Info("BrontesTracer: GetResult")
	return json.RawMessage(`{}`), nil
}

// Stop terminates execution of the tracer at the first opportune moment.
func (t *brontesTracer) Stop(err error) {
	t.reason = err
	t.interrupt.Store(true)
}

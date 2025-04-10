package live

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/native"
)

// initializer for the tracer
func init() {
	tracers.LiveDirectory.Register("blockGasDimensionByOpcode", NewBlockGasDimensionByOpcodeLogger)
}

// could just be paranoia but better safe than sorry
// avoids overflow by addition
type GasesByDimensionBigInt struct {
	OneDimensionalGasCost *big.Int
	Computation           *big.Int
	StateAccess           *big.Int
	StateGrowth           *big.Int
	HistoryGrowth         *big.Int
	StateGrowthRefund     *big.Int
}

// initializer for empty GasesByDimensionBigInt
func NewGasesByDimensionBigInt() GasesByDimensionBigInt {
	return GasesByDimensionBigInt{
		OneDimensionalGasCost: big.NewInt(0),
		Computation:           big.NewInt(0),
		StateAccess:           big.NewInt(0),
		StateGrowth:           big.NewInt(0),
		HistoryGrowth:         big.NewInt(0),
		StateGrowthRefund:     big.NewInt(0),
	}
}

type blockGasDimensionByOpcodeLiveTraceConfig struct {
	Path string `json:"path"` // Path to directory for output
}

// gasDimensionTracer struct
type BlockGasDimensionByOpcodeLiveTracer struct {
	Path               string `json:"path"` // Path to directory for output
	env                *tracing.VMContext
	blockTimestamp     uint64
	blockNumber        *big.Int
	opcodeToDimensions map[vm.OpCode]GasesByDimensionBigInt
	blockGas           *big.Int
	callStack          native.CallGasDimensionStack
	depth              int
	refundAccumulated  uint64

	// temp big int to avoid a bunch of allocations
	tempBigInt *big.Int

	interrupt atomic.Bool // Atomic flag to signal execution interruption
	reason    error       // Textual reason for the interruption
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewBlockGasDimensionByOpcodeLogger(
	cfg json.RawMessage,
) (*tracing.Hooks, error) {
	var config blockGasDimensionByOpcodeLiveTraceConfig
	if err := json.Unmarshal(cfg, &config); err != nil {
		return nil, err
	}

	if config.Path == "" {
		return nil, fmt.Errorf("block gas dimension live tracer path for output is required: %v", config)
	}

	t := &BlockGasDimensionByOpcodeLiveTracer{
		Path:               config.Path,
		depth:              1,
		refundAccumulated:  0,
		blockGas:           big.NewInt(0),
		blockNumber:        big.NewInt(-1),
		tempBigInt:         big.NewInt(0),
		blockTimestamp:     0,
		opcodeToDimensions: make(map[vm.OpCode]GasesByDimensionBigInt),
	}

	return &tracing.Hooks{
		OnOpcode:     t.OnOpcode,
		OnTxStart:    t.OnTxStart,
		OnTxEnd:      t.OnTxEnd,
		OnBlockStart: t.OnBlockStart,
		OnBlockEnd:   t.OnBlockEnd,
	}, nil
}

// ############################################################################
//                                    HOOKS
// ############################################################################

// hook into each opcode execution
func (t *BlockGasDimensionByOpcodeLiveTracer) OnOpcode(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) {
	if t.interrupt.Load() {
		return
	}
	if depth != t.depth && depth != t.depth-1 {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"expected depth fell out of sync with actual depth: %d %d != %d, callStack: %v",
			pc,
			t.depth,
			depth,
			t.callStack,
		)
		return
	}
	if t.depth != len(t.callStack)+1 {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"depth fell out of sync with callStack: %d %d != %d, callStack: %v",
			pc,
			t.depth,
			len(t.callStack),
			t.callStack,
		)
		return
	}

	// get the gas dimension function
	// if it's not a call, directly calculate the gas dimensions for the opcode
	f := native.GetCalcGasDimensionFunc(vm.OpCode(op))
	gasesByDimension, callStackInfo, err := f(t, pc, op, gas, cost, scope, rData, depth, err)
	if err != nil {
		t.interrupt.Store(true)
		t.reason = err
		return
	}
	opcode := vm.OpCode(op)

	if native.WasCallOrCreate(opcode) && callStackInfo == nil || !native.WasCallOrCreate(opcode) && callStackInfo != nil {
		t.interrupt.Store(true)
		t.reason = fmt.Errorf(
			"logic bug, calls/creates should always be accompanied by callStackInfo and non-calls should not have callStackInfo %d %s %v",
			pc,
			opcode.String(),
			callStackInfo,
		)
		return
	}

	// if callStackInfo is not nil then we need to take a note of the index of the
	// DimensionLog that represents this opcode and save the callStackInfo
	// to call finishX after the call has returned
	if native.WasCallOrCreate(opcode) {
		t.callStack.Push(
			native.CallGasDimensionStackInfo{
				GasDimensionInfo:     *callStackInfo,
				DimensionLogPosition: 0, //unused in this tracer
				ExecutionCost:        0,
			})
		t.depth += 1
	} else {

		// update the aggregrate map for this opcode
		accumulatedDimensions, exists := t.opcodeToDimensions[opcode]
		if !exists {
			accumulatedDimensions = NewGasesByDimensionBigInt()
		}

		// add the gas dimensions for this opcode to the accumulated dimensions
		t.addGasesByDimension(&accumulatedDimensions, gasesByDimension)

		t.opcodeToDimensions[opcode] = accumulatedDimensions

		// if the opcode returns from the call stack depth, or
		// if this is an opcode immediately after a call that did not increase the stack depth
		// because it called an empty account or contract or wrong function signature,
		// call the appropriate finishX function to write the gas dimensions
		// for the call that increased the stack depth in the past
		if depth < t.depth {
			stackInfo, ok := t.callStack.Pop()
			// base case, stack is empty, do nothing
			if !ok {
				t.interrupt.Store(true)
				t.reason = fmt.Errorf("call stack is unexpectedly empty %d %d %d", pc, depth, t.depth)
				return
			}
			finishFunction := native.GetFinishCalcGasDimensionFunc(stackInfo.GasDimensionInfo.Op)
			if finishFunction == nil {
				t.interrupt.Store(true)
				t.reason = fmt.Errorf(
					"no finish function found for opcode %s, call stack is messed up %d",
					stackInfo.GasDimensionInfo.Op.String(),
					pc,
				)
				return
			}
			// IMPORTANT NOTE: for some reason the only reliable way to actually get the gas cost of the call
			// is to subtract gas at time of call from gas at opcode AFTER return
			// you can't trust the `gas` field on the call itself. I wonder if the gas field is an estimation
			gasUsedByCall := stackInfo.GasDimensionInfo.GasCounterAtTimeOfCall - gas
			gasesByDimensionCall := finishFunction(gasUsedByCall, stackInfo.ExecutionCost, stackInfo.GasDimensionInfo)
			accumulatedDimensionsCall, exists := t.opcodeToDimensions[stackInfo.GasDimensionInfo.Op]
			if !exists {
				accumulatedDimensionsCall = NewGasesByDimensionBigInt()
			}

			t.addGasesByDimension(&accumulatedDimensionsCall, gasesByDimensionCall)
			t.opcodeToDimensions[stackInfo.GasDimensionInfo.Op] = accumulatedDimensionsCall
			t.depth -= 1
		}

		// if we are in a call stack depth greater than 0,
		// then we need to track the execution gas
		// of our own code so that when the call returns,
		// we can write the gas dimensions for the call opcode itself
		if len(t.callStack) > 0 {
			t.callStack.UpdateExecutionCost(cost)
		}
	}
}

// on tx start, get the environment and set the depth to 1
func (t *BlockGasDimensionByOpcodeLiveTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
	t.env = env
	t.depth = 1
	t.refundAccumulated = 0 // refunds per tx
}

// on tx end, add the gas used to the block gas
func (t *BlockGasDimensionByOpcodeLiveTracer) OnTxEnd(receipt *types.Receipt, err error) {
	t.blockGas.Add(t.blockGas, new(big.Int).SetUint64(receipt.GasUsed))
}

// on block start take note of the block timestamp and number
func (t *BlockGasDimensionByOpcodeLiveTracer) OnBlockStart(ev tracing.BlockEvent) {
	t.blockTimestamp = ev.Block.Time()
	t.blockNumber = ev.Block.Number()
}

// on block end, write out the gas dimensions for each opcode in the block to file
func (t *BlockGasDimensionByOpcodeLiveTracer) OnBlockEnd(err error) {
	resultJsonBytes, errGettingResult := t.GetResult()
	if errGettingResult != nil {
		errorJsonString := fmt.Sprintf("{\"errorGettingResult\": \"%s\"}", errGettingResult.Error())
		fmt.Println(errorJsonString)
		resultJsonBytes = []byte(errorJsonString)
		return
	}

	filename := fmt.Sprintf("%s.json", t.blockNumber.String())
	filepath := filepath.Join(t.Path, filename)

	// Ensure the directory exists
	if err := os.MkdirAll(t.Path, 0755); err != nil {
		fmt.Printf("Failed to create directory %s: %v\n", t.Path, err)
		return
	}

	// Write the file
	if err := os.WriteFile(filepath, resultJsonBytes, 0644); err != nil {
		fmt.Printf("Failed to write file %s: %v\n", filepath, err)
		return
	}

}

// ############################################################################
//                                HELPERS
// ############################################################################

func (t *BlockGasDimensionByOpcodeLiveTracer) GetOpRefund() uint64 {
	return t.env.StateDB.GetRefund()
}

func (t *BlockGasDimensionByOpcodeLiveTracer) GetRefundAccumulated() uint64 {
	return t.refundAccumulated
}

func (t *BlockGasDimensionByOpcodeLiveTracer) SetRefundAccumulated(refund uint64) {
	t.refundAccumulated = refund
}

// avoid allocating a lot of big ints in a loop
func (t *BlockGasDimensionByOpcodeLiveTracer) addGasesByDimension(target *GasesByDimensionBigInt, value native.GasesByDimension) {
	t.tempBigInt.SetUint64(value.OneDimensionalGasCost)
	target.OneDimensionalGasCost.Add(target.OneDimensionalGasCost, t.tempBigInt)
	t.tempBigInt.SetUint64(value.Computation)
	target.Computation.Add(target.Computation, t.tempBigInt)
	t.tempBigInt.SetUint64(value.StateAccess)
	target.StateAccess.Add(target.StateAccess, t.tempBigInt)
	t.tempBigInt.SetUint64(value.StateGrowth)
	target.StateGrowth.Add(target.StateGrowth, t.tempBigInt)
	t.tempBigInt.SetUint64(value.HistoryGrowth)
	target.HistoryGrowth.Add(target.HistoryGrowth, t.tempBigInt)
	t.tempBigInt.SetInt64(value.StateGrowthRefund)
	target.StateGrowthRefund.Add(target.StateGrowthRefund, t.tempBigInt)
}

// ############################################################################
//                        JSON OUTPUT PRODUCTION
// ############################################################################

// ExecutionResult groups all dimension logs emitted by the EVM
// while replaying a transaction in debug mode as well as transaction
// execution status, the amount of gas used and the return value
type BlockGasDimensionByOpcodeExecutionResult struct {
	Gas           *big.Int                          `json:"gas"`
	BlockTimetamp uint64                            `json:"timestamp"`
	BlockNumber   *big.Int                          `json:"blockNumber"`
	Dimensions    map[string]GasesByDimensionBigInt `json:"dimensions"`
}

// produce json result for output from tracer
// this is what the end-user actually gets from the RPC endpoint
func (t *BlockGasDimensionByOpcodeLiveTracer) GetResult() (json.RawMessage, error) {
	// Tracing aborted
	if t.reason != nil {
		return nil, t.reason
	}
	return json.Marshal(&BlockGasDimensionByOpcodeExecutionResult{
		Gas:           t.blockGas,
		Dimensions:    t.GetOpcodeDimensionSummary(),
		BlockTimetamp: t.blockTimestamp,
		BlockNumber:   t.blockNumber,
	})
}

// stringify opcodes for dimension log output
func (t *BlockGasDimensionByOpcodeLiveTracer) GetOpcodeDimensionSummary() map[string]GasesByDimensionBigInt {
	summary := make(map[string]GasesByDimensionBigInt)
	for opcode, dimensions := range t.opcodeToDimensions {
		summary[opcode.String()] = dimensions
	}
	return summary
}

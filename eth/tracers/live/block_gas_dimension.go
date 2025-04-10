package live

/*
import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/native"
)

// tracks the state of the tracer over the lifecycle of the tracer
type blockGasDimensionLiveTracer struct {
	Path               string          `json:"path"` // path directory to write files out to
	GasDimensionTracer *tracers.Tracer // gas dimension tracer. Changes every tx
	blockNumber        *big.Int        // block number. changes every block
	interrupt          atomic.Bool     // Atomic flag to signal execution interruption
	txInterruptor      common.Hash     // track which tx broke the tracer
	reason             error           // Textual reason for the interruption
}

// initialize the tracer
func init() {
	tracers.LiveDirectory.Register("blockGasDimension", newBlockGasDimensionLiveTracer)
}

// config for the tracer
type blockGasDimensionLiveTracerConfig struct {
	Path string `json:"path"` // Path to directory for output
}

// create a new tracer
func newBlockGasDimensionLiveTracer(cfg json.RawMessage) (*tracing.Hooks, error) {
	var config blockGasDimensionLiveTracerConfig
	if err := json.Unmarshal(cfg, &config); err != nil {
		return nil, err
	}

	if config.Path == "" {
		return nil, fmt.Errorf("gas dimension live tracer path for output is required: %v", config)
	}
	t := &blockGasDimensionLiveTracer{
		Path:               config.Path,
		GasDimensionTracer: nil,
		blockNumber:        nil,
	}

	return &tracing.Hooks{
		OnOpcode:     t.OnOpcode,
		OnTxStart:    t.OnTxStart,
		OnTxEnd:      t.OnTxEnd,
		OnBlockStart: t.OnBlockStart,
		OnBlockEnd:   t.OnBlockEnd,
	}, nil
}

// create a gas dimension tracer for this TX
// then hook it to the txStart event
func (t *blockGasDimensionLiveTracer) OnTxStart(vm *tracing.VMContext, tx *types.Transaction, from common.Address) {
	gasDimensionTracer, err := native.NewGasDimensionLogger(nil, nil, nil)
	if err != nil {
		t.reason = err
		t.interrupt.Store(true)
		return
	}
	if t.GasDimensionTracer != nil {
		t.reason = fmt.Errorf("single-threaded execution order violation: gas dimension tracer already exists at time of txStart")
		t.interrupt.Store(true)
		return
	}
	t.GasDimensionTracer = gasDimensionTracer
	t.GasDimensionTracer.OnTxStart(vm, tx, from)
}

// hook the gasDimensionTracer to the opcode event
func (t *blockGasDimensionLiveTracer) OnOpcode(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	if t.interrupt.Load() {
		return
	}
	if t.GasDimensionTracer == nil {
		t.reason = fmt.Errorf("single-threaded execution order violation: gas dimension tracer not found when onOpcode fired")
		t.interrupt.Store(true)
		return
	}
	t.GasDimensionTracer.OnOpcode(pc, op, gas, cost, scope, rData, depth, err)
}

// on TxEnd collate all the information from this transaction
// and store it for the block
func (t *blockGasDimensionLiveTracer) OnTxEnd(receipt *types.Receipt, err error) {
	if t.interrupt.Load() {
		return
	}
	if t.GasDimensionTracer == nil {
		t.reason = fmt.Errorf("single-threaded execution order violation: gas dimension tracer not found when onTxEnd fired")
		t.interrupt.Store(true)
		return
	}
	t.GasDimensionTracer.OnTxEnd(receipt, err)

	// todo
	// go through the gasDimensionTracer's results for this transaction.

	// free the gasDimensionTracer for the next tx
	t.GasDimensionTracer = nil
}

// on block start create a struct to store the gas dimensions for the block
func (t *blockGasDimensionLiveTracer) OnBlockStart(ev tracing.BlockEvent) {
	fmt.Println("Live Tracer Seen: new block", ev.Block.Number())
}

// on block end write the block's data to a file
func (t *blockGasDimensionLiveTracer) OnBlockEnd(err error) {
	fmt.Println("Live Tracer Seen block end")
	blockNumber := t.blockNumber.String()

	// Create the filename
	filename := fmt.Sprintf("%s.json", blockNumber)
	filepath := filepath.Join(t.Path, filename)

	// Ensure the directory exists
	if err := os.MkdirAll(t.Path, 0755); err != nil {
		fmt.Printf("Failed to create directory %s: %v\n", t.Path, err)
		return
	}

	// todo
	// create executionResultJsonBytes

	// Write the file
	if err := os.WriteFile(filepath, executionResultJsonBytes, 0644); err != nil {
		fmt.Printf("Failed to write file %s: %v\n", filepath, err)
		return
	}
}


*/

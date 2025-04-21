package live

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/native"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	_vm "github.com/ethereum/go-ethereum/core/vm"
)

// initializer for the tracer
func init() {
	tracers.LiveDirectory.Register("txGasDimensionByOpcode", NewTxGasDimensionByOpcodeLogger)
}

type txGasDimensionByOpcodeLiveTraceConfig struct {
	Path string `json:"path"` // Path to directory for output
}

// gasDimensionTracer struct
type TxGasDimensionByOpcodeLiveTracer struct {
	Path               string `json:"path"` // Path to directory for output
	GasDimensionTracer *native.TxGasDimensionByOpcodeTracer
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewTxGasDimensionByOpcodeLogger(
	cfg json.RawMessage,
) (*tracing.Hooks, error) {
	var config txGasDimensionByOpcodeLiveTraceConfig
	if err := json.Unmarshal(cfg, &config); err != nil {
		return nil, err
	}

	if config.Path == "" {
		return nil, fmt.Errorf("tx gas dimension live tracer path for output is required: %v", config)
	}

	t := &TxGasDimensionByOpcodeLiveTracer{
		Path:               config.Path,
		GasDimensionTracer: nil,
	}

	return &tracing.Hooks{
		OnOpcode:     t.OnOpcode,
		OnTxStart:    t.OnTxStart,
		OnTxEnd:      t.OnTxEnd,
		OnBlockStart: t.OnBlockStart,
		OnBlockEnd:   t.OnBlockEnd,
	}, nil
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnTxStart(
	vm *tracing.VMContext,
	tx *types.Transaction,
	from common.Address,
) {
	if t.GasDimensionTracer != nil {
		fmt.Println("Error seen in the gas dimension live tracer lifecycle!")
	}

	t.GasDimensionTracer = &native.TxGasDimensionByOpcodeTracer{
		BaseGasDimensionTracer: native.NewBaseGasDimensionTracer(),
		OpcodeToDimensions:     make(map[_vm.OpCode]native.GasesByDimension),
	}
	t.GasDimensionTracer.OnTxStart(vm, tx, from)
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnOpcode(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	rData []byte,
	depth int,
	err error,
) {
	t.GasDimensionTracer.OnOpcode(pc, op, gas, cost, scope, rData, depth, err)
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnTxEnd(
	receipt *types.Receipt,
	err error,
) {
	// first call the native tracer's OnTxEnd
	t.GasDimensionTracer.OnTxEnd(receipt, err)

	// system transactions don't use any gas
	// they can be skipped
	if receipt.GasUsed != 0 {
		executionResultBytes, err := t.GasDimensionTracer.GetProtobufResult()
		if err != nil {
			fmt.Printf("Failed to get protobuf result: %v\n", err)
			return
		}

		blockNumber := receipt.BlockNumber.String()
		txHashString := receipt.TxHash.Hex()

		// Create the file path
		filename := fmt.Sprintf("%s.pb", txHashString)
		dirPath := filepath.Join(t.Path, blockNumber)
		filepath := filepath.Join(dirPath, filename)

		// Ensure the directory exists (including block number subdirectory)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			fmt.Printf("Failed to create directory %s: %v\n", dirPath, err)
			return
		}

		// Write the file
		if err := os.WriteFile(filepath, executionResultBytes, 0644); err != nil {
			fmt.Printf("Failed to write file %s: %v\n", filepath, err)
			return
		}
	}

	// reset the tracer
	t.GasDimensionTracer = nil
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnBlockStart(ev tracing.BlockEvent) {
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnBlockEnd(err error) {
}

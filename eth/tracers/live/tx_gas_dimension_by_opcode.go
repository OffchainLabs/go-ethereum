package live

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/native"
	"github.com/ethereum/go-ethereum/params"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	_vm "github.com/ethereum/go-ethereum/core/vm"
)

// initializer for the tracer
func init() {
	tracers.LiveDirectory.Register("txGasDimensionByOpcode", NewTxGasDimensionByOpcodeLogger)
}

type txGasDimensionByOpcodeLiveTraceConfig struct {
	Path        string              `json:"path"` // Path to directory for output
	ChainConfig *params.ChainConfig `json:"chainConfig"`
}

// gasDimensionTracer struct
type TxGasDimensionByOpcodeLiveTracer struct {
	Path               string `json:"path"` // Path to directory for output
	ChainConfig        *params.ChainConfig
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

	// if you get stuck here, look at
	// cmd/chaininfo/arbitrum_chain_info.json
	// for a sample chain config
	if config.ChainConfig == nil {
		return nil, fmt.Errorf("tx gas dimension live tracer chain config is required: %v", config)
	}

	t := &TxGasDimensionByOpcodeLiveTracer{
		Path:               config.Path,
		ChainConfig:        config.ChainConfig,
		GasDimensionTracer: nil,
	}

	return &tracing.Hooks{
		OnOpcode:          t.OnOpcode,
		OnFault:           t.OnFault,
		OnTxStart:         t.OnTxStart,
		OnTxEnd:           t.OnTxEnd,
		OnBlockStart:      t.OnBlockStart,
		OnBlockEnd:        t.OnBlockEnd,
		OnBlockEndMetrics: t.OnBlockEndMetrics,
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
		BaseGasDimensionTracer: native.NewBaseGasDimensionTracer(t.ChainConfig),
		OpcodeToDimensions:     make(map[_vm.OpCode]native.GasesByDimension),
	}
	t.GasDimensionTracer.OnTxStart(vm, tx, from)
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnFault(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	depth int,
	err error,
) {
	t.GasDimensionTracer.OnFault(pc, op, gas, cost, scope, depth, err)
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

	blockNumber := receipt.BlockNumber.String()
	txHashString := receipt.TxHash.Hex()

	// Handle errors by saving them to a separate errors directory
	// Note: We only consider errors in the tracing process itself, not transaction reverts
	tracerErr := t.GasDimensionTracer.Reason()
	if tracerErr != nil || err != nil {
		// Create error directory path
		errorDirPath := filepath.Join(t.Path, "errors", blockNumber)
		if err := os.MkdirAll(errorDirPath, 0755); err != nil {
			fmt.Printf("Failed to create error directory %s: %v\n", errorDirPath, err)
			return
		}

		// Create error info structure
		errorInfo := struct {
			TxHash       string      `json:"txHash"`
			BlockNumber  string      `json:"blockNumber"`
			Error        string      `json:"error"`
			TracerError  string      `json:"tracerError,omitempty"`
			GasUsed      uint64      `json:"gasUsed"`
			GasUsedForL1 uint64      `json:"gasUsedForL1"`
			GasUsedForL2 uint64      `json:"gasUsedForL2"`
			IntrinsicGas uint64      `json:"intrinsicGas"`
			Dimensions   interface{} `json:"dimensions,omitempty"`
		}{
			TxHash:       txHashString,
			BlockNumber:  blockNumber,
			Error:        err.Error(),
			GasUsed:      receipt.GasUsed,
			GasUsedForL1: receipt.GasUsedForL1,
			GasUsedForL2: receipt.GasUsedForL2(),
		}

		// Add tracer error if present
		if tracerErr != nil {
			errorInfo.TracerError = tracerErr.Error()
		}

		// Try to get any available gas dimension data
		dimensions := t.GasDimensionTracer.GetOpcodeDimensionSummary()
		errorInfo.Dimensions = dimensions

		// Marshal error info to JSON
		errorData, err := json.MarshalIndent(errorInfo, "", "  ")
		if err != nil {
			fmt.Printf("Failed to marshal error info: %v\n", err)
			return
		}

		// Write error file
		errorFilepath := filepath.Join(errorDirPath, fmt.Sprintf("%s.json", txHashString))
		if err := os.WriteFile(errorFilepath, errorData, 0644); err != nil {
			fmt.Printf("Failed to write error file %s: %v\n", errorFilepath, err)
			return
		}
	} else {
		// system transactions don't use any gas
		// they can be skipped
		if receipt.GasUsed != 0 {
			executionResultBytes, err := t.GasDimensionTracer.GetProtobufResult()
			if err != nil {
				fmt.Printf("Failed to get protobuf result: %v\n", err)
				return
			}

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
	}

	// reset the tracer
	t.GasDimensionTracer = nil
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnBlockStart(ev tracing.BlockEvent) {
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnBlockEnd(err error) {
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnBlockEndMetrics(blockNumber uint64, blockInsertDuration time.Duration) {
	filename := fmt.Sprintf("%d.txt", blockNumber)
	dirPath := filepath.Join(t.Path, "blocks")
	filepath := filepath.Join(dirPath, filename)

	// Ensure the directory exists
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		fmt.Printf("Failed to create directory %s: %v\n", dirPath, err)
		return
	}

	// the output is the duration in nanoseconds
	var outData int64 = blockInsertDuration.Nanoseconds()

	// Write the file
	if err := os.WriteFile(filepath, fmt.Appendf(nil, "%d", outData), 0644); err != nil {
		fmt.Printf("Failed to write file %s: %v\n", filepath, err)
		return
	}
}

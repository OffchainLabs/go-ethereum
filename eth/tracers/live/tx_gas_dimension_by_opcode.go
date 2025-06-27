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
	"github.com/ethereum/go-ethereum/log"
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
	Path                    string                               `json:"path"` // Path to directory for output
	ChainConfig             *params.ChainConfig                  // chain config, needed for the tracer
	skip                    bool                                 // skip hooking system transactions
	nativeGasByOpcodeTracer *native.TxGasDimensionByOpcodeTracer // the native tracer that does all the actual work
}

// an struct to capture information about errors in tracer execution
type TxGasDimensionByOpcodeLiveTraceErrorInfo struct {
	TxHash       string      `json:"txHash"`
	BlockNumber  string      `json:"blockNumber"`
	Error        string      `json:"error"`
	TracerError  string      `json:"tracerError,omitempty"`
	Status       uint64      `json:"status"`
	GasUsed      uint64      `json:"gasUsed"`
	GasUsedForL1 uint64      `json:"gasUsedForL1"`
	GasUsedForL2 uint64      `json:"gasUsedForL2"`
	IntrinsicGas uint64      `json:"intrinsicGas"`
	Dimensions   interface{} `json:"dimensions,omitempty"`
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
	// be sure path exists
	os.MkdirAll(config.Path, 0755)

	// if you get stuck here, look at
	// cmd/chaininfo/arbitrum_chain_info.json
	// for a sample chain config
	if config.ChainConfig == nil {
		return nil, fmt.Errorf("tx gas dimension live tracer chain config is required: %v", config)
	}

	t := &TxGasDimensionByOpcodeLiveTracer{
		Path:                    config.Path,
		ChainConfig:             config.ChainConfig,
		skip:                    false,
		nativeGasByOpcodeTracer: nil,
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
	if t.nativeGasByOpcodeTracer != nil {
		log.Error("Error seen in the gas dimension live tracer lifecycle!")
	}

	// we skip internal / system transactions
	if tx.Type() == types.ArbitrumInternalTxType {
		t.skip = true
		return
	}
	baseGasDimensionTracer, err := native.NewBaseGasDimensionTracer(nil, t.ChainConfig)
	if err != nil {
		log.Error("Failed to create base gas dimension tracer", "error", err)
		return
	}

	t.nativeGasByOpcodeTracer = &native.TxGasDimensionByOpcodeTracer{
		BaseGasDimensionTracer: baseGasDimensionTracer,
		OpcodeToDimensions:     make(map[_vm.OpCode]native.GasesByDimension),
	}
	t.nativeGasByOpcodeTracer.OnTxStart(vm, tx, from)
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnFault(
	pc uint64,
	op byte,
	gas, cost uint64,
	scope tracing.OpContext,
	depth int,
	err error,
) {
	if t.skip {
		return
	}
	t.nativeGasByOpcodeTracer.OnFault(pc, op, gas, cost, scope, depth, err)
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
	if t.skip {
		return
	}
	t.nativeGasByOpcodeTracer.OnOpcode(pc, op, gas, cost, scope, rData, depth, err)
}

func (t *TxGasDimensionByOpcodeLiveTracer) OnTxEnd(
	receipt *types.Receipt,
	err error,
) {
	// If we skipped this transaction, just reset and return
	if t.skip {
		t.nativeGasByOpcodeTracer = nil
		t.skip = false
		return
	}

	// first call the native tracer's OnTxEnd
	t.nativeGasByOpcodeTracer.OnTxEnd(receipt, err)

	tracerErr := t.nativeGasByOpcodeTracer.Reason()

	if tracerErr != nil || err != nil || receipt == nil {
		writeTxErrorToFile(t, receipt, err, tracerErr)
	} else { // tx did not have any errors
		writeTxSuccessToFile(t, receipt)
	}

	// reset the tracer
	t.nativeGasByOpcodeTracer = nil
	t.skip = false
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
		log.Error("Failed to create directory", "path", dirPath, "error", err)
		return
	}

	// the output is the duration in nanoseconds
	var outData int64 = blockInsertDuration.Nanoseconds()

	// Write the file
	if err := os.WriteFile(filepath, fmt.Appendf(nil, "%d", outData), 0644); err != nil {
		log.Error("Failed to write file", "path", filepath, "error", err)
		return
	}
}

// if the transaction has any kind of error, try to get as much information
// as you can out of it, and then write that out to a file underneath
// Path/errors/block_group/blocknumber_txhash.json
func writeTxErrorToFile(t *TxGasDimensionByOpcodeLiveTracer, receipt *types.Receipt, err error, tracerError error) {
	var txHashStr string = "no-tx-hash"
	var blockNumberStr string = "no-block-number"
	var errorInfo TxGasDimensionByOpcodeLiveTraceErrorInfo

	var errStr string = ""
	var tracerErrStr string = ""
	if err != nil {
		errStr = err.Error()
	}
	if tracerError != nil {
		tracerErrStr = tracerError.Error()
	}
	// dimensions should not error, just return empty if there is no data
	dimensions := t.nativeGasByOpcodeTracer.GetOpcodeDimensionSummary()

	if receipt == nil {
		// we need something to use as a name for the error file,
		// and we have no tx to hash
		txHashStr += time.Now().String()
		outErrString := fmt.Sprintf("receipt is nil, err: %s", errStr)
		errorInfo = TxGasDimensionByOpcodeLiveTraceErrorInfo{
			Error:       outErrString,
			TracerError: tracerErrStr,
			Dimensions:  dimensions,
		}
	} else {
		// if we errored in the tracer because we had an unexpected gas cost mismatch
		blockNumberStr = receipt.BlockNumber.String()
		txHashStr = receipt.TxHash.Hex()

		var intrinsicGas uint64 = 0
		baseExecutionResult, err := t.nativeGasByOpcodeTracer.GetBaseExecutionResult()
		if err == nil {
			intrinsicGas = baseExecutionResult.IntrinsicGas
		}

		errorInfo = TxGasDimensionByOpcodeLiveTraceErrorInfo{
			TxHash:       txHashStr,
			BlockNumber:  blockNumberStr,
			Error:        errStr,
			TracerError:  tracerErrStr,
			Status:       receipt.Status,
			GasUsed:      receipt.GasUsed,
			GasUsedForL1: receipt.GasUsedForL1,
			GasUsedForL2: receipt.GasUsedForL2(),
			IntrinsicGas: intrinsicGas,
			Dimensions:   dimensions,
		}
	}

	// Create error directory path grouped by 1000 blocks
	var errorDirPath string
	if receipt == nil {
		// For nil receipts, use a special directory
		errorDirPath = filepath.Join(t.Path, "errors", "nil-receipts")
	} else {
		// Group by 1000 blocks like successful transactions
		blockNumber := receipt.BlockNumber.Uint64()
		blockGroup := (blockNumber / 1000) * 1000
		errorDirPath = filepath.Join(t.Path, "errors", fmt.Sprintf("%d", blockGroup))
	}

	if err := os.MkdirAll(errorDirPath, 0755); err != nil {
		log.Error("Failed to create error directory", "path", errorDirPath, "error", err)
		return
	}
	// Marshal error info to JSON
	errorData, err := json.MarshalIndent(errorInfo, "", "  ")
	if err != nil {
		log.Error("Failed to marshal error info", "error", err)
		return
	}

	// Write error file
	errorFilepath := filepath.Join(errorDirPath, fmt.Sprintf("%s_%s.json", blockNumberStr, txHashStr))
	if err := os.WriteFile(errorFilepath, errorData, 0644); err != nil {
		log.Error("Failed to write error file", "path", errorFilepath, "error", err)
		return
	}
}

// if the transaction is a non-erroring transaction, write it out to
// the path specified when the tracer was created, under a folder organized by
// every 1000 blocks (this avoids making a huge number of directories,
// which makes analysis iteration over the entire dataset faster)
// the individual filenames are where the filename is blocknumber_txhash.pb
// so you have Path/block_group/blocknumber_txhash.pb
// e.g. Path/1000/1890_0x123abc.pb
func writeTxSuccessToFile(t *TxGasDimensionByOpcodeLiveTracer, receipt *types.Receipt) {
	// system transactions don't use any gas
	// they can be skipped
	if receipt.GasUsed != 0 {
		txHashString := receipt.TxHash.Hex()
		blockNumber := receipt.BlockNumber
		executionResultBytes, err := t.nativeGasByOpcodeTracer.GetProtobufResult()
		if err != nil {
			log.Error("Failed to get protobuf result", "error", err)
			return
		}

		blockGroup := (blockNumber.Uint64() / 1000) * 1000
		dirPath := filepath.Join(t.Path, fmt.Sprintf("%d", blockGroup))
		filename := fmt.Sprintf("%s_%s.pb", blockNumber.String(), txHashString)
		filepath := filepath.Join(dirPath, filename)

		// Ensure the directory exists (including block number subdirectory)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			log.Error("Failed to create directory", "path", dirPath, "error", err)
			return
		}

		// Write the file
		if err := os.WriteFile(filepath, executionResultBytes, 0644); err != nil {
			log.Error("Failed to write file", "path", filepath, "error", err)
			return
		}
	}
}

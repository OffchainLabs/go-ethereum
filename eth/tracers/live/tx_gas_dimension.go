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
	"github.com/ethereum/go-ethereum/eth/tracers/native/proto"
	"github.com/ethereum/go-ethereum/params"
	protobuf "google.golang.org/protobuf/proto"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// initializer for the tracer
func init() {
	tracers.LiveDirectory.Register("txGasDimensionLive", NewTxGasDimensionLiveTracer)
}

type txGasDimensionLiveTraceConfig struct {
	Path        string              `json:"path"` // Path to directory for output
	ChainConfig *params.ChainConfig `json:"chainConfig"`
}

const fileWriteQueueMaxSize = 1000

// gasDimensionTracer struct
// The tracer uses asynchronous file writing to avoid blocking on I/O operations.
// File writes are queued and processed by worker goroutines, ensuring the tracer
// doesn't block the main execution thread.
type TxGasDimensionLiveTracer struct {
	Path                    string                                             `json:"path"` // Path to directory for output
	ChainConfig             *params.ChainConfig                                // chain config, needed for the tracer
	skip                    bool                                               // skip hooking system transactions
	nativeGasTracer         *native.TxGasDimensionTracer                       // the native tracer that does all the actual work
	blockTimestamp          uint64                                             // the timestamp of the currently processing block
	blockBaseFee            uint64                                             // the base fee of the currently processing block
	txWriteQueue            [fileWriteQueueMaxSize]*proto.TxGasDimensionResult // the queue of transactions to write to file
	txWriteQueueSize        int                                                // the size of the file write queue
	blockInfoWriteQueue     [fileWriteQueueMaxSize]*proto.BlockInfo            // the queue of block info to write to file
	blockInfoWriteQueueSize int                                                // the size of the block info write queue
}

// an struct to capture information about errors in tracer execution
type TxGasDimensionLiveTraceErrorInfo struct {
	TxHash       string                  `json:"txHash"`
	BlockNumber  string                  `json:"blockNumber"`
	Error        string                  `json:"error"`
	TracerError  string                  `json:"tracerError,omitempty"`
	Status       uint64                  `json:"status"`
	GasUsed      uint64                  `json:"gasUsed"`
	GasUsedForL1 uint64                  `json:"gasUsedForL1"`
	GasUsedForL2 uint64                  `json:"gasUsedForL2"`
	IntrinsicGas uint64                  `json:"intrinsicGas"`
	Dimensions   native.GasesByDimension `json:"dimensions,omitempty"`
}

// gasDimensionTracer returns a new tracer that traces gas
// usage for each opcode against the dimension of that opcode
// takes a context, and json input for configuration parameters
func NewTxGasDimensionLiveTracer(
	cfg json.RawMessage,
) (*tracing.Hooks, error) {
	var config txGasDimensionLiveTraceConfig
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

	t := &TxGasDimensionLiveTracer{
		Path:            config.Path,
		ChainConfig:     config.ChainConfig,
		skip:            false,
		nativeGasTracer: nil,
	}

	return &tracing.Hooks{
		OnOpcode:          t.OnOpcode,
		OnFault:           t.OnFault,
		OnTxStart:         t.OnTxStart,
		OnTxEnd:           t.OnTxEnd,
		OnBlockStart:      t.OnBlockStart,
		OnBlockEnd:        t.OnBlockEnd,
		OnBlockEndMetrics: t.OnBlockEndMetrics,
		OnClose:           t.Close,
	}, nil
}

func (t *TxGasDimensionLiveTracer) OnTxStart(
	vm *tracing.VMContext,
	tx *types.Transaction,
	from common.Address,
) {
	if t.nativeGasTracer != nil {
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

	t.nativeGasTracer = &native.TxGasDimensionTracer{
		BaseGasDimensionTracer: baseGasDimensionTracer,
		Dimensions:             native.ZeroGasesByDimension(),
	}
	t.nativeGasTracer.OnTxStart(vm, tx, from)
}

func (t *TxGasDimensionLiveTracer) OnFault(
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
	t.nativeGasTracer.OnFault(pc, op, gas, cost, scope, depth, err)
}

func (t *TxGasDimensionLiveTracer) OnOpcode(
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
	t.nativeGasTracer.OnOpcode(pc, op, gas, cost, scope, rData, depth, err)
}

func (t *TxGasDimensionLiveTracer) OnTxEnd(
	receipt *types.Receipt,
	err error,
) {
	// If we skipped this transaction, just reset and return
	if t.skip {
		t.nativeGasTracer = nil
		t.skip = false
		return
	}

	// first call the native tracer's OnTxEnd
	t.nativeGasTracer.OnTxEnd(receipt, err)

	tracerErr := t.nativeGasTracer.Reason()

	if tracerErr != nil || err != nil || receipt == nil {
		writeTxErrorToFile(t, receipt, err, tracerErr)
	} else { // tx did not have any errors
		writeTxSuccessToFile(t, receipt)
	}

	// reset the tracer
	t.nativeGasTracer = nil
	t.skip = false
}

func (t *TxGasDimensionLiveTracer) OnBlockStart(ev tracing.BlockEvent) {
	t.blockTimestamp = 0
	t.blockBaseFee = 0
	if ev.Block != nil {
		t.blockTimestamp = ev.Block.Time()
		t.blockBaseFee = ev.Block.BaseFee().Uint64()
	}
}

func (t *TxGasDimensionLiveTracer) OnBlockEnd(err error) {
}

func (t *TxGasDimensionLiveTracer) OnBlockEndMetrics(blockNumber uint64, blockInsertDuration time.Duration) {
	// Calculate block group number (every 1000 blocks)
	blockGroup := (blockNumber / 1000) * 1000

	// Create BlockInfo protobuf message
	blockInfo := &proto.BlockInfo{
		Timestamp:                 t.blockTimestamp,
		InsertDurationNanoseconds: uint64(blockInsertDuration.Nanoseconds()),
		BaseFee:                   t.blockBaseFee,
		BlockNumber:               blockNumber,
	}

	// Add to block info write queue
	t.blockInfoWriteQueue[t.blockInfoWriteQueueSize] = blockInfo
	t.blockInfoWriteQueueSize++

	// If queue is full, write the batch
	if t.blockInfoWriteQueueSize >= fileWriteQueueMaxSize {
		t.writeBlockBatchToFile(fmt.Sprintf("%d", blockGroup))
	}
}

// writeBlockBatchToFile converts the blockInfoWriteQueue to a BlockInfoBatch,
// marshals it to bytes, and writes it to a file
func (t *TxGasDimensionLiveTracer) writeBlockBatchToFile(blockGroup string) {
	if t.blockInfoWriteQueueSize == 0 {
		return
	}

	// Create the batch from the queue
	batch := &proto.BlockInfoBatch{
		Blocks: make([]*proto.BlockInfo, t.blockInfoWriteQueueSize),
	}

	// Copy block infos from queue to batch
	var i int = 0
	for i < t.blockInfoWriteQueueSize {
		batch.Blocks[i] = t.blockInfoWriteQueue[i]
		i++
	}

	// Marshal the batch to bytes
	batchBytes, err := protobuf.Marshal(batch)
	if err != nil {
		log.Error("Failed to marshal block batch", "error", err)
		return
	}

	// Create directory path
	dirPath := filepath.Join(t.Path, "blocks", blockGroup)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		log.Error("Failed to create directory", "path", dirPath, "error", err)
		return
	}

	// Create filename with timestamp to avoid conflicts
	timestamp := time.Now().UnixNano()
	filename := fmt.Sprintf("block_batch_%d.pb", timestamp)
	filepath := filepath.Join(dirPath, filename)

	// Write the batch file
	if err := os.WriteFile(filepath, batchBytes, 0644); err != nil {
		log.Error("Failed to write block batch file", "path", filepath, "error", err)
		return
	}

	// Reset the queue size to zero so next writes overwrite previous data
	t.blockInfoWriteQueueSize = 0
}

// Close implements the Close method for the tracer hooks
func (t *TxGasDimensionLiveTracer) Close() {
	// Flush any remaining items in the queue before closing
	if t.txWriteQueueSize > 0 {
		t.flushQueue()
	}
	if t.blockInfoWriteQueueSize > 0 {
		t.flushBlockQueue()
	}
}

// writeTxBatchToFile converts the fileWriteQueue to a TxGasDimensionResultBatch,
// marshals it to bytes, and writes it to a file
func (t *TxGasDimensionLiveTracer) writeTxBatchToFile(blockGroup string) {
	if t.txWriteQueueSize == 0 {
		return
	}

	// Create the batch from the queue
	batch := &proto.TxGasDimensionResultBatch{
		Results: make([]*proto.TxGasDimensionResult, t.txWriteQueueSize),
	}

	// Copy results from queue to batch
	var i int = 0
	for i < t.txWriteQueueSize {
		batch.Results[i] = t.txWriteQueue[i]
		i++
	}

	// Marshal the batch to bytes
	batchBytes, err := protobuf.Marshal(batch)
	if err != nil {
		log.Error("Failed to marshal batch", "error", err)
		return
	}

	// Create directory path
	dirPath := filepath.Join(t.Path, blockGroup)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		log.Error("Failed to create directory", "path", dirPath, "error", err)
		return
	}

	// Create filename with timestamp to avoid conflicts
	timestamp := time.Now().UnixNano()
	filename := fmt.Sprintf("batch_%d.pb", timestamp)
	filepath := filepath.Join(dirPath, filename)

	// Write the batch file
	if err := os.WriteFile(filepath, batchBytes, 0644); err != nil {
		log.Error("Failed to write batch file", "path", filepath, "error", err)
		return
	}

	// Reset the queue size to zero so next writes overwrite previous data
	t.txWriteQueueSize = 0
}

// flushQueue is a helper method to flush the current queue contents
func (t *TxGasDimensionLiveTracer) flushQueue() {
	if t.txWriteQueueSize > 0 {
		// Use a default block group for flushing
		t.writeTxBatchToFile("flush")
	}
}

// flushBlockQueue is a helper method to flush the current block queue contents
func (t *TxGasDimensionLiveTracer) flushBlockQueue() {
	if t.blockInfoWriteQueueSize > 0 {
		// Use a default block group for flushing
		t.writeBlockBatchToFile("flush")
	}
}

// if the transaction has any kind of error, try to get as much information
// as you can out of it, and then write that out to a file underneath
// Path/errors/block_group/blocknumber_txhash.json
func writeTxErrorToFile(t *TxGasDimensionLiveTracer, receipt *types.Receipt, err error, tracerError error) {
	var txHashStr string = "no-tx-hash"
	var blockNumberStr string = "no-block-number"
	var errorInfo TxGasDimensionLiveTraceErrorInfo

	var errStr string = ""
	var tracerErrStr string = ""
	if err != nil {
		errStr = err.Error()
	}
	if tracerError != nil {
		tracerErrStr = tracerError.Error()
	}
	if receipt == nil {
		// we need something to use as a name for the error file,
		// and we have no tx to hash
		txHashStr += time.Now().String()
		outErrString := fmt.Sprintf("receipt is nil, err: %s", errStr)
		errorInfo = TxGasDimensionLiveTraceErrorInfo{
			Error:       outErrString,
			TracerError: tracerErrStr,
			Dimensions:  t.nativeGasTracer.Dimensions,
		}
	} else {
		// if we errored in the tracer because we had an unexpected gas cost mismatch
		blockNumberStr = receipt.BlockNumber.String()
		txHashStr = receipt.TxHash.Hex()

		var intrinsicGas uint64 = 0
		baseExecutionResult, err := t.nativeGasTracer.GetBaseExecutionResult()
		if err == nil {
			intrinsicGas = baseExecutionResult.IntrinsicGas
		}

		errorInfo = TxGasDimensionLiveTraceErrorInfo{
			TxHash:       txHashStr,
			BlockNumber:  blockNumberStr,
			Error:        errStr,
			TracerError:  tracerErrStr,
			Status:       receipt.Status,
			GasUsed:      receipt.GasUsed,
			GasUsedForL1: receipt.GasUsedForL1,
			GasUsedForL2: receipt.GasUsedForL2(),
			IntrinsicGas: intrinsicGas,
			Dimensions:   t.nativeGasTracer.Dimensions,
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
func writeTxSuccessToFile(t *TxGasDimensionLiveTracer, receipt *types.Receipt) {
	// system transactions don't use any gas
	// they can be skipped
	if receipt.GasUsed != 0 {
		blockNumber := receipt.BlockNumber
		executionResult, err := t.nativeGasTracer.GetProtobufResult()
		if err != nil {
			log.Error("Failed to get protobuf result", "error", err)
			return
		}

		blockGroup := (blockNumber.Uint64() / 1000) * 1000
		/*
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
		*/
		t.txWriteQueue[t.txWriteQueueSize] = executionResult
		t.txWriteQueueSize++
		if t.txWriteQueueSize >= fileWriteQueueMaxSize {
			t.writeTxBatchToFile(fmt.Sprintf("%d", blockGroup))
		}
	}
}

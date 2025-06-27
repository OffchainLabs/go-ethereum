package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// fileWriteTask represents a file writing task
type fileWriteTask struct {
	filepath string
	data     []byte
	taskType string // "block", "tx_success", "tx_error"
}

// gasDimensionTracer struct
// The tracer uses asynchronous file writing to avoid blocking on I/O operations.
// File writes are queued and processed by worker goroutines, ensuring the tracer
// doesn't block the main execution thread.
type TxGasDimensionLiveTracer struct {
	Path            string                       `json:"path"` // Path to directory for output
	ChainConfig     *params.ChainConfig          // chain config, needed for the tracer
	skip            bool                         // skip hooking system transactions
	nativeGasTracer *native.TxGasDimensionTracer // the native tracer that does all the actual work
	blockTimestamp  uint64                       // the timestamp of the currently processing block
	blockBaseFee    uint64                       // the base fee of the currently processing block

	// Async file writing components
	writeQueue     chan fileWriteTask // Channel for queuing file write tasks
	workerWg       sync.WaitGroup     // WaitGroup for graceful shutdown of workers
	ctx            context.Context    // Context for cancellation
	cancel         context.CancelFunc // Cancel function for graceful shutdown
	writeQueueSize int                // Size of the write queue buffer

	// Queue monitoring
	queueStats struct {
		sync.Mutex
		totalQueued    uint64
		totalProcessed uint64
		totalDropped   uint64
		maxQueueSize   int
	}
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

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())

	t := &TxGasDimensionLiveTracer{
		Path:            config.Path,
		ChainConfig:     config.ChainConfig,
		skip:            false,
		nativeGasTracer: nil,
		writeQueue:      make(chan fileWriteTask, 10000), // Increased buffer size to 10000 tasks
		ctx:             ctx,
		cancel:          cancel,
		writeQueueSize:  10000,
	}

	// Start async file writing workers
	t.startFileWorkers()

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
	filename := fmt.Sprintf("%d.pb", blockNumber)

	// Create path with block group subdirectory
	dirPath := filepath.Join(t.Path, "blocks", fmt.Sprintf("%d", blockGroup))
	filepath := filepath.Join(dirPath, filename)

	// Create BlockInfo protobuf message
	blockInfo := &proto.BlockInfo{
		Timestamp:                 t.blockTimestamp,
		InsertDurationNanoseconds: uint64(blockInsertDuration.Nanoseconds()),
		BaseFee:                   t.blockBaseFee,
	}

	// Marshal the protobuf message
	blockInfoBytes, err := protobuf.Marshal(blockInfo)
	if err != nil {
		log.Error("Failed to marshal block info", "error", err)
		return
	}

	// Queue the file write task for async processing
	t.queueFileWrite(filepath, blockInfoBytes, "block")
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

	// Marshal error info to JSON
	errorData, err := json.MarshalIndent(errorInfo, "", "  ")
	if err != nil {
		log.Error("Failed to marshal error info", "error", err)
		return
	}

	// Queue the error file write task for async processing
	errorFilepath := filepath.Join(errorDirPath, fmt.Sprintf("%s_%s.json", blockNumberStr, txHashStr))
	t.queueFileWrite(errorFilepath, errorData, "tx_error")
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
		txHashString := receipt.TxHash.Hex()
		blockNumber := receipt.BlockNumber
		executionResultBytes, err := t.nativeGasTracer.GetProtobufResult()
		if err != nil {
			log.Error("Failed to get protobuf result", "error", err)
			return
		}

		blockGroup := (blockNumber.Uint64() / 1000) * 1000
		dirPath := filepath.Join(t.Path, fmt.Sprintf("%d", blockGroup))
		filename := fmt.Sprintf("%s_%s.pb", blockNumber.String(), txHashString)
		filepath := filepath.Join(dirPath, filename)

		// Queue the file write task for async processing
		t.queueFileWrite(filepath, executionResultBytes, "tx_success")
	}
}

// startFileWorkers starts the async file writing worker goroutines
func (t *TxGasDimensionLiveTracer) startFileWorkers() {
	// Start 4 worker goroutines for file writing
	numWorkers := 4
	for i := 0; i < numWorkers; i++ {
		t.workerWg.Add(1)
		go t.fileWorker()
	}
}

// fileWorker is the worker goroutine that processes file writing tasks
func (t *TxGasDimensionLiveTracer) fileWorker() {
	defer t.workerWg.Done()

	for {
		select {
		case task := <-t.writeQueue:
			t.processFileWriteTask(task)
			// Update processed statistics
			t.queueStats.Lock()
			t.queueStats.totalProcessed++
			t.queueStats.Unlock()
		case <-t.ctx.Done():
			return
		}
	}
}

// processFileWriteTask handles the actual file writing with proper error handling
func (t *TxGasDimensionLiveTracer) processFileWriteTask(task fileWriteTask) {
	// Ensure the directory exists
	dir := filepath.Dir(task.filepath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Error("Failed to create directory for async file write", "path", dir, "error", err, "taskType", task.taskType)
		return
	}

	// Write the file
	if err := os.WriteFile(task.filepath, task.data, 0644); err != nil {
		log.Error("Failed to write file asynchronously", "path", task.filepath, "error", err, "taskType", task.taskType)
		return
	}
}

// queueFileWrite safely queues a file writing task for async processing
// This method blocks until space is available in the queue to ensure no writes are dropped
func (t *TxGasDimensionLiveTracer) queueFileWrite(filepath string, data []byte, taskType string) {
	task := fileWriteTask{
		filepath: filepath,
		data:     data,
		taskType: taskType,
	}

	// Update queue statistics
	t.queueStats.Lock()
	t.queueStats.totalQueued++
	currentQueueSize := len(t.writeQueue)
	if currentQueueSize > t.queueStats.maxQueueSize {
		t.queueStats.maxQueueSize = currentQueueSize
	}
	t.queueStats.Unlock()

	// Log queue performance periodically (every 1000 queued tasks)
	if t.queueStats.totalQueued%1000 == 0 {
		t.queueStats.Lock()
		log.Info("File write queue statistics",
			"totalQueued", t.queueStats.totalQueued,
			"totalProcessed", t.queueStats.totalProcessed,
			"currentQueueSize", currentQueueSize,
			"maxQueueSize", t.queueStats.maxQueueSize,
			"queueUtilization", float64(currentQueueSize)/float64(t.writeQueueSize)*100)
		t.queueStats.Unlock()
	}

	// Block until space is available in the queue
	// This ensures no writes are dropped, but may slow down the tracer if I/O is slow
	select {
	case t.writeQueue <- task:
		// Task queued successfully
	case <-t.ctx.Done():
		// Context was cancelled, log and return
		t.queueStats.Lock()
		t.queueStats.totalDropped++
		t.queueStats.Unlock()
		log.Warn("Context cancelled while queuing file write task", "taskType", taskType, "filepath", filepath)
	}
}

// Close gracefully shuts down the async file writing workers
func (t *TxGasDimensionLiveTracer) Close() {
	// Log final statistics
	t.queueStats.Lock()
	log.Info("Final file write queue statistics",
		"totalQueued", t.queueStats.totalQueued,
		"totalProcessed", t.queueStats.totalProcessed,
		"totalDropped", t.queueStats.totalDropped,
		"maxQueueSize", t.queueStats.maxQueueSize)
	t.queueStats.Unlock()

	t.cancel()
	t.workerWg.Wait()
}

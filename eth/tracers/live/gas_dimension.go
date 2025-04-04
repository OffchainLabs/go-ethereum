package live

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/eth/tracers"

	"gopkg.in/natefinch/lumberjack.v2"
)

type gasDimensionLiveTracer struct {
	logger *log.Logger
}

type ExecutionResult struct {
	Relyt  string `json:"relyt"`
	TxHash string `json:"txHash"`
}

func init() {
	tracers.LiveDirectory.Register("gasDimension", newGasDimensionLiveTracer)
}

type gasDimensionLiveTracerConfig struct {
	Path    string `json:"path"`    // Path to directory for output
	MaxSize int    `json:"maxSize"` // MaxSize default 100 MB
}

func newGasDimensionLiveTracer(cfg json.RawMessage) (*tracing.Hooks, error) {
	var config gasDimensionLiveTracerConfig
	if err := json.Unmarshal(cfg, &config); err != nil {
		return nil, err
	}

	if config.Path == "" {
		return nil, errors.New("gas dimension live tracer path for output is required")
	}

	loggerOutput := &lumberjack.Logger{
		Filename: filepath.Join(config.Path, "gas_dimension.jsonl"),
	}

	logger := log.New(loggerOutput, "", 0)

	t := &gasDimensionLiveTracer{
		logger: logger,
	}

	return &tracing.Hooks{
		OnTxStart: t.OnTxStart,
		OnTxEnd:   t.OnTxEnd,
		//OnEnter:          t.OnEnter,
		//OnExit:           t.OnExit,
		//OnOpcode:         t.OnOpcode,
		//OnFault:          t.OnFault,
		//OnGasChange:      t.OnGasChange,
		//OnBlockchainInit: t.OnBlockchainInit,
		OnBlockStart: t.OnBlockStart,
		OnBlockEnd:   t.OnBlockEnd,
		//OnSkippedBlock:   t.OnSkippedBlock,
		//OnGenesisBlock:   t.OnGenesisBlock,
		//OnBalanceChange:  t.OnBalanceChange,
		//OnNonceChange:    t.OnNonceChange,
		//OnCodeChange:     t.OnCodeChange,
		//OnStorageChange:  t.OnStorageChange,
		//OnLog:            t.OnLog,
	}, nil
}

/*
func (t *gasDimensionLiveTracer) OnOpcode(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	t.logger.Println("Opcode Seen")
}

func (t *gasDimensionLiveTracer) OnFault(pc uint64, op byte, gas, cost uint64, _ tracing.OpContext, depth int, err error) {
	t.logger.Println("Fault Seen")
}

func (t *gasDimensionLiveTracer) OnEnter(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
	t.logger.Println("Enter Seen")
}

func (t *gasDimensionLiveTracer) OnExit(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
	t.logger.Println("Exit Seen")
}
*/

func (t *gasDimensionLiveTracer) OnTxStart(vm *tracing.VMContext, tx *types.Transaction, from common.Address) {
	t.logger.Println("Tx Start Seen")
}

func (t *gasDimensionLiveTracer) OnTxEnd(receipt *types.Receipt, err error) {
	var executionResult ExecutionResult = ExecutionResult{
		Relyt:  "Uninitialized",
		TxHash: "Uninitialized",
	}
	if err != nil {
		executionResult = ExecutionResult{
			Relyt:  err.Error(),
			TxHash: "nil",
		}
	} else {
		if receipt == nil {
			executionResult = ExecutionResult{
				Relyt:  "Receipt is nil",
				TxHash: "Receipt is nil",
			}
		} else {
			executionResult = ExecutionResult{
				Relyt:  "hello world relyt29",
				TxHash: receipt.TxHash.Hex(),
			}
		}
	}
	executionResultJsonBytes, errMarshalling := json.Marshal(executionResult)
	if errMarshalling != nil {
		errorJsonString := fmt.Sprintf("{\"errorMarshallingJson\": \"%s\"}", errMarshalling.Error())
		t.logger.Println(errorJsonString)
	} else {
		t.logger.Println(string(executionResultJsonBytes))
	}
}

func (t *gasDimensionLiveTracer) OnBlockStart(ev tracing.BlockEvent) {
	t.logger.Println("Block Start")
}

func (t *gasDimensionLiveTracer) OnBlockEnd(err error) {
	t.logger.Println("Block End")
}

/*
func (t *gasDimensionLiveTracer) OnSkippedBlock(ev tracing.BlockEvent) {
	t.logger.Println("Skipped Block")
}

func (t *gasDimensionLiveTracer) OnBlockchainInit(chainConfig *params.ChainConfig) {
	t.logger.Println("Blockchain Init")
}

func (t *gasDimensionLiveTracer) OnGenesisBlock(b *types.Block, alloc types.GenesisAlloc) {
	t.logger.Println("Genesis Block")
}

func (t *gasDimensionLiveTracer) OnBalanceChange(a common.Address, prev, new *big.Int, reason tracing.BalanceChangeReason) {
	t.logger.Println("Balance Change")
}

func (t *gasDimensionLiveTracer) OnNonceChange(a common.Address, prev, new uint64) {
	t.logger.Println("Nonce Change")
}

func (t *gasDimensionLiveTracer) OnCodeChange(a common.Address, prevCodeHash common.Hash, prev []byte, codeHash common.Hash, code []byte) {
	t.logger.Println("Code Change")
}

func (t *gasDimensionLiveTracer) OnStorageChange(a common.Address, k, prev, new common.Hash) {
	t.logger.Println("Storage Change")
}

func (t *gasDimensionLiveTracer) OnLog(l *types.Log) {
	t.logger.Println("Log")
}

func (t *gasDimensionLiveTracer) OnGasChange(old, new uint64, reason tracing.GasChangeReason) {
	t.logger.Println("Gas Change")
}
*/

package live

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/eth/tracers/native"
)

type gasDimensionLiveTracer struct {
	Path               string `json:"path"`
	GasDimensionTracer *tracers.Tracer
}

func init() {
	tracers.LiveDirectory.Register("gasDimension", newGasDimensionLiveTracer)
}

type gasDimensionLiveTracerConfig struct {
	Path string `json:"path"` // Path to directory for output
}

func newGasDimensionLiveTracer(cfg json.RawMessage) (*tracing.Hooks, error) {
	var config gasDimensionLiveTracerConfig
	if err := json.Unmarshal(cfg, &config); err != nil {
		return nil, err
	}

	if config.Path == "" {
		return nil, fmt.Errorf("gas dimension live tracer path for output is required: %v", config)
	}

	gasDimensionTracer, err := native.NewGasDimensionTracer(nil, nil, nil)
	if err != nil {
		return nil, err
	}

	t := &gasDimensionLiveTracer{
		Path:               config.Path,
		GasDimensionTracer: gasDimensionTracer,
	}

	return &tracing.Hooks{
		OnOpcode:     t.OnOpcode,
		OnTxStart:    t.OnTxStart,
		OnTxEnd:      t.OnTxEnd,
		OnBlockStart: t.OnBlockStart,
		OnBlockEnd:   t.OnBlockEnd,
	}, nil
}

func (t *gasDimensionLiveTracer) OnTxStart(vm *tracing.VMContext, tx *types.Transaction, from common.Address) {
	t.GasDimensionTracer.OnTxStart(vm, tx, from)
}

func (t *gasDimensionLiveTracer) OnOpcode(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	t.GasDimensionTracer.OnOpcode(pc, op, gas, cost, scope, rData, depth, err)
}

func (t *gasDimensionLiveTracer) OnTxEnd(receipt *types.Receipt, err error) {
	// first call the navive tracer's OnTxEnd
	t.GasDimensionTracer.OnTxEnd(receipt, err)

	// then get the json from the native tracer
	executionResultJsonBytes, errGettingResult := t.GasDimensionTracer.GetResult()
	if errGettingResult != nil {
		errorJsonString := fmt.Sprintf("{\"errorGettingResult\": \"%s\"}", errGettingResult.Error())
		fmt.Println(errorJsonString)
		return
	}

	blockNumber := receipt.BlockNumber.String()
	txHashString := receipt.TxHash.Hex()

	// Create the filename
	filename := fmt.Sprintf("%s_%s.json", blockNumber, txHashString)
	filepath := filepath.Join(t.Path, filename)

	// Ensure the directory exists
	if err := os.MkdirAll(t.Path, 0755); err != nil {
		fmt.Printf("Failed to create directory %s: %v\n", t.Path, err)
		return
	}

	// Write the file
	if err := os.WriteFile(filepath, executionResultJsonBytes, 0644); err != nil {
		fmt.Printf("Failed to write file %s: %v\n", filepath, err)
		return
	}
}

func (t *gasDimensionLiveTracer) OnBlockStart(ev tracing.BlockEvent) {
	fmt.Println("Live Tracer Seen: new block", ev.Block.Number())
}

func (t *gasDimensionLiveTracer) OnBlockEnd(err error) {
	fmt.Println("Live Tracer Seen block end")
}

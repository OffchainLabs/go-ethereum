package live

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/eth/tracers"
)

type blockInsertTimes struct {
	Path string `json:"path"`
}

func init() {
	tracers.LiveDirectory.Register("blockInsertTimes", newBlockInsertTimes)
}

type blockInsertTimesConfig struct {
	Path string `json:"path"` // Path to directory for output
}

func newBlockInsertTimes(cfg json.RawMessage) (*tracing.Hooks, error) {
	var config blockInsertTimesConfig
	if err := json.Unmarshal(cfg, &config); err != nil {
		return nil, err
	}

	if config.Path == "" {
		return nil, fmt.Errorf("gas dimension live tracer path for output is required: %v", config)
	}

	t := &blockInsertTimes{
		Path: config.Path,
	}

	return &tracing.Hooks{
		OnBlockEndMetrics: t.OnBlockEndMetrics,
	}, nil
}

func (t *blockInsertTimes) OnBlockEndMetrics(blockNumber uint64, blockInsertDuration time.Duration) {
	filename := fmt.Sprintf("%d.json", blockNumber)
	filepath := filepath.Join(t.Path, filename)

	// Ensure the directory exists
	if err := os.MkdirAll(t.Path, 0755); err != nil {
		fmt.Printf("Failed to create directory %s: %v\n", t.Path, err)
		return
	}

	// the output is the duration in nanoseconds
	var outData int64 = blockInsertDuration.Nanoseconds()

	// Write the file
	if err := os.WriteFile(filepath, []byte(fmt.Sprintf("%d", outData)), 0644); err != nil {
		fmt.Printf("Failed to write file %s: %v\n", filepath, err)
		return
	}
}

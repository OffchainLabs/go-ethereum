package multigas

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/arbitrum/multigas/proto"
	"github.com/ethereum/go-ethereum/log"
	protobuf "google.golang.org/protobuf/proto"
)

const (
	// batchFilenameFormat defines the naming pattern for batch files.
	// Format: multigas_batch_<batch_number>_<timestamp>.pb
	batchFilenameFormat = "multigas_batch_%010d_%d.pb"
)

var (
	ErrOutputDirRequired = errors.New("output directory is required")
	ErrBatchSizeRequired = errors.New("batch size must be greater than zero")
	ErrCreateOutputDir   = errors.New("failed to create output directory")
	ErrMarshalBatch      = errors.New("failed to marshal batch")
	ErrWriteBatchFile    = errors.New("failed to write batch file")
)

// BlockMultiGas contains all the multi-dimensional gas data accumulated for a single block
// along with the block's identifying information.
type BlockMultiGas struct {
	MultiGas
	BlockNumber uint64
	BlockHash   string
}

// Config holds the configuration for the MultiGas collector.
type Config struct {
	OutputDir string
	BatchSize uint64
}

// Collector manages the asynchronous collection and batching of multi-dimensional
// gas data from blocks. It receives BlockMultiGas data through a channel, buffers
// it in memory, and periodically writes batches to disk in protobuf format.
type Collector struct {
	config   Config
	input    <-chan *BlockMultiGas
	wg       sync.WaitGroup
	buffer   []*proto.BlockMultiGasData
	batchNum uint64
	mu       sync.Mutex
}

// ToProto converts the BlockMultiGas to its protobuf representation.
func (bmg *BlockMultiGas) ToProto() *proto.BlockMultiGasData {
	return &proto.BlockMultiGasData{
		BlockNumber: bmg.BlockNumber,
		BlockHash:   bmg.BlockHash,
		GasData: &proto.MultiGasData{
			Computation:   bmg.Get(ResourceKindComputation),
			StorageAccess: bmg.Get(ResourceKindStorageAccess),
			StorageGrowth: bmg.Get(ResourceKindStorageGrowth),
			HistoryGrowth: bmg.Get(ResourceKindHistoryGrowth),
			Unknown:       bmg.Get(ResourceKindUnknown),
			TotalGas:      bmg.total,
			Refund:        bmg.refund,
		},
	}
}

// NewCollector creates and starts a new multi-gas data collector.
//
// The collector will:
// 1. Validate the provided configuration
// 2. Create the output directory if it doesn't exist
// 3. Start a background goroutine to process incoming data
// 4. Return immediately, ready to receive data on the input channel
//
// Parameters:
//   - config: Configuration specifying output directory and batch size
//   - input: Channel for receiving BlockMultiGas data (collector takes ownership)
//
// Returns:
//   - *Collector: The initialized collector ready to receive data
//   - error: Configuration validation or initialization error
//
// The caller should close the input channel when done sending data, then call
// Wait() to ensure all data has been written to disk.
func NewCollector(config Config, input <-chan *BlockMultiGas) (*Collector, error) {
	if config.OutputDir == "" {
		return nil, ErrOutputDirRequired
	}

	if config.BatchSize == 0 {
		return nil, ErrBatchSizeRequired
	}

	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return nil, ErrCreateOutputDir
	}

	c := &Collector{
		config: config,
		input:  input,
		buffer: make([]*proto.BlockMultiGasData, 0, config.BatchSize),
	}

	// Start processing data in a separate goroutine
	c.wg.Add(1)
	go c.processData()

	return c, nil
}

// processData is the main processing loop that runs in a background goroutine.
// It continuously reads BlockMultiGas data from the input channel, converts it
// to protobuf format, buffers it, and writes batches to disk when the buffer
// fills up or when the channel is closed.
func (c *Collector) processData() {
	defer c.wg.Done()

	for multiGas := range c.input {
		protoData := multiGas.ToProto()

		c.mu.Lock()
		c.buffer = append(c.buffer, protoData)

		if uint64(len(c.buffer)) >= c.config.BatchSize {
			if err := c.flushBatch(); err != nil {
				log.Error("Failed to flush batch", "error", err)
			}
		}
		c.mu.Unlock()
	}

	// Channel closed, flush remaining data
	c.mu.Lock()
	if len(c.buffer) > 0 {
		if err := c.flushBatch(); err != nil {
			log.Error("Failed to flush final batch", "error", err)
		}
	}
	c.mu.Unlock()
}

// flushBatch writes the current buffer contents to disk as a protobuf batch file.
// This method:
// 1. Creates a BlockMultiGasBatch protobuf message with current buffer data
// 2. Serializes the batch to binary protobuf format
// 3. Writes the data to a uniquely named file
// 4. Clears the buffer and increments the batch counter
//
// File naming pattern: multigas_batch_<batch_number>_<timestamp>.pb
func (c *Collector) flushBatch() error {
	if len(c.buffer) == 0 {
		return nil
	}

	batch := &proto.BlockMultiGasBatch{
		BatchTimestamp: uint64(time.Now().Unix()),
		Data:           make([]*proto.BlockMultiGasData, len(c.buffer)),
	}
	copy(batch.Data, c.buffer)

	data, err := protobuf.Marshal(batch)
	if err != nil {
		return fmt.Errorf("%s: %w", ErrMarshalBatch, err)
	}

	filename := fmt.Sprintf(batchFilenameFormat, c.batchNum, time.Now().Unix())
	filepath := filepath.Join(c.config.OutputDir, filename)

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("%s: %w", ErrWriteBatchFile, err)
	}

	log.Info("Wrote multi-gas batch",
		"file", filename,
		"count", len(c.buffer),
		"size_bytes", len(data))

	c.buffer = c.buffer[:0]
	c.batchNum++

	return nil
}

// Wait blocks until the collector has finished processing all data and shut down.
// This method should be called after closing the input channel to ensure all
// data has been written to disk before the program exits.
//
// Usage pattern:
//
//	close(input)       // Signal no more data
//	collector.Wait()   // Wait for shutdown
//
// This method is safe to call multiple times and will return immediately if
// the collector has already stopped.
func (c *Collector) Wait() {
	c.wg.Wait()
	log.Info("Multi-gas collector stopped")
}

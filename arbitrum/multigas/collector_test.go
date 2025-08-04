package multigas

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/arbitrum/multigas/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protobuf "google.golang.org/protobuf/proto"
)

func TestBlockMultiGasToProto(t *testing.T) {
	// Create test data
	blockMultiGas := &BlockMultiGas{
		MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
			ResourceKindComputation:   100,
			ResourceKindHistoryGrowth: 50,
			ResourceKindStorageAccess: 200,
			ResourceKindStorageGrowth: 1000,
			ResourceKindUnknown:       10,
		}),
		BlockNumber: 12345,
		BlockHash:   "0xabcdef123456",
	}

	// Convert to protobuf
	protoData := blockMultiGas.ToProto()

	// Verify block metadata
	assert.Equal(t, uint64(12345), protoData.BlockNumber)
	assert.Equal(t, "0xabcdef123456", protoData.BlockHash)

	// Verify gas data
	assert.Equal(t, uint64(100), protoData.GasData.Computation)
	assert.Equal(t, uint64(50), protoData.GasData.HistoryGrowth)
	assert.Equal(t, uint64(200), protoData.GasData.StorageAccess)
	assert.Equal(t, uint64(1000), protoData.GasData.StorageGrowth)
	assert.Equal(t, uint64(10), protoData.GasData.Unknown)
	assert.Equal(t, uint64(1360), protoData.GasData.TotalGas) // Sum: 100+50+200+1000+10
	assert.Equal(t, uint64(0), protoData.GasData.Refund)
}

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		expectErr error
	}{
		{
			name: "valid config",
			config: Config{
				OutputDir: t.TempDir(),
				BatchSize: 10,
			},
			expectErr: nil,
		},
		{
			name: "empty output directory",
			config: Config{
				OutputDir: "",
				BatchSize: 10,
			},
			expectErr: ErrOutputDirRequired,
		},
		{
			name: "zero batch size",
			config: Config{
				OutputDir: t.TempDir(),
				BatchSize: 0,
			},
			expectErr: ErrBatchSizeRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := make(chan *BlockMultiGas)

			collector, err := NewCollector(tt.config, input)

			if tt.expectErr != nil {
				assert.Error(t, err)
				assert.Equal(t, tt.expectErr, err)
				assert.Nil(t, collector)
			} else {
				require.NoError(t, err)
				require.NotNil(t, collector)
				assert.Equal(t, tt.config.OutputDir, collector.config.OutputDir)
				assert.Equal(t, tt.config.BatchSize, collector.config.BatchSize)

				close(input)
				collector.Wait()
			}
		})
	}
}

func TestDataCollection(t *testing.T) {
	tests := []struct {
		name        string
		batchSize   uint64
		inputData   []*BlockMultiGas
		expectFiles int
	}{
		{name: "empty input", batchSize: 10, inputData: nil, expectFiles: 0},
		{
			name:      "single data",
			batchSize: 1,
			inputData: []*BlockMultiGas{
				{
					MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
						ResourceKindComputation:   100,
						ResourceKindHistoryGrowth: 50,
						ResourceKindStorageAccess: 200,
						ResourceKindStorageGrowth: 1000,
					}),
					BlockNumber: 12345,
					BlockHash:   "0xabcdef123456",
				},
			},
			expectFiles: 1,
		},
		{
			name:      "multiple data, single batch",
			batchSize: 3,
			inputData: []*BlockMultiGas{
				{
					MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
						ResourceKindComputation:   100,
						ResourceKindHistoryGrowth: 50,
						ResourceKindStorageAccess: 200,
						ResourceKindStorageGrowth: 1000,
					}),
					BlockNumber: 12345,
					BlockHash:   "0xabcdef123456",
				},
				{
					MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
						ResourceKindUnknown: 10,
					}),
					BlockNumber: 12346,
					BlockHash:   "0x123def456789",
				},
			},
			expectFiles: 1,
		},
		{
			name:      "multiple data, multiple batches",
			batchSize: 3,
			inputData: []*BlockMultiGas{
				{
					MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
						ResourceKindComputation:   100,
						ResourceKindHistoryGrowth: 50,
						ResourceKindStorageAccess: 200,
						ResourceKindStorageGrowth: 1000,
					}),
					BlockNumber: 12345,
					BlockHash:   "0xabcdef123456",
				},
				{
					MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
						ResourceKindComputation:   200,
						ResourceKindHistoryGrowth: 100,
						ResourceKindStorageAccess: 300,
						ResourceKindStorageGrowth: 1500,
					}),
					BlockNumber: 12346,
					BlockHash:   "0x123def456789",
				},
				{
					MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
						ResourceKindComputation:   300,
						ResourceKindHistoryGrowth: 150,
						ResourceKindStorageAccess: 400,
						ResourceKindStorageGrowth: 2000,
					}),
					BlockNumber: 12347,
					BlockHash:   "0x789abc012345",
				},
				{
					MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
						ResourceKindComputation:   400,
						ResourceKindHistoryGrowth: 200,
						ResourceKindStorageAccess: 500,
						ResourceKindStorageGrowth: 2500,
					}),
					BlockNumber: 12348,
					BlockHash:   "0xdef456789abc",
				},
				{
					MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
						ResourceKindComputation:   500,
						ResourceKindHistoryGrowth: 100,
						ResourceKindStorageAccess: 200,
					}),
					BlockNumber: 12349,
					BlockHash:   "0x456789abcdef",
				},
			},
			expectFiles: 2, // 3 + 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			input := make(chan *BlockMultiGas, 10)

			config := Config{
				OutputDir: tmpDir,
				BatchSize: tt.batchSize,
			}

			collector, err := NewCollector(config, input)
			require.NoError(t, err)

			// Send all input data
			for _, multiGas := range tt.inputData {
				input <- multiGas
			}

			// Close input and wait for completion
			close(input)
			collector.Wait()

			// Verify file count
			files, err := filepath.Glob(filepath.Join(tmpDir, "multigas_batch_*.pb"))
			require.NoError(t, err)
			assert.Len(t, files, tt.expectFiles)

			// Read all batch data from files
			var allData []*proto.BlockMultiGasData
			for _, file := range files {
				data, err := os.ReadFile(file)
				require.NoError(t, err)

				var batch proto.BlockMultiGasBatch
				err = protobuf.Unmarshal(data, &batch)
				require.NoError(t, err)

				allData = append(allData, batch.Data...)
			}

			// Verify all data was written correctly
			assert.Len(t, allData, len(tt.inputData))

			for i, data := range allData {
				expected := tt.inputData[i]

				// Verify block metadata
				assert.Equal(t, expected.BlockNumber, data.BlockNumber)
				assert.Equal(t, expected.BlockHash, data.BlockHash)

				// Verify gas data
				assert.Equal(t, expected.gas[ResourceKindComputation], data.GasData.Computation)
				assert.Equal(t, expected.gas[ResourceKindHistoryGrowth], data.GasData.HistoryGrowth)
				assert.Equal(t, expected.gas[ResourceKindStorageAccess], data.GasData.StorageAccess)
				assert.Equal(t, expected.gas[ResourceKindStorageGrowth], data.GasData.StorageGrowth)
				assert.Equal(t, expected.gas[ResourceKindUnknown], data.GasData.Unknown)
				assert.Equal(t, expected.total, data.GasData.TotalGas)
				assert.Equal(t, expected.refund, data.GasData.Refund)
			}
		})
	}
}

func TestCollectorChannelClosed(t *testing.T) {
	tmpDir := t.TempDir()
	input := make(chan *BlockMultiGas, 10)

	config := Config{
		OutputDir: tmpDir,
		BatchSize: 10,
	}

	collector, err := NewCollector(config, input)
	require.NoError(t, err)

	// Add some data
	multiGas := &BlockMultiGas{
		MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
			ResourceKindComputation:   100,
			ResourceKindHistoryGrowth: 50,
			ResourceKindStorageAccess: 200,
			ResourceKindStorageGrowth: 1000,
		}),
		BlockNumber: 12345,
		BlockHash:   "0xabcdef123456",
	}

	input <- multiGas

	// Close input channel - should flush remaining data
	close(input)

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	// Verify data was flushed
	files, err := filepath.Glob(filepath.Join(tmpDir, "multigas_batch_*.pb"))
	require.NoError(t, err)
	assert.Len(t, files, 1)

	collector.Wait()
}

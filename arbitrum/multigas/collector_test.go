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

func TestBlockTransactionsMultiGasToProto(t *testing.T) {
	blockTxsMultiGas := &BlockTransactionsMultiGas{
		BlockNumber:    12345,
		BlockHash:      []byte{0xab, 0xcd, 0xef},
		BlockTimestamp: 1234567890,
		Transactions: []TransactionMultiGas{
			{
				TxHash:  []byte{0x12, 0x34, 0x56},
				TxIndex: 0,
				MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
					ResourceKindComputation:   100,
					ResourceKindHistoryGrowth: 50,
					ResourceKindStorageAccess: 200,
					ResourceKindStorageGrowth: 1000,
					ResourceKindUnknown:       10,
				}),
			},
			{
				TxHash:  []byte{0x78, 0x9a, 0xbc},
				TxIndex: 1,
				MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
					ResourceKindComputation: 150,
				}),
			},
		},
	}

	protoData := blockTxsMultiGas.ToProto()

	// Verify block metadata
	assert.Equal(t, blockTxsMultiGas.BlockNumber, protoData.BlockNumber)
	assert.Equal(t, blockTxsMultiGas.BlockHash, protoData.BlockHash)
	assert.Equal(t, blockTxsMultiGas.BlockTimestamp, protoData.BlockTimestamp)
	assert.Len(t, protoData.Transactions, len(blockTxsMultiGas.Transactions))

	// Verify first transaction
	expectedTx1 := blockTxsMultiGas.Transactions[0]
	tx1 := protoData.Transactions[0]
	assert.Equal(t, expectedTx1.TxHash, tx1.TxHash)
	assert.Equal(t, expectedTx1.TxIndex, tx1.TxIndex)
	assert.Equal(t, expectedTx1.MultiGas.Get(ResourceKindComputation), tx1.MultiGas.Computation)
	assert.Equal(t, expectedTx1.MultiGas.Get(ResourceKindHistoryGrowth), tx1.MultiGas.HistoryGrowth)
	assert.Equal(t, expectedTx1.MultiGas.Get(ResourceKindStorageAccess), tx1.MultiGas.StorageAccess)
	assert.Equal(t, expectedTx1.MultiGas.Get(ResourceKindStorageGrowth), tx1.MultiGas.StorageGrowth)
	assert.NotNil(t, tx1.MultiGas.Unknown)
	assert.Equal(t, expectedTx1.MultiGas.Get(ResourceKindUnknown), *tx1.MultiGas.Unknown)

	// Verify second transaction (no unknown or refund)
	expectedTx2 := blockTxsMultiGas.Transactions[1]
	tx2 := protoData.Transactions[1]
	assert.Equal(t, expectedTx2.TxHash, tx2.TxHash)
	assert.Equal(t, expectedTx2.TxIndex, tx2.TxIndex)
	assert.Equal(t, expectedTx2.MultiGas.Get(ResourceKindComputation), tx2.MultiGas.Computation)
	assert.Nil(t, tx2.MultiGas.Unknown)
	assert.Nil(t, tx2.MultiGas.Refund)
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
			input := make(chan *BlockTransactionsMultiGas)

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
		inputData   []*BlockTransactionsMultiGas
		expectFiles int
	}{
		{name: "empty input", batchSize: 10, inputData: nil, expectFiles: 0},
		{
			name:      "single block with transactions",
			batchSize: 1,
			inputData: []*BlockTransactionsMultiGas{
				{
					BlockNumber:    12345,
					BlockHash:      []byte{0xab, 0xcd, 0xef},
					BlockTimestamp: 1234567890,
					Transactions: []TransactionMultiGas{
						{
							TxHash:  []byte{0x12, 0x34, 0x56},
							TxIndex: 0,
							MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
								ResourceKindComputation:   100,
								ResourceKindHistoryGrowth: 50,
								ResourceKindStorageAccess: 200,
								ResourceKindStorageGrowth: 1000,
								ResourceKindUnknown:       25,
							}),
						},
						{
							TxHash:  []byte{0x78, 0x9a, 0xbc},
							TxIndex: 1,
							MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
								ResourceKindComputation:   75,
								ResourceKindHistoryGrowth: 30,
								ResourceKindStorageAccess: 150,
								ResourceKindUnknown:       10,
							}),
						},
					},
				},
			},
			expectFiles: 1,
		},
		{
			name:      "multiple blocks, single batch",
			batchSize: 3,
			inputData: []*BlockTransactionsMultiGas{
				{
					BlockNumber:    12345,
					BlockHash:      []byte{0xab, 0xcd, 0xef},
					BlockTimestamp: 1234567890,
					Transactions: []TransactionMultiGas{
						{
							TxHash:  []byte{0x12, 0x34, 0x56},
							TxIndex: 0,
							MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
								ResourceKindComputation:   100,
								ResourceKindHistoryGrowth: 25,
								ResourceKindStorageAccess: 50,
							}),
						},
					},
				},
				{
					BlockNumber:    12346,
					BlockHash:      []byte{0xde, 0xf1, 0x23},
					BlockTimestamp: 1234567891,
					Transactions: []TransactionMultiGas{
						{
							TxHash:  []byte{0x45, 0x67, 0x89},
							TxIndex: 0,
							MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
								ResourceKindComputation:   200,
								ResourceKindStorageGrowth: 500,
								ResourceKindUnknown:       15,
							}),
						},
					},
				},
			},
			expectFiles: 1,
		},
		{
			name:      "multiple blocks, multiple batches",
			batchSize: 2,
			inputData: []*BlockTransactionsMultiGas{
				{
					BlockNumber:    12345,
					BlockHash:      []byte{0xab, 0xcd, 0xef},
					BlockTimestamp: 1234567890,
					Transactions: []TransactionMultiGas{
						{
							TxHash:  []byte{0x12, 0x34, 0x56},
							TxIndex: 0,
							MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
								ResourceKindComputation:   100,
								ResourceKindHistoryGrowth: 40,
								ResourceKindStorageAccess: 80,
								ResourceKindStorageGrowth: 300,
							}),
						},
					},
				},
				{
					BlockNumber:    12346,
					BlockHash:      []byte{0xde, 0xf1, 0x23},
					BlockTimestamp: 1234567891,
					Transactions: []TransactionMultiGas{
						{
							TxHash:  []byte{0x78, 0x9a, 0xbc},
							TxIndex: 0,
							MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
								ResourceKindComputation:   200,
								ResourceKindHistoryGrowth: 60,
								ResourceKindStorageAccess: 120,
								ResourceKindUnknown:       20,
							}),
						},
					},
				},
				{
					BlockNumber:    12347,
					BlockHash:      []byte{0x45, 0x67, 0x89},
					BlockTimestamp: 1234567892,
					Transactions: []TransactionMultiGas{
						{
							TxHash:  []byte{0xab, 0xcd, 0xef},
							TxIndex: 0,
							MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
								ResourceKindComputation:   300,
								ResourceKindStorageGrowth: 800,
								ResourceKindUnknown:       35,
							}),
						},
					},
				},
			},
			expectFiles: 2, // 2 + 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			input := make(chan *BlockTransactionsMultiGas, 10)

			config := Config{
				OutputDir: tmpDir,
				BatchSize: tt.batchSize,
			}

			collector, err := NewCollector(config, input)
			require.NoError(t, err)

			// Send all input data
			for _, blockData := range tt.inputData {
				input <- blockData
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
				assert.Equal(t, expected.BlockTimestamp, data.BlockTimestamp)
				assert.Len(t, data.Transactions, len(expected.Transactions))

				// Verify each transaction
				for j, txData := range data.Transactions {
					expectedTx := expected.Transactions[j]
					assert.Equal(t, expectedTx.TxHash, txData.TxHash)
					assert.Equal(t, expectedTx.TxIndex, txData.TxIndex)
					assert.Equal(t, expectedTx.MultiGas.Get(ResourceKindComputation), txData.MultiGas.Computation)
					assert.Equal(t, expectedTx.MultiGas.Get(ResourceKindHistoryGrowth), txData.MultiGas.HistoryGrowth)
					assert.Equal(t, expectedTx.MultiGas.Get(ResourceKindStorageAccess), txData.MultiGas.StorageAccess)
					assert.Equal(t, expectedTx.MultiGas.Get(ResourceKindStorageGrowth), txData.MultiGas.StorageGrowth)

					// Verify optional fields
					if unknown := expectedTx.MultiGas.Get(ResourceKindUnknown); unknown > 0 {
						assert.NotNil(t, txData.MultiGas.Unknown)
						assert.Equal(t, unknown, *txData.MultiGas.Unknown)
					} else {
						assert.Nil(t, txData.MultiGas.Unknown)
					}

					if refund := expectedTx.MultiGas.GetRefund(); refund > 0 {
						assert.NotNil(t, txData.MultiGas.Refund)
						assert.Equal(t, refund, *txData.MultiGas.Refund)
					} else {
						assert.Nil(t, txData.MultiGas.Refund)
					}
				}
			}
		})
	}
}

func TestCollectorChannelClosed(t *testing.T) {
	tmpDir := t.TempDir()
	input := make(chan *BlockTransactionsMultiGas, 10)

	config := Config{
		OutputDir: tmpDir,
		BatchSize: 10,
	}

	collector, err := NewCollector(config, input)
	require.NoError(t, err)

	// Add some data
	blockData := &BlockTransactionsMultiGas{
		BlockNumber:    12345,
		BlockHash:      []byte{0xab, 0xcd, 0xef},
		BlockTimestamp: 1234567890,
		Transactions: []TransactionMultiGas{
			{
				TxHash:  []byte{0x12, 0x34, 0x56},
				TxIndex: 0,
				MultiGas: *MultiGasFromMap(map[ResourceKind]uint64{
					ResourceKindComputation: 100,
				}),
			},
		},
	}

	input <- blockData

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

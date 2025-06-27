package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/params"
)

func TestAsyncFileWriting(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "async_file_writing_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test configuration
	config := txGasDimensionByOpcodeLiveTraceConfig{
		Path:        tempDir,
		ChainConfig: &params.ChainConfig{},
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Create the tracer
	_, err = NewTxGasDimensionByOpcodeLogger(configBytes)
	if err != nil {
		t.Fatalf("Failed to create tracer: %v", err)
	}

	// Create a mock tracer instance for testing
	ctx, cancel := context.WithCancel(context.Background())
	tracer := &TxGasDimensionByOpcodeLiveTracer{
		Path:           tempDir,
		ChainConfig:    &params.ChainConfig{},
		writeQueue:     make(chan fileWriteTask, 1000),
		ctx:            ctx,
		cancel:         cancel,
		writeQueueSize: 1000,
	}
	tracer.startFileWorkers()

	// Test async file writing
	t.Run("TestAsyncFileWrite", func(t *testing.T) {
		// Queue multiple file write tasks
		for i := 0; i < 10; i++ {
			testData := []byte(fmt.Sprintf("test data %d", i))
			testPath := filepath.Join(tempDir, fmt.Sprintf("test_%d.txt", i))
			tracer.queueFileWrite(testPath, testData, "test")
		}

		// Wait a bit for async processing
		time.Sleep(100 * time.Millisecond)

		// Check that files were written
		for i := 0; i < 10; i++ {
			testPath := filepath.Join(tempDir, fmt.Sprintf("test_%d.txt", i))
			if _, err := os.Stat(testPath); os.IsNotExist(err) {
				t.Errorf("File was not written: %s", testPath)
			}
		}
	})

	// Test graceful shutdown
	t.Run("TestGracefulShutdown", func(t *testing.T) {
		tracer.Close()
		// Should not panic or hang
	})
}

func TestFileWriteQueueOverflow(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "queue_overflow_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test configuration
	config := txGasDimensionByOpcodeLiveTraceConfig{
		Path:        tempDir,
		ChainConfig: &params.ChainConfig{},
	}

	configBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Create the tracer
	_, err = NewTxGasDimensionByOpcodeLogger(configBytes)
	if err != nil {
		t.Fatalf("Failed to create tracer: %v", err)
	}

	// Create a mock tracer instance for testing
	ctx, cancel := context.WithCancel(context.Background())
	tracer := &TxGasDimensionByOpcodeLiveTracer{
		Path:           tempDir,
		ChainConfig:    &params.ChainConfig{},
		writeQueue:     make(chan fileWriteTask, 1000),
		ctx:            ctx,
		cancel:         cancel,
		writeQueueSize: 1000,
	}
	tracer.startFileWorkers()

	// Test queue overflow handling
	t.Run("TestQueueOverflow", func(t *testing.T) {
		// Try to queue more tasks than the buffer can hold
		// This should not block and should log warnings for dropped tasks
		for i := 0; i < 2000; i++ {
			testData := []byte(fmt.Sprintf("overflow test data %d", i))
			testPath := filepath.Join(tempDir, fmt.Sprintf("overflow_%d.txt", i))
			tracer.queueFileWrite(testPath, testData, "overflow_test")
		}

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		// Should not have crashed or hung
		t.Log("Queue overflow test completed without hanging")
	})

	// Clean shutdown
	tracer.Close()
}

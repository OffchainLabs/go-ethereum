// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbitrum

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
)

// mockNodeReader is a test helper that returns a fixed blob for a given hash.
type mockNodeReader struct {
	nodes map[common.Hash][]byte
}

func (m *mockNodeReader) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	blob, ok := m.nodes[hash]
	if !ok {
		return nil, fmt.Errorf("missing node %v", hash)
	}
	return blob, nil
}

func TestRecordingNodeReaderCapturesPreimages(t *testing.T) {
	// Create a RecordingTrieDB that wraps a real triedb.
	// Since we can't easily mock a full triedb, we test the recording node reader directly.
	collector := &RecordingTrieDB{
		preimages: make(map[common.Hash][]byte),
	}

	hash1 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	hash2 := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	blob1 := []byte("blob1")
	blob2 := []byte("blob2")

	inner := &mockNodeReader{
		nodes: map[common.Hash][]byte{
			hash1: blob1,
			hash2: blob2,
		},
	}

	reader := &recordingNodeReader{
		inner:     inner,
		collector: collector,
	}

	// Read node1
	gotBlob, err := reader.Node(common.Hash{}, []byte("path1"), hash1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(gotBlob) != string(blob1) {
		t.Fatalf("expected blob1, got %s", gotBlob)
	}

	// Read node2
	gotBlob, err = reader.Node(common.Hash{}, []byte("path2"), hash2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(gotBlob) != string(blob2) {
		t.Fatalf("expected blob2, got %s", gotBlob)
	}

	// Read node1 again — should still work
	gotBlob, err = reader.Node(common.Hash{}, []byte("path1"), hash1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(gotBlob) != string(blob1) {
		t.Fatalf("expected blob1, got %s", gotBlob)
	}

	// Check that preimages were captured
	preimages := collector.Preimages()
	if len(preimages) != 2 {
		t.Fatalf("expected 2 preimages, got %d", len(preimages))
	}
	if string(preimages[hash1]) != string(blob1) {
		t.Fatalf("preimage mismatch for hash1")
	}
	if string(preimages[hash2]) != string(blob2) {
		t.Fatalf("preimage mismatch for hash2")
	}
}

func TestRecordingNodeReaderErrorDoesNotRecord(t *testing.T) {
	collector := &RecordingTrieDB{
		preimages: make(map[common.Hash][]byte),
	}

	inner := &mockNodeReader{
		nodes: map[common.Hash][]byte{},
	}

	reader := &recordingNodeReader{
		inner:     inner,
		collector: collector,
	}

	hash := common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")
	_, err := reader.Node(common.Hash{}, []byte("path"), hash)
	if err == nil {
		t.Fatal("expected error for missing node")
	}

	// Should not have recorded anything
	preimages := collector.Preimages()
	if len(preimages) != 0 {
		t.Fatalf("expected 0 preimages, got %d", len(preimages))
	}
}

func TestRecordingTrieDBNodeReader(t *testing.T) {
	// Create a real triedb with hashdb backend for testing
	db := triedb.NewDatabase(nil, &triedb.Config{HashDB: &hashdb.Config{}})

	recordingDB := NewRecordingTrieDB(db)

	// The NodeReader method should return a recording wrapper
	// We can't easily test Node reads against a real triedb without seeding it with data,
	// but we can verify the structure is correct
	if recordingDB.Preimages() == nil {
		t.Fatal("preimages map should not be nil")
	}
	if len(recordingDB.Preimages()) != 0 {
		t.Fatal("preimages map should be empty initially")
	}
}

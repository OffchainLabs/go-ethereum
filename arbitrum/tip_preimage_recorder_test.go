// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbitrum

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie/bintrie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/database"
)

type staticNodeReader struct {
	blob []byte
}

func (r *staticNodeReader) Node(common.Hash, []byte, common.Hash) ([]byte, error) {
	return r.blob, nil
}

func TestRecordingNodeReaderRecordsPreimagesUntilStopped(t *testing.T) {
	disk := rawdb.NewMemoryDatabase()
	t.Cleanup(func() { disk.Close() })
	triedb := triedb.NewDatabase(disk, triedb.HashDefaults)
	t.Cleanup(func() { triedb.Close() })
	recordingDB := NewRecordingTrieDB(triedb)

	blob, err := rlp.EncodeToBytes([][]byte{{0x20}, []byte("value")})
	if err != nil {
		t.Fatal(err)
	}
	hash := crypto.Keccak256Hash(blob)
	reader := &recordingNodeReader{
		inner:           &staticNodeReader{blob: blob},
		recordingTrieDB: recordingDB,
	}
	got, err := reader.Node(common.Hash{}, nil, hash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) || !bytes.Equal(recordingDB.Preimages()[hash], blob) {
		t.Fatal("node preimage was not returned and recorded")
	}

	recordingDB.StopRecording()
	stoppedBlob := []byte("after-stop")
	stoppedHash := crypto.Keccak256Hash(stoppedBlob)
	reader.inner = &staticNodeReader{blob: stoppedBlob}
	if _, err := reader.Node(common.Hash{}, nil, stoppedHash); err != nil {
		t.Fatal(err)
	}
	if _, ok := recordingDB.Preimages()[stoppedHash]; ok {
		t.Fatal("node preimage was recorded after StopRecording")
	}
}

func TestRecordingReaderRecordsCodeAccessUntilStopped(t *testing.T) {
	db := state.NewDatabaseForTesting()
	codes := [][]byte{{0x01}, {0x02, 0x03}, {0x04, 0x05, 0x06}, {0x07}}
	hashes := make([]common.Hash, len(codes))
	batch := db.CodeDB().NewBatchWithSize(len(codes))
	for i, code := range codes {
		hashes[i] = crypto.Keccak256Hash(code)
		batch.Put(hashes[i], code)
	}
	if err := batch.Commit(); err != nil {
		t.Fatal(err)
	}

	recordingDB := NewTipRecordingStateDatabase(db)
	reader, err := recordingDB.Reader(types.EmptyRootHash)
	if err != nil {
		t.Fatal(err)
	}
	if !reader.Has(common.Address{}, hashes[0]) {
		t.Fatal("expected code to exist")
	}
	if got := reader.Code(common.Address{}, hashes[1]); !bytes.Equal(got, codes[1]) {
		t.Fatalf("unexpected code: %x", got)
	}
	if got := reader.CodeSize(common.Address{}, hashes[2]); got != len(codes[2]) {
		t.Fatalf("unexpected code size: %d", got)
	}
	assertRecordedCodeHashes(t, recordingDB.CodeHashes(), hashes[:3]...)

	recordingDB.StopRecording()
	if got := reader.Code(common.Address{}, hashes[3]); !bytes.Equal(got, codes[3]) {
		t.Fatalf("unexpected code after stop: %x", got)
	}
	assertRecordedCodeHashes(t, recordingDB.CodeHashes(), hashes[:3]...)
}

func assertRecordedCodeHashes(t *testing.T, got []common.Hash, want ...common.Hash) {
	t.Helper()
	gotSet := make(map[common.Hash]struct{}, len(got))
	for _, hash := range got {
		gotSet[hash] = struct{}{}
	}
	if len(gotSet) != len(want) {
		t.Fatalf("unexpected recorded code hash count: got %d want %d", len(gotSet), len(want))
	}
	for _, hash := range want {
		if _, ok := gotSet[hash]; !ok {
			t.Fatalf("code hash %s was not recorded", hash)
		}
	}
}

func TestTipRecordingStateDatabaseOpensBinaryTrie(t *testing.T) {
	disk := rawdb.NewMemoryDatabase()
	t.Cleanup(func() { disk.Close() })
	triedb := triedb.NewDatabase(disk, triedb.UBTDefaults)
	t.Cleanup(func() { triedb.Close() })
	recordingDB := NewTipRecordingStateDatabase(state.NewDatabase(triedb, nil))

	tr, err := recordingDB.OpenTrie(types.EmptyRootHash)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.(*bintrie.BinaryTrie); !ok {
		t.Fatalf("unexpected trie type %T", tr)
	}
	storage, err := recordingDB.OpenStorageTrie(types.EmptyRootHash, common.Address{}, types.EmptyRootHash, tr)
	if err != nil {
		t.Fatal(err)
	}
	if storage != tr {
		t.Fatal("binary storage trie did not reuse the account trie")
	}
}

var _ database.NodeReader = (*staticNodeReader)(nil)

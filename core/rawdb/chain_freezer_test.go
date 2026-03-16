// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package rawdb

// NOTE: Do not use t.Parallel() in freezer tests — each test relies on
// deterministic freeze-trigger ordering and timing-sensitive assertions
// that can become flaky under concurrent execution.

import (
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
)

// writeBlock writes a minimal block with header, body, receipts, and header
// number mapping. If canonical is true, it also writes the canonical hash.
// An optional parentHash can be provided; if non-zero, it is set on the header.
func writeBlock(db ethdb.KeyValueStore, number uint64, extra []byte, canonical bool, parentHash ...common.Hash) common.Hash {
	header := &types.Header{
		Number: new(big.Int).SetUint64(number),
		Extra:  extra,
	}
	if len(parentHash) > 0 && parentHash[0] != (common.Hash{}) {
		header.ParentHash = parentHash[0]
	}
	hash := header.Hash()

	WriteHeader(db, header)
	WriteBody(db, hash, number, &types.Body{})
	WriteReceipts(db, hash, number, nil)
	WriteHeaderNumber(db, hash, number)
	if canonical {
		WriteCanonicalHash(db, hash, number)
	}
	return hash
}

// writeTestBlock writes a canonical block with all data required by the chain freezer.
func writeTestBlock(db ethdb.KeyValueStore, number uint64) common.Hash {
	return writeBlock(db, number, []byte("test"), true)
}

// setupTestChain creates a test chain in the key-value database with blocks
// from 0 to count-1 and sets the head block hash.
func setupTestChain(db ethdb.KeyValueStore, count uint64) {
	extendTestChain(db, 0, count)
}

// writeSideChainBlock writes a non-canonical block at the given number with a
// distinct hash (via different Extra data).
func writeSideChainBlock(db ethdb.KeyValueStore, number uint64, nonce int) common.Hash {
	return writeBlock(db, number, []byte(fmt.Sprintf("side-%d", nonce)), false)
}

// writeBlockWithParent writes a non-canonical block at the given number with
// the specified parent hash. Used to create dangling child blocks in tests.
func writeBlockWithParent(db ethdb.KeyValueStore, number uint64, parentHash common.Hash, extra []byte) common.Hash {
	return writeBlock(db, number, extra, false, parentHash)
}

// blockExistsInKV checks whether a block's canonical hash mapping is present
// in the key-value database by checking for the headerHashKey directly,
// bypassing the ancient store lookup.
func blockExistsInKV(t *testing.T, db ethdb.KeyValueReader, number uint64) bool {
	t.Helper()
	has, err := db.Has(headerHashKey(number))
	if err != nil {
		t.Fatalf("unexpected error checking block %d existence: %v", number, err)
	}
	return has
}

// setTestMargin sets the cleanupMargin on the chainFreezer embedded in db.
func setTestMargin(t *testing.T, db ethdb.Database, margin uint64) {
	t.Helper()
	db.(*freezerdb).chainFreezer.cleanupMargin = margin
}

// freezeTestDB triggers a single deterministic freeze cycle on db.
func freezeTestDB(t *testing.T, db ethdb.Database) {
	t.Helper()
	if err := db.(interface{ Freeze() error }).Freeze(); err != nil {
		t.Fatal(err)
	}
}

// openTestFreezerDB creates a test database with a freezer backed by a
// temporary directory. It sets the cleanup margin on the chainFreezer and
// returns the database and a freeze function.
func openTestFreezerDB(t *testing.T, margin uint64) (ethdb.Database, func()) {
	t.Helper()
	ancientDir := t.TempDir()
	db, err := Open(NewMemoryDatabase(), OpenOptions{Ancient: ancientDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	setTestMargin(t, db, margin)
	freezeDB := db.(interface{ Freeze() error })
	return db, func() {
		if err := freezeDB.Freeze(); err != nil {
			t.Fatal(err)
		}
	}
}

// advanceCleanupPast extends the chain and freezes repeatedly until the
// cleanup tail exceeds target. Fails the test if the target is not reached
// after 15 iterations. Returns the updated next block number.
func advanceCleanupPast(t *testing.T, db ethdb.Database, freeze func(), margin, nextBlock, target uint64) uint64 {
	t.Helper()
	for i := 0; i < 15; i++ {
		extra := nextBlock + margin
		extendTestChain(db, nextBlock, extra)
		nextBlock = extra
		freeze()
		tail, ok := ReadFreezerCleanupTail(db)
		if !ok {
			t.Fatal("cleanup tail not set during advanceCleanupPast")
		}
		if tail > target {
			t.Logf("cleanup tail %d passed target %d after %d extra cycles", tail, target, i+1)
			return nextBlock
		}
	}
	t.Fatalf("cleanup tail did not advance past target %d after 15 iterations", target)
	return nextBlock // unreachable
}

// extendTestChain writes blocks [from, to) and updates the head hash.
func extendTestChain(db ethdb.KeyValueStore, from, to uint64) {
	var lastHash common.Hash
	for i := from; i < to; i++ {
		lastHash = writeTestBlock(db, i)
	}
	WriteHeadBlockHash(db, lastHash)
	WriteHeadHeaderHash(db, lastHash)
}

// TestFreezerCleanupMargin verifies that the chain freezer retains the most
// recent cleanupMargin frozen blocks in the key-value database before
// deleting older ones.
func TestFreezerCleanupMargin(t *testing.T) {
	margin := uint64(200)
	// Use a small margin for testing to avoid creating hundreds of thousands of blocks.
	// Phase 1: create enough blocks for first freeze + margin initialization.
	// Phase 2: add more blocks and freeze again to trigger actual cleanup.
	phase1Blocks := params.FullImmutabilityThreshold + margin + 100
	phase2Extra := margin // add enough to push old blocks past margin

	db, freeze := openTestFreezerDB(t, margin)

	// Phase 1: write blocks and trigger first freeze (initializes cleanup tail)
	setupTestChain(db, phase1Blocks)
	freeze()

	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if frozen == 0 {
		t.Fatal("expected some blocks to be frozen")
	}
	t.Logf("phase 1: frozen=%d, totalBlocks=%d, margin=%d", frozen, phase1Blocks, margin)

	// After first freeze, recently frozen blocks should still be in KV.
	recentFrozen := frozen - 1
	if !blockExistsInKV(t, db, recentFrozen) {
		t.Errorf("block %d was frozen but should still be in key-value store (within cleanup margin)", recentFrozen)
	}

	// Cleanup tail should be initialized to frozen - margin
	cleanupTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("freezer cleanup tail not persisted after first freeze")
	}
	if frozen > margin {
		expectedTail := frozen - margin
		if cleanupTail != expectedTail {
			t.Errorf("phase 1: expected cleanup tail %d (frozen %d - margin %d), got %d",
				expectedTail, frozen, margin, cleanupTail)
		}
	}
	t.Logf("phase 1: cleanupTail=%d", cleanupTail)

	// Phase 2: add more blocks (only the new ones) and freeze again. This
	// should trigger actual cleanup of old blocks that are now beyond the margin.
	totalBlocks := phase1Blocks + phase2Extra
	extendTestChain(db, phase1Blocks, totalBlocks)

	// May need multiple freeze cycles since freezerBatchLimit caps each one
	for i := 0; i < 5; i++ {
		freeze()
	}

	frozen2, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("phase 2: frozen=%d, totalBlocks=%d", frozen2, totalBlocks)

	// Verify cleanup actually advanced (fail early if setup is wrong)
	cleanupTail2, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("freezer cleanup tail not persisted after second freeze")
	}
	if cleanupTail2 <= cleanupTail {
		t.Fatalf("cleanup tail should have advanced after phase 2: was %d, now %d (frozen=%d, margin=%d)",
			cleanupTail, cleanupTail2, frozen2, margin)
	}
	t.Logf("phase 2: cleanupTail=%d (advanced by %d)", cleanupTail2, cleanupTail2-cleanupTail)

	// Blocks between the initial tail and the new tail should have been cleaned
	// from KV but still readable from the ancient store.
	for _, bn := range []uint64{cleanupTail, cleanupTail + 1, cleanupTail + 50} {
		if bn < cleanupTail2 {
			// blockExistsInKV checks for the canonical hash key in KV
			if blockExistsInKV(t, db, bn) {
				t.Errorf("block %d should have been cleaned up from key-value store (cleanup tail is %d)", bn, cleanupTail2)
			}
			// Verify the block is still readable from the ancient store.
			if hash, err := db.Ancient(ChainFreezerHashTable, bn); err != nil || len(hash) == 0 {
				t.Errorf("block %d should still be readable from ancient store after cleanup", bn)
			}
		}
	}

	// But recently frozen blocks should still be in KV
	if frozen2 > margin {
		recentBlock := frozen2 - 1
		if !blockExistsInKV(t, db, recentBlock) {
			t.Errorf("block %d was frozen but should still be in key-value store (within cleanup margin)", recentBlock)
		}
	}
}

// TestFreezerCleanupMarginRecovery verifies that after an unclean shutdown
// (simulated by truncating the freezer), blocks within the safety margin
// can still be read from LevelDB and re-frozen.
func TestFreezerCleanupMarginRecovery(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	// Freeze
	freeze()

	frozenBefore, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("frozen before truncation: %d", frozenBefore)

	if frozenBefore <= margin {
		t.Fatalf("expected frozen (%d) > margin (%d) given test parameters", frozenBefore, margin)
	}

	// Simulate unclean shutdown: truncate the freezer, losing recent entries.
	// This simulates what repair() does on restart: truncating unflushed
	// entries from the freezer head.
	truncateTarget := frozenBefore - 100
	if _, err := db.TruncateHead(truncateTarget); err != nil {
		t.Fatal(err)
	}

	frozenAfter, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("frozen after truncation: %d (lost %d blocks)", frozenAfter, frozenBefore-frozenAfter)

	// The blocks that were "lost" from the freezer should still be available
	// in LevelDB because they were within the cleanup margin.
	for number := frozenAfter; number < frozenBefore; number++ {
		if !blockExistsInKV(t, db, number) {
			t.Fatalf("block %d was lost from freezer and not in KV store — safety margin failed", number)
		}
	}
	t.Logf("all %d truncated blocks still available in LevelDB", frozenBefore-frozenAfter)

	// Re-freeze: trigger another cycle. The blocks should be re-frozen from LevelDB.
	freeze()

	frozenRecovered, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if frozenRecovered < frozenBefore {
		t.Errorf("expected to recover to at least %d frozen blocks, got %d", frozenBefore, frozenRecovered)
	}
	t.Logf("frozen after recovery: %d", frozenRecovered)
}

// TestFreezerCleanupTailPersistence verifies that the cleanup tail is persisted
// and correctly read back, so cleanup doesn't re-scan already-cleaned blocks.
func TestFreezerCleanupTailPersistence(t *testing.T) {
	db := NewMemoryDatabase()

	// Initially should not exist
	if _, ok := ReadFreezerCleanupTail(db); ok {
		t.Fatal("cleanup tail should not exist initially")
	}

	// Write and read back
	WriteFreezerCleanupTail(db, 12345)
	val, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should exist after write")
	}
	if val != 12345 {
		t.Fatalf("expected 12345, got %d", val)
	}

	// Overwrite
	WriteFreezerCleanupTail(db, 99999)
	val, ok = ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should exist after overwrite")
	}
	if val != 99999 {
		t.Fatalf("expected 99999, got %d", val)
	}
}

// TestFreezerGenesisPreserved verifies that block 0 (genesis) is never deleted
// from the key-value store during cleanup.
func TestFreezerGenesisPreserved(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 200

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	for i := 0; i < 5; i++ {
		freeze()
	}

	// Extend chain and freeze more to push cleanup well past genesis.
	totalBlocks2 := totalBlocks + margin
	extendTestChain(db, totalBlocks, totalBlocks2)
	for i := 0; i < 5; i++ {
		freeze()
	}

	cleanupTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should exist after multiple freeze cycles")
	}
	t.Logf("cleanupTail=%d", cleanupTail)

	if cleanupTail <= 1 {
		t.Fatalf("expected cleanup tail > 1 after multiple freeze cycles, got %d", cleanupTail)
	}

	// Genesis must still be in KV.
	if !blockExistsInKV(t, db, 0) {
		t.Fatal("genesis block (0) should never be deleted from key-value store")
	}
}

// TestFreezerSideChainCleanup verifies that non-canonical (side chain) blocks
// are cleaned up from LevelDB alongside canonical blocks when the cleanup
// range advances past them.
func TestFreezerSideChainCleanup(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	// Do the initial freeze to establish the cleanup tail. After this,
	// cleanupStart = frozen - margin. Side-chain blocks must be placed
	// ABOVE this value to fall within a future cleanup range.
	freeze()
	initTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail not set after first freeze")
	}
	t.Logf("initial cleanup tail: %d", initTail)

	// Write side-chain blocks above the initial cleanup tail so they will
	// be reached by future cleanup cycles.
	sideChainStart := initTail + 10
	sideChainEnd := initTail + 50
	sideHashes := make(map[uint64]common.Hash)
	for n := sideChainStart; n < sideChainEnd; n++ {
		sideHashes[n] = writeSideChainBlock(db, n, 1)
	}

	// Verify side-chain blocks are discoverable via ReadAllHashes.
	for n := sideChainStart; n < sideChainEnd; n++ {
		hashes := ReadAllHashes(db, n)
		if len(hashes) < 2 {
			t.Fatalf("block %d: expected at least 2 hashes (canonical + side), got %d", n, len(hashes))
		}
	}

	// Freeze additional blocks until the cleanup tail passes the side chain range.
	advanceCleanupPast(t, db, freeze, margin, totalBlocks, sideChainEnd)

	cleanupTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should exist after advancing cleanup")
	}

	// Side-chain blocks in the cleaned-up range should be gone from KV.
	for n := sideChainStart; n < sideChainEnd; n++ {
		if slices.Contains(ReadAllHashes(db, n), sideHashes[n]) {
			t.Errorf("block %d: side-chain hash %s should have been cleaned up (cleanup tail=%d)",
				n, sideHashes[n].Hex(), cleanupTail)
		}
	}

	// Canonical blocks in the cleaned-up range should also be gone from KV.
	for n := sideChainStart; n < sideChainEnd; n++ {
		if blockExistsInKV(t, db, n) {
			t.Errorf("block %d: canonical block should have been cleaned up (cleanup tail=%d)",
				n, cleanupTail)
		}
	}

	// Verify canonical blocks are still readable from the ancient store.
	for n := sideChainStart; n < sideChainEnd; n++ {
		hash, err := db.Ancient(ChainFreezerHashTable, n)
		if err != nil || len(hash) == 0 {
			t.Errorf("block %d: should still be readable from ancient store after KV cleanup", n)
		}
	}
}

// TestFreezerRepeatedCrashRecovery verifies that blocks within the safety
// margin remain recoverable across multiple crash/recovery cycles and that
// the cleanup tail advances monotonically.
func TestFreezerRepeatedCrashRecovery(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	var prevTail uint64
	for round := 1; round <= 3; round++ {
		// Freeze
		freeze()
		frozen, err := db.Ancients()
		if err != nil {
			t.Fatal(err)
		}

		// Simulate unclean shutdown: lose 50 blocks from freezer tip
		truncateTarget := frozen - 50
		if _, err := db.TruncateHead(truncateTarget); err != nil {
			t.Fatal(err)
		}
		frozenAfter, err := db.Ancients()
		if err != nil {
			t.Fatal(err)
		}

		// Verify lost blocks are still in LevelDB
		for n := frozenAfter; n < frozen; n++ {
			if !blockExistsInKV(t, db, n) {
				t.Fatalf("round %d: block %d truncated from freezer but not in KV", round, n)
			}
		}

		// Re-freeze to recover
		freeze()
		recovered, err := db.Ancients()
		if err != nil {
			t.Fatal(err)
		}
		if recovered < frozen {
			t.Fatalf("round %d: expected recovery to %d, got %d", round, frozen, recovered)
		}

		tail, tailOk := ReadFreezerCleanupTail(db)
		if !tailOk {
			t.Fatalf("round %d: cleanup tail should exist after freeze", round)
		}
		t.Logf("round %d: frozen=%d, truncated=%d, recovered=%d, cleanupTail=%d",
			round, frozen, frozenAfter, recovered, tail)

		// Cleanup tail must never go backwards.
		if round > 1 && tail < prevTail {
			t.Fatalf("round %d: cleanup tail went backwards: %d -> %d", round, prevTail, tail)
		}
		prevTail = tail
	}
}

// TestFreezerDataLossBeyondMargin verifies that the node refuses to start
// when the freezer's block count has fallen below the cleanup tail (i.e.,
// blocks were deleted from LevelDB that the freezer can no longer serve).
func TestFreezerDataLossBeyondMargin(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100

	ancientDir := t.TempDir()
	kvdb := NewMemoryDatabase()

	db, err := Open(kvdb, OpenOptions{Ancient: ancientDir})
	if err != nil {
		t.Fatal(err)
	}
	setTestMargin(t, db, margin)

	setupTestChain(db, totalBlocks)

	// Freeze to move blocks to the ancient store
	freezeTestDB(t, db)
	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("frozen=%d", frozen)

	// Simulate severe data loss: set cleanup tail well ahead of where the
	// freezer will be after truncation. This represents the scenario where
	// blocks were cleaned from LevelDB but then the freezer lost data.
	fakeTail := frozen + 100
	WriteFreezerCleanupTail(kvdb, fakeTail)

	// Now truncate the freezer below the cleanup tail
	truncateTarget := frozen - 50
	if _, err := db.TruncateHead(truncateTarget); err != nil {
		t.Fatal(err)
	}
	frozenAfter, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("truncated freezer to %d (cleanup tail=%d, gap=%d)", frozenAfter, fakeTail, fakeTail-frozenAfter)

	// Close the first db to release the freezer lock.
	db.Close()

	// Verify the check would catch this on startup by calling Open with a
	// new kvdb that has the cleanup tail pre-set and enough chain data for
	// the existing gap check to not fire first.
	kvdb2 := NewMemoryDatabase()
	for i := uint64(0); i <= frozenAfter+1; i++ {
		writeTestBlock(kvdb2, i)
	}
	WriteFreezerCleanupTail(kvdb2, fakeTail)

	_, err = Open(kvdb2, OpenOptions{Ancient: ancientDir})
	if err == nil {
		t.Fatal("expected error when opening database with data loss beyond safety margin")
	}
	if !strings.Contains(err.Error(), "beyond safety margin") {
		t.Fatalf("expected 'beyond safety margin' error, got: %v", err)
	}
	t.Logf("correctly refused to start: %v", err)
}

// TestFreezerEmptyFreezerWithCleanupTail verifies that the startup check
// catches the case where the freezer is completely empty (e.g. ancient
// directory wiped) but the KV store still has a cleanup tail from a previous
// run, indicating blocks were already deleted from LevelDB.
func TestFreezerEmptyFreezerWithCleanupTail(t *testing.T) {
	ancientDir := t.TempDir()
	kvdb := NewMemoryDatabase()

	// Write genesis and a few blocks so the KV store looks populated.
	for i := uint64(0); i <= 5; i++ {
		writeTestBlock(kvdb, i)
	}
	// Simulate: cleanup ran previously (blocks deleted from KV) but the
	// freezer is brand new / wiped — frozen will be 0.
	WriteFreezerCleanupTail(kvdb, 50)

	_, err := Open(kvdb, OpenOptions{Ancient: ancientDir})
	if err == nil {
		t.Fatal("expected error when freezer is empty but cleanup tail exists")
	}
	if !strings.Contains(err.Error(), "beyond safety margin") {
		t.Fatalf("expected 'beyond safety margin' error, got: %v", err)
	}
	t.Logf("correctly refused to start: %v", err)
}

// TestFreezerCleanupTailEqualsFrozenAllowed verifies that the startup check
// accepts cleanupTail == frozen (no data gap exists in this case).
func TestFreezerCleanupTailEqualsFrozenAllowed(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100
	ancientDir := t.TempDir()
	kvdb := NewMemoryDatabase()

	// Create a populated freezer with a meaningful frozen count.
	db, err := Open(kvdb, OpenOptions{Ancient: ancientDir})
	if err != nil {
		t.Fatal(err)
	}
	setTestMargin(t, db, margin)
	setupTestChain(db, totalBlocks)
	freezeTestDB(t, db)
	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if frozen == 0 {
		t.Fatal("expected some blocks to be frozen")
	}
	t.Logf("frozen=%d", frozen)
	db.Close()

	// Set cleanup tail exactly equal to frozen (boundary condition).
	kvdb2 := NewMemoryDatabase()
	for i := uint64(0); i <= frozen+1; i++ {
		writeTestBlock(kvdb2, i)
	}
	WriteFreezerCleanupTail(kvdb2, frozen)

	db2, err := Open(kvdb2, OpenOptions{Ancient: ancientDir})
	if err != nil {
		t.Fatalf("cleanupTail == frozen (%d) should be accepted, got error: %v", frozen, err)
	}
	db2.Close()
}

// TestFreezerNoCleanupWhenFrozenWithinMargin verifies that no blocks are
// cleaned from LevelDB when the frozen count is still within the safety margin.
func TestFreezerNoCleanupWhenFrozenWithinMargin(t *testing.T) {
	margin := uint64(200)
	// Create just enough blocks to freeze some, but fewer than margin + immutability.
	// With params.FullImmutabilityThreshold and margin=200, we need at least
	// FullImmutabilityThreshold + some blocks to freeze anything. The frozen
	// count will be small relative to the margin.
	totalBlocks := uint64(params.FullImmutabilityThreshold) + 50

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)
	freeze()

	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("frozen=%d, margin=%d", frozen, margin)

	if frozen > margin {
		t.Fatalf("expected frozen (%d) <= margin (%d) given test parameters", frozen, margin)
	}

	// No blocks should have been cleaned up since frozen <= margin.
	// The cleanup tail should not exist at all because cleanupLimit = 0 when
	// frozen <= margin, so WriteFreezerCleanupTail is never called.
	if tail, ok := ReadFreezerCleanupTail(db); ok {
		t.Errorf("cleanup tail should not exist when frozen <= margin, but found tail=%d", tail)
	}

	// All non-genesis frozen blocks should still be in KV.
	for bn := uint64(1); bn < frozen; bn++ {
		if !blockExistsInKV(t, db, bn) {
			t.Errorf("block %d should still be in KV when frozen (%d) <= margin (%d)", bn, frozen, margin)
		}
	}
}

// TestFreezerZeroMargin verifies that a zero safety margin degrades gracefully
// to the old behavior (immediate cleanup) without off-by-one errors or panics.
func TestFreezerZeroMargin(t *testing.T) {
	margin := uint64(0)
	totalBlocks := uint64(params.FullImmutabilityThreshold) + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)
	freeze()

	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if frozen == 0 {
		t.Fatal("expected some blocks to be frozen")
	}
	t.Logf("frozen=%d, margin=0", frozen)

	// With margin=0, cleanupLimit = frozen - 0 = frozen, so all frozen blocks
	// (except genesis) should be cleaned from KV on the first cycle (fresh
	// install path starts cleanup from block 1). Add more blocks and freeze
	// again to extend the range further.
	extendTestChain(db, totalBlocks, totalBlocks+100)
	freeze()

	frozen2, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}

	// With zero margin, blocks well behind the freeze point should be gone from KV.
	cleanupTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should exist after freeze with zero margin")
	}
	t.Logf("frozen=%d, cleanupTail=%d", frozen2, cleanupTail)

	// Verify a block in the cleaned range is gone from KV.
	if cleanupTail <= 2 {
		t.Fatalf("expected cleanupTail > 2 with zero margin, got %d", cleanupTail)
	}
	if blockExistsInKV(t, db, cleanupTail-1) {
		t.Errorf("block %d should have been cleaned from KV with zero margin", cleanupTail-1)
	}

	// Genesis must still be preserved.
	if !blockExistsInKV(t, db, 0) {
		t.Fatal("genesis block (0) should never be deleted")
	}
}

// TestFreezerCleanupPerCycleCap verifies that the cleanup logic caps the
// number of blocks deleted per freeze cycle to freezerBatchLimit. This test
// artificially sets the cleanup tail to 1 to create a large backlog, then
// verifies that cleanup advances by at most freezerBatchLimit per cycle.
func TestFreezerCleanupPerCycleCap(t *testing.T) {
	margin := uint64(50)
	totalBlocks := params.FullImmutabilityThreshold + margin + 200

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	// Freeze all blocks to build up the ancient store.
	freeze()

	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if frozen <= margin {
		t.Fatalf("expected frozen (%d) > margin (%d) given test parameters", frozen, margin)
	}

	origTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should exist after freeze")
	}

	// Simulate an upgrade scenario: reset the cleanup tail to 1.
	// This creates a backlog of blocks to clean up.
	WriteFreezerCleanupTail(db, 1)

	// Add blocks to trigger another freeze cycle.
	newTotal := totalBlocks + 10
	extendTestChain(db, totalBlocks, newTotal)
	freeze()

	newTail, _ := ReadFreezerCleanupTail(db)
	advanced := newTail - 1 // started from 1

	t.Logf("frozen=%d, cleanup advanced by %d blocks (batchLimit=%d, full target=%d)",
		frozen, advanced, freezerBatchLimit, origTail)

	// Verify cleanup advanced but did not exceed the per-cycle cap.
	if newTail <= 1 {
		t.Error("cleanup should have made some progress")
	}
	if advanced > freezerBatchLimit {
		t.Errorf("cleanup exceeded per-cycle cap: advanced %d blocks (limit is %d)",
			advanced, freezerBatchLimit)
	}
}

// TestFreezerCleanupTailCorruptData verifies that ReadFreezerCleanupTail
// handles corrupt or short data gracefully by returning (0, false).
func TestFreezerCleanupTailCorruptData(t *testing.T) {
	db := NewMemoryDatabase()

	// Write a too-short value (4 bytes instead of 8).
	if err := db.Put(freezerCleanupTailKey, []byte{0x01, 0x02, 0x03, 0x04}); err != nil {
		t.Fatal(err)
	}
	val, ok := ReadFreezerCleanupTail(db)
	if ok {
		t.Fatalf("expected (0, false) for 4-byte data, got (%d, true)", val)
	}

	// Write an empty value.
	if err := db.Put(freezerCleanupTailKey, []byte{}); err != nil {
		t.Fatal(err)
	}
	val, ok = ReadFreezerCleanupTail(db)
	if ok {
		t.Fatalf("expected (0, false) for empty data, got (%d, true)", val)
	}

	// Write a too-long value (16 bytes).
	if err := db.Put(freezerCleanupTailKey, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	val, ok = ReadFreezerCleanupTail(db)
	if ok {
		t.Fatalf("expected (0, false) for 16-byte data, got (%d, true)", val)
	}

	// Valid 8-byte value should still work.
	WriteFreezerCleanupTail(db, 42)
	val, ok = ReadFreezerCleanupTail(db)
	if !ok || val != 42 {
		t.Fatalf("expected (42, true), got (%d, %v)", val, ok)
	}
}

// TestReadFreezerCleanupTailStrict verifies that readFreezerCleanupTailStrict
// returns proper errors for corrupt data (rather than silently suppressing
// them like the non-strict variant).
func TestReadFreezerCleanupTailStrict(t *testing.T) {
	db := NewMemoryDatabase()

	// Key absent: should return (0, false, nil).
	val, ok, err := readFreezerCleanupTailStrict(db)
	if err != nil || ok || val != 0 {
		t.Fatalf("absent key: expected (0, false, nil), got (%d, %v, %v)", val, ok, err)
	}

	// Corrupt data (4 bytes): should return a non-nil error.
	if putErr := db.Put(freezerCleanupTailKey, []byte{1, 2, 3, 4}); putErr != nil {
		t.Fatal(putErr)
	}
	val, ok, err = readFreezerCleanupTailStrict(db)
	if err == nil {
		t.Fatalf("corrupt data: expected error, got (%d, %v, nil)", val, ok)
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("corrupt data: expected 'corrupt' in error, got: %v", err)
	}

	// Valid 8-byte data: should succeed.
	WriteFreezerCleanupTail(db, 42)
	val, ok, err = readFreezerCleanupTailStrict(db)
	if err != nil || !ok || val != 42 {
		t.Fatalf("valid data: expected (42, true, nil), got (%d, %v, %v)", val, ok, err)
	}
}

// TestFreezerCorruptCleanupTailAtStartup verifies that the startup check in
// Open() detects corrupt cleanup tail data and fails with a proper error,
// rather than silently ignoring it and proceeding.
func TestFreezerCorruptCleanupTailAtStartup(t *testing.T) {
	ancientDir := t.TempDir()
	kvdb := NewMemoryDatabase()

	// Write some blocks to make the database look populated.
	for i := uint64(0); i <= 10; i++ {
		writeTestBlock(kvdb, i)
	}

	// Write corrupt cleanup tail data (4 bytes instead of 8).
	if err := kvdb.Put(freezerCleanupTailKey, []byte{0x01, 0x02, 0x03, 0x04}); err != nil {
		t.Fatal(err)
	}

	_, err := Open(kvdb, OpenOptions{Ancient: ancientDir})
	if err == nil {
		t.Fatal("expected error when cleanup tail data is corrupt")
	}
	if !strings.Contains(err.Error(), "corrupt freezer cleanup tail data") {
		t.Fatalf("expected 'corrupt freezer cleanup tail data' error, got: %v", err)
	}
	t.Logf("correctly refused to start: %v", err)
}

// TestFreezerCleanupTailSetThenTruncate verifies the edge case where the
// cleanup tail is set on the first freeze cycle, and then before additional
// cycles run the freezer is truncated past the tail (simulating a crash +
// repair). If the freezer is truncated below that tail, the startup check
// detects the inconsistency.
func TestFreezerCleanupTailSetThenTruncate(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100

	ancientDir := t.TempDir()
	kvdb := NewMemoryDatabase()
	db, err := Open(kvdb, OpenOptions{Ancient: ancientDir})
	if err != nil {
		t.Fatal(err)
	}
	setTestMargin(t, db, margin)

	setupTestChain(db, totalBlocks)

	// Freeze to set the cleanup tail.
	freezeTestDB(t, db)
	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	cleanupTail, ok := ReadFreezerCleanupTail(db)
	if !ok || cleanupTail == 0 {
		t.Fatal("cleanup tail should be set after first freeze")
	}
	t.Logf("frozen=%d, cleanupTail=%d, margin=%d", frozen, cleanupTail, margin)

	// Simulate severe truncation: repair() rolls the freezer back past the
	// cleanup tail. All data is still in LevelDB (no blocks were deleted in
	// the first cycle), but the startup check should still flag this.
	truncateTarget := cleanupTail - 10
	if truncateTarget >= frozen {
		t.Fatalf("test setup: truncateTarget %d should be < frozen %d", truncateTarget, frozen)
	}
	if _, err := db.TruncateHead(truncateTarget); err != nil {
		t.Fatal(err)
	}
	frozenAfter, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("truncated to %d (below cleanup tail %d)", frozenAfter, cleanupTail)
	db.Close()

	// Reopen: the startup check should detect cleanupTail > frozen.
	kvdb2 := NewMemoryDatabase()
	for i := uint64(0); i <= frozenAfter+1; i++ {
		writeTestBlock(kvdb2, i)
	}
	WriteFreezerCleanupTail(kvdb2, cleanupTail)

	_, err = Open(kvdb2, OpenOptions{Ancient: ancientDir})
	if err == nil {
		t.Fatal("expected startup error when freezer truncated below cleanup tail")
	}
	if !strings.Contains(err.Error(), "beyond safety margin") {
		t.Fatalf("expected 'beyond safety margin' error, got: %v", err)
	}
	t.Logf("correctly refused to start: %v", err)
}

// TestFreezerUpgradeFromOldCode verifies that the first freeze cycle on a
// node upgrading from old code (which deleted blocks immediately without a
// margin) correctly initializes the cleanup tail without errors, even though
// blocks in [1, frozen-margin) are already absent from LevelDB.
//
// The upgrade skip-ahead path requires frozen > FullImmutabilityThreshold.
// Since creating 90k+ blocks is slow, this test uses a small margin so that
// frozen (≈300) stays below the threshold and takes the fresh install path
// instead — which still handles missing blocks gracefully via skippedCleanup.
// The actual upgrade skip-ahead branch is not covered here due to the cost
// of creating 90k+ blocks in a unit test.
func TestFreezerUpgradeFromOldCode(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	// Freeze to populate the ancient store.
	freeze()

	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if frozen <= margin {
		t.Fatalf("expected frozen (%d) > margin (%d) given test parameters", frozen, margin)
	}

	// Simulate old code behavior: delete early blocks from KV and remove
	// the cleanup tail key (as if the feature never existed).
	batch := db.NewBatch()
	nfdb := &nofreezedb{KeyValueStore: db}
	for number := uint64(1); number < frozen-margin-50; number++ {
		hash := ReadCanonicalHash(nfdb, number)
		if hash != (common.Hash{}) {
			DeleteBlockWithoutNumber(batch, hash, number)
			DeleteCanonicalHash(batch, number)
		}
	}
	if err := batch.Write(); err != nil {
		t.Fatal(err)
	}
	// Remove the cleanup tail to simulate pre-feature state.
	if err := db.Delete(freezerCleanupTailKey); err != nil {
		t.Fatal(err)
	}

	// Verify cleanup tail is gone.
	if _, ok := ReadFreezerCleanupTail(db); ok {
		t.Fatal("cleanup tail should not exist after deletion")
	}

	// Add more blocks and freeze again. With frozen < FullImmutabilityThreshold,
	// this takes the fresh install path (cleanup from block 1), which handles
	// the missing blocks via skippedCleanup without errors.
	extendTestChain(db, totalBlocks, totalBlocks+margin)

	for i := 0; i < 3; i++ {
		freeze()
	}

	// Verify the cleanup tail was re-established.
	tail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should be set after freeze on upgraded node")
	}
	t.Logf("upgrade: frozen=%d, new cleanupTail=%d", frozen, tail)

	// The tail should be reasonable (not 0 or 1).
	if tail <= 1 {
		t.Errorf("cleanup tail should have advanced past 1, got %d", tail)
	}
}

// TestFreezerCorruptHeaderInDanglingChase verifies that the dangling side chain
// cleanup handles the case where a block's header is corrupt (undecodable).
// The cleanup should log an error but continue processing without crashing.
func TestFreezerCorruptHeaderInDanglingChase(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	// First freeze to establish cleanup tail.
	freeze()
	initTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail not set")
	}

	// Write a side chain block at a height that will be cleaned up.
	sideChainHeight := initTail + 10
	sideChainHash := writeSideChainBlock(db, sideChainHeight, 1)

	// Write a child block at the next height, then corrupt its header.
	// ReadAllHashes will still find it (the key exists), but ReadHeader
	// will fail to decode and return nil.
	childHeight := sideChainHeight + 1
	childHash := writeBlockWithParent(db, childHeight, sideChainHash, []byte("orphan-with-corrupt-header"))

	// Corrupt the header by writing invalid RLP data to the header key.
	// The key still exists so ReadAllHashes finds it, but RLP decode fails.
	if err := db.Put(headerKey(childHeight, childHash), []byte{0xff, 0xff, 0xff}); err != nil {
		t.Fatal(err)
	}
	t.Logf("corrupted header at height %d", childHeight)

	// Verify the block is discoverable via ReadAllHashes (key exists).
	if !slices.Contains(ReadAllHashes(db, childHeight), childHash) {
		t.Fatal("corrupted block should still be discoverable via ReadAllHashes")
	}

	// Verify ReadHeader returns nil for the corrupt block.
	if header := ReadHeader(db, childHash, childHeight); header != nil {
		t.Fatal("ReadHeader should return nil for corrupt header")
	}

	// Advance cleanup past the side chain. The dangling chain chase will
	// encounter the child block, call ReadHeader, get nil, log an error,
	// and continue. The test passes if no panic occurs.
	advanceCleanupPast(t, db, freeze, margin, totalBlocks, sideChainHeight+1)

	// Success: cleanup completed without crashing.
	t.Logf("dangling chain chase handled corrupt header gracefully")
}

// TestFreezerStaleCleanupTailRecovery verifies that the cleanup loop handles
// the scenario where the cleanup tail is stale (behind actual deletions). This
// can happen if the node crashes after committing canonical block deletions
// but before updating the cleanup tail. The next cycle should skip over the
// already-deleted blocks without errors.
func TestFreezerStaleCleanupTailRecovery(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	// Freeze to establish cleanup tail and run some cleanup.
	for i := 0; i < 3; i++ {
		freeze()
	}

	cleanupTail, ok := ReadFreezerCleanupTail(db)
	if !ok || cleanupTail <= 10 {
		t.Fatalf("expected cleanup tail > 10 after 3 freeze cycles, got ok=%v tail=%d", ok, cleanupTail)
	}
	t.Logf("cleanup tail before rollback: %d", cleanupTail)

	// Simulate a crash scenario: roll back the cleanup tail to an earlier value.
	// This simulates the case where canonical block deletions were committed
	// but the cleanup tail update batch crashed before committing.
	staleTail := cleanupTail - 50
	if staleTail < 1 {
		staleTail = 1
	}
	WriteFreezerCleanupTail(db, staleTail)
	t.Logf("rolled back cleanup tail to: %d (simulating stale state)", staleTail)

	// Add more blocks and run another freeze cycle. The cleanup loop should
	// iterate over blocks in [staleTail, cleanupLimit) even though some of
	// them are already deleted from KV. It should skip them gracefully.
	extendTestChain(db, totalBlocks, totalBlocks+margin)
	freeze()

	newTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should exist after recovery freeze")
	}
	if newTail <= staleTail {
		t.Errorf("cleanup tail should have advanced past stale value %d, got %d", staleTail, newTail)
	}
	t.Logf("cleanup tail after recovery: %d (advanced by %d)", newTail, newTail-staleTail)

	// The test passes if no panic or error occurred during the recovery freeze.
	// The cleanup loop's skip logic should have handled the missing blocks.
}

// TestFreezerDanglingChildCleanup verifies that orphaned children of deleted
// side chain blocks are also cleaned up. When a side chain block at height N
// is deleted, any block at height N+1 whose parent hash points to that side
// chain block should also be deleted (and so on recursively).
func TestFreezerDanglingChildCleanup(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	// First freeze to establish cleanup tail.
	freeze()
	initTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail not set after first freeze")
	}
	t.Logf("initial cleanup tail: %d", initTail)

	// Write a side chain block at height initTail+10.
	sideChainHeight := initTail + 10
	sideChainHash := writeSideChainBlock(db, sideChainHeight, 1)
	t.Logf("wrote side chain block at height %d: %s", sideChainHeight, sideChainHash.Hex())

	// Write a "dangling child" block at height initTail+11 whose parent is the
	// side chain block. This simulates a side chain that extended beyond the
	// canonical chain's fork point.
	danglingChildHeight := sideChainHeight + 1
	danglingChildHash := writeBlockWithParent(db, danglingChildHeight, sideChainHash, []byte("dangling-child"))
	t.Logf("wrote dangling child block at height %d: %s (parent: %s)",
		danglingChildHeight, danglingChildHash.Hex(), sideChainHash.Hex())

	// Verify the dangling child is discoverable.
	if !slices.Contains(ReadAllHashes(db, danglingChildHeight), danglingChildHash) {
		t.Fatal("dangling child block should be discoverable via ReadAllHashes")
	}

	// Advance the cleanup tail past the side chain height. The side chain block
	// will be deleted, and the dangling child cleanup logic should chase it.
	advanceCleanupPast(t, db, freeze, margin, totalBlocks, sideChainHeight+1)

	// The side chain block should be gone.
	if slices.Contains(ReadAllHashes(db, sideChainHeight), sideChainHash) {
		t.Errorf("side chain block at height %d should have been cleaned up", sideChainHeight)
	}

	// The dangling child should also be gone (this is the key assertion).
	if slices.Contains(ReadAllHashes(db, danglingChildHeight), danglingChildHash) {
		t.Errorf("dangling child block at height %d should have been cleaned up "+
			"(its parent %s was a deleted side chain block)", danglingChildHeight, sideChainHash.Hex())
	}
	t.Logf("dangling child cleanup verified: both side chain and its child are gone")
}

// TestFreezerCleanupBoundaryBlock verifies that the block at exactly
// frozen - cleanupMargin is NOT deleted (it is the first block
// within the safety margin), while the block just before it IS deleted.
func TestFreezerCleanupBoundaryBlock(t *testing.T) {
	margin := uint64(200)
	// Create enough blocks to freeze well past the margin.
	phase1Blocks := params.FullImmutabilityThreshold + margin + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, phase1Blocks)

	// First freeze: initializes cleanup tail and cleans blocks from 1.
	freeze()

	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if frozen <= margin {
		t.Fatalf("expected frozen (%d) > margin (%d) given test parameters", frozen, margin)
	}

	// Add more blocks and freeze until cleanup actually runs.
	// Use a large target to ensure enough cycles run.
	advanceCleanupPast(t, db, freeze, margin, phase1Blocks, frozen-1)

	frozen2, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	cleanupTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should exist")
	}
	t.Logf("frozen=%d, cleanupTail=%d, margin=%d", frozen2, cleanupTail, margin)

	if cleanupTail <= 2 {
		t.Fatalf("expected cleanup tail > 2 after advanceCleanupPast, got %d", cleanupTail)
	}

	// The block at cleanupTail-1 should have been deleted from KV (it is
	// the last block in the cleaned range [1, cleanupTail)).
	if blockExistsInKV(t, db, cleanupTail-1) {
		t.Errorf("block %d (cleanupTail-1) should have been cleaned from KV", cleanupTail-1)
	}

	// The boundary block: frozen2 - margin should still be in KV.
	// This is the first block within the safety margin. Given the test
	// parameters, frozen2 > margin is guaranteed, and boundaryBlock ==
	// cleanupTail (cleanup never advances past frozen - margin).
	if frozen2 <= margin {
		t.Fatalf("expected frozen2 (%d) > margin (%d)", frozen2, margin)
	}
	boundaryBlock := frozen2 - margin
	if !blockExistsInKV(t, db, boundaryBlock) {
		t.Errorf("boundary block %d (frozen-margin) should still be in KV", boundaryBlock)
	}

	// Blocks well within the margin should definitely be in KV.
	recentBlock := frozen2 - 1
	if !blockExistsInKV(t, db, recentBlock) {
		t.Errorf("block %d (most recent frozen) should still be in KV", recentBlock)
	}
}

// TestFreezerFreshInstallCleansEarlyBlocks verifies that on a fresh install
// (frozen <= FullImmutabilityThreshold when cleanup tail is first initialized),
// blocks starting from 1 are cleaned from the KV store rather than being
// permanently skipped. Without this, blocks 1..margin would remain as garbage
// in the KV store forever because the skip-ahead optimization would jump past them.
func TestFreezerFreshInstallCleansEarlyBlocks(t *testing.T) {
	margin := uint64(200)
	// Use enough blocks to freeze past the margin but stay below
	// FullImmutabilityThreshold so the fresh install path is taken.
	totalBlocks := uint64(params.FullImmutabilityThreshold) + margin + 100

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	// First freeze: on a fresh install, cleanupStart should be 1 (not
	// skip-ahead to cleanupLimit) since frozen <= FullImmutabilityThreshold.
	freeze()

	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if frozen <= margin {
		t.Fatalf("expected frozen (%d) > margin (%d) given test parameters", frozen, margin)
	}
	if frozen > params.FullImmutabilityThreshold {
		t.Fatalf("expected frozen (%d) <= FullImmutabilityThreshold (%d) for fresh install test",
			frozen, params.FullImmutabilityThreshold)
	}

	cleanupTail, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should be set after first freeze")
	}
	t.Logf("frozen=%d, cleanupTail=%d, margin=%d", frozen, cleanupTail, margin)

	// On a fresh install, the first cycle starts cleanup from block 1,
	// capped by freezerBatchLimit. Verify the tail reflects progress from 1.
	expectedLimit := frozen - margin
	if cleanupTail != expectedLimit {
		t.Errorf("expected cleanup tail %d (frozen %d - margin %d), got %d",
			expectedLimit, frozen, margin, cleanupTail)
	}

	// Early blocks (1..cleanupTail) should have been cleaned from KV.
	for _, bn := range []uint64{1, 2, cleanupTail - 1} {
		if bn >= cleanupTail || bn == 0 {
			continue
		}
		if blockExistsInKV(t, db, bn) {
			t.Errorf("block %d should have been cleaned from KV on fresh install (cleanupTail=%d)", bn, cleanupTail)
		}
	}

	// Blocks within the margin should still be in KV.
	if !blockExistsInKV(t, db, frozen-1) {
		t.Errorf("block %d (within margin) should still be in KV", frozen-1)
	}
}

// TestFreezerCleanupTailRegressionGuard verifies that when the frozen count
// regresses (e.g., after repair() truncation), the cleanup tail is NOT
// decreased. Writing a lower tail would cause the node to skip cleanup of
// blocks that were already deleted, creating a gap.
func TestFreezerCleanupTailRegressionGuard(t *testing.T) {
	margin := uint64(200)
	totalBlocks := params.FullImmutabilityThreshold + margin + 200

	db, freeze := openTestFreezerDB(t, margin)
	setupTestChain(db, totalBlocks)

	// Freeze and advance cleanup to establish a meaningful tail.
	freeze()
	extendTestChain(db, totalBlocks, totalBlocks+margin)
	freeze()

	tailBefore, ok := ReadFreezerCleanupTail(db)
	if !ok || tailBefore <= 1 {
		t.Fatal("cleanup tail should be established and > 1")
	}

	frozen, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("before regression: frozen=%d, cleanupTail=%d", frozen, tailBefore)

	// Simulate repair() truncating the freezer below the margin. This makes
	// cleanupLimit (= frozen - margin) less than the current tail.
	truncateTarget := tailBefore - 10
	if truncateTarget == 0 {
		truncateTarget = 1
	}
	if _, err := db.TruncateHead(truncateTarget); err != nil {
		t.Fatal(err)
	}
	frozenAfter, err := db.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after truncation: frozen=%d (cleanupLimit would be %d, tail is %d)",
		frozenAfter, max(frozenAfter, margin)-margin, tailBefore)

	// Extend chain and freeze. The regression guard should prevent the tail
	// from being written with a value lower than tailBefore.
	extendTestChain(db, totalBlocks+margin, totalBlocks+margin+100)
	freeze()

	tailAfter, ok := ReadFreezerCleanupTail(db)
	if !ok {
		t.Fatal("cleanup tail should still exist after regressed freeze")
	}
	if tailAfter < tailBefore {
		t.Fatalf("cleanup tail regressed: was %d, now %d (regression guard failed)", tailBefore, tailAfter)
	}
	t.Logf("regression guard held: tail stayed at %d (or advanced to %d)", tailBefore, tailAfter)
}

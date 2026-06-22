package state

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
)

const recentWasmsRetain = uint16(8)

func newTestState(t *testing.T) *StateDB {
	t.Helper()
	state, err := New(types.EmptyRootHash, NewDatabaseForTesting())
	if err != nil {
		t.Fatalf("failed to create state: %v", err)
	}
	return state
}

// Insert reports a miss then a hit; Copy carries the cache and stays independent.
func TestRecentWasmsInsertAndCopy(t *testing.T) {
	state := newTestState(t)

	hash1 := common.HexToHash("0x01")
	hash2 := common.HexToHash("0x02")
	hash3 := common.HexToHash("0x03")

	if hit := state.GetRecentWasms().Insert(hash1, recentWasmsRetain); hit {
		t.Fatalf("first insert of hash1 should be a miss")
	}
	if hit := state.GetRecentWasms().Insert(hash1, recentWasmsRetain); !hit {
		t.Fatalf("second insert of hash1 should be a hit")
	}
	if hit := state.GetRecentWasms().Insert(hash2, recentWasmsRetain); hit {
		t.Fatalf("first insert of hash2 should be a miss")
	}

	cp := state.Copy()
	if hit := cp.GetRecentWasms().Insert(hash1, recentWasmsRetain); !hit {
		t.Fatalf("copy: expected hit for hash1 present before copy")
	}
	if hit := cp.GetRecentWasms().Insert(hash2, recentWasmsRetain); !hit {
		t.Fatalf("copy: expected hit for hash2 present before copy")
	}
	// A new insert on the copy must not leak back into the original.
	if hit := cp.GetRecentWasms().Insert(hash3, recentWasmsRetain); hit {
		t.Fatalf("copy: first insert of hash3 should be a miss")
	}
	if hit := state.GetRecentWasms().Insert(hash3, recentWasmsRetain); hit {
		t.Fatalf("original: hash3 inserted on the copy must not leak back")
	}
}

// A single LRU: filling past capacity evicts the least-recently-used entry, so
// re-touching an evicted program is a miss.
func TestRecentWasmsEviction(t *testing.T) {
	state := newTestState(t)
	rw := state.GetRecentWasms()
	const capacity = uint16(4)

	for i := 0; i < int(capacity); i++ {
		h := common.BytesToHash([]byte{byte(i + 1)})
		if hit := rw.Insert(h, capacity); hit {
			t.Fatalf("fill insert %d should be a miss", i)
		}
	}
	oldest := common.BytesToHash([]byte{1})
	overflow := common.BytesToHash([]byte{byte(capacity + 1)})
	if hit := rw.Insert(overflow, capacity); hit {
		t.Fatalf("overflow insert should be a miss")
	}
	if hit := rw.Insert(oldest, capacity); hit {
		t.Fatalf("the evicted oldest entry should be a miss on re-insert")
	}
}

// RestoreRecentWasms (used by the block processor to undo a dropped tx) must
// restore the cache's exact contents AND recency, undoing additions and evictions.
func TestRecentWasmsRestore(t *testing.T) {
	state := newTestState(t)
	rw := state.GetRecentWasms()
	const capacity = uint16(4)

	base := make([]common.Hash, capacity)
	for i := range base {
		base[i] = common.BytesToHash([]byte{byte(i + 1)})
		rw.Insert(base[i], capacity) // base[0] oldest ... base[3] newest
	}

	snapshot := state.GetRecentWasms().Copy()

	// Mutate: bump base[0] to most-recent (a Get hit), then add a new hash, which
	// evicts the new oldest (base[1]).
	if hit := rw.Insert(base[0], capacity); !hit {
		t.Fatalf("base[0] should be present before restore")
	}
	newHash := common.BytesToHash([]byte{0xAA})
	rw.Insert(newHash, capacity)

	state.RestoreRecentWasms(snapshot)
	rw = state.GetRecentWasms()

	// Contents restored: base[1] (evicted during mutation) is back; newHash is gone.
	if hit := rw.Insert(base[1], capacity); !hit {
		t.Fatalf("restore must bring back the entry evicted during the mutation")
	}
	// Restore again so the base[1] insert above can't mask newHash's absence.
	state.RestoreRecentWasms(snapshot)
	rw = state.GetRecentWasms()
	if hit := rw.Insert(newHash, capacity); hit {
		t.Fatalf("restore must drop the entry added during the mutation")
	}

	// Recency restored: base[0] was the snapshot's oldest, so one eviction drops it
	// — it would have survived if the pre-restore recency bump had leaked through.
	state.RestoreRecentWasms(snapshot)
	rw = state.GetRecentWasms()
	rw.Insert(common.BytesToHash([]byte{0xBB}), capacity) // evicts snapshot's oldest = base[0]
	if hit := rw.Insert(base[0], capacity); hit {
		t.Fatalf("restore must preserve recency: base[0] was oldest and should evict first")
	}
}

// The cache is block-level, not per-tx: a program warmed in one tx stays warm for
// later txs in the same block (there is no per-tx reset).
func TestRecentWasmsPersistsAcrossTxs(t *testing.T) {
	state := newTestState(t)
	h := common.HexToHash("0x0a")

	state.SetTxContext(common.HexToHash("0x01"), 0)
	if hit := state.GetRecentWasms().Insert(h, recentWasmsRetain); hit {
		t.Fatalf("first insert should be a miss")
	}
	state.SetTxContext(common.HexToHash("0x02"), 1)
	if hit := state.GetRecentWasms().Insert(h, recentWasmsRetain); !hit {
		t.Fatalf("cache must persist across txs in the same block")
	}
}

// A warm-start must survive an EVM revert: a program touched in a reverted
// sub-call (or a reverted-but-included tx) stays warm. Guards against a "fix" that
// drops warmings on revert, which would wrongly charge later calls cold.
func TestRecentWasmsSurvivesRevert(t *testing.T) {
	state := newTestState(t)
	h := common.HexToHash("0x0a")

	snap := state.Snapshot()
	if hit := state.GetRecentWasms().Insert(h, recentWasmsRetain); hit {
		t.Fatalf("first insert should be a miss")
	}
	state.RevertToSnapshot(snap)
	if hit := state.GetRecentWasms().Insert(h, recentWasmsRetain); !hit {
		t.Fatalf("warm-start must survive an EVM revert (cache must not be journaled)")
	}
}

func TestActivateWasmRevert(t *testing.T) {
	db := NewDatabaseForTesting()
	state, err := New(types.EmptyRootHash, db)
	if err != nil {
		t.Fatalf("failed to create state: %v", err)
	}

	module1 := common.HexToHash("0x01")
	module2 := common.HexToHash("0x02")

	asmMap := map[rawdb.WasmTarget][]byte{
		rawdb.TargetArm64:          []byte("sp-arm"),
		rawdb.TargetArm64Cranelift: []byte("cl-arm"),
		rawdb.TargetWavm:           []byte("wavm"),
	}

	// Activate first module before snapshot.
	if err := state.ActivateWasm(module1, asmMap); err != nil {
		t.Fatalf("ActivateWasm(module1): %v", err)
	}

	snap := state.Snapshot()

	// Activate second module after snapshot.
	asmMap2 := map[rawdb.WasmTarget][]byte{
		rawdb.TargetArm64:          []byte("sp-arm-2"),
		rawdb.TargetArm64Cranelift: []byte("cl-arm-2"),
		rawdb.TargetWavm:           []byte("wavm-2"),
	}
	if err := state.ActivateWasm(module2, asmMap2); err != nil {
		t.Fatalf("ActivateWasm(module2): %v", err)
	}

	// Verify both modules are accessible — both singlepass and cranelift.
	if asm := state.ActivatedAsm(rawdb.TargetArm64, module1); string(asm) != "sp-arm" {
		t.Fatalf("module1 singlepass: got %q, want %q", asm, "sp-arm")
	}
	if asm := state.ActivatedAsm(rawdb.TargetArm64Cranelift, module1); string(asm) != "cl-arm" {
		t.Fatalf("module1 cranelift: got %q, want %q", asm, "cl-arm")
	}
	if asm := state.ActivatedAsm(rawdb.TargetArm64, module2); string(asm) != "sp-arm-2" {
		t.Fatalf("module2 singlepass: got %q, want %q", asm, "sp-arm-2")
	}

	// Revert to snapshot — should undo module2 activation.
	state.RevertToSnapshot(snap)

	// module1 should still be accessible.
	if asm := state.ActivatedAsm(rawdb.TargetArm64, module1); string(asm) != "sp-arm" {
		t.Fatalf("module1 singlepass lost after revert: got %q", asm)
	}
	if asm := state.ActivatedAsm(rawdb.TargetArm64Cranelift, module1); string(asm) != "cl-arm" {
		t.Fatalf("module1 cranelift lost after revert: got %q", asm)
	}

	// module2 should be fully reverted.
	if asm := state.ActivatedAsm(rawdb.TargetArm64, module2); len(asm) > 0 {
		t.Fatal("module2 singlepass should be reverted")
	}
	if asm := state.ActivatedAsm(rawdb.TargetArm64Cranelift, module2); len(asm) > 0 {
		t.Fatal("module2 cranelift should be reverted")
	}
}

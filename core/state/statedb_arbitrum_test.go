package state

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestRecentWasmsInsertAndCopy(t *testing.T) {
	db := NewDatabaseForTesting()
	state, err := New(types.EmptyRootHash, db)
	if err != nil {
		t.Fatalf("failed to create state: %v", err)
	}

	const retain = uint16(8)

	hash1 := common.HexToHash("0x01")
	hash2 := common.HexToHash("0x02")
	hash3 := common.HexToHash("0x03")

	if hit := state.GetRecentWasms().Insert(hash1, retain); hit {
		t.Fatalf("first insert of hash1 should be a miss")
	}

	if hit := state.GetRecentWasms().Insert(hash1, retain); !hit {
		t.Fatalf("second insert of hash1 should be a hit (cache not persisting)")
	}

	if hit := state.GetRecentWasms().Insert(hash2, retain); hit {
		t.Fatalf("first insert of hash2 should be a miss")
	}

	copy := state.Copy()

	if hit := copy.GetRecentWasms().Insert(hash1, retain); !hit {
		t.Fatalf("copy: expected hit for hash1 present before copy")
	}
	if hit := copy.GetRecentWasms().Insert(hash2, retain); !hit {
		t.Fatalf("copy: expected hit for hash2 present before copy")
	}

	if hit := copy.GetRecentWasms().Insert(hash3, retain); hit {
		t.Fatalf("copy: first insert of hash3 should be a miss")
	}

	if hit := state.GetRecentWasms().Insert(hash3, retain); hit {
		t.Fatalf("original: first insert of hash3 should be a miss (must be independent of copy)")
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

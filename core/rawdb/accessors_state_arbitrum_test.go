package rawdb

import (
	"testing"

	"github.com/ethereum/go-ethereum/ethdb/memorydb"
)

func TestIsCraneliftTarget(t *testing.T) {
	nonCranelift := []WasmTarget{TargetArm64, TargetAmd64, TargetHost, TargetWasm, TargetWavm}
	for _, target := range nonCranelift {
		if IsCraneliftTarget(target) {
			t.Fatalf("%v should not be cranelift", target)
		}
	}
	cranelift := []WasmTarget{TargetArm64Cranelift, TargetAmd64Cranelift, TargetHostCranelift}
	for _, target := range cranelift {
		if !IsCraneliftTarget(target) {
			t.Fatalf("%v should be cranelift", target)
		}
	}
}

func TestWavmSerializeVersionAccessors(t *testing.T) {
	db := memorydb.New()

	// Reading from an empty db must report not-found rather than a corrupt value.
	if _, err := ReadWavmSerializeVersion(db); err == nil {
		t.Fatalf("expected not-found error reading missing WavmSerializeVersion")
	} else if !IsDbErrNotFound(err) {
		t.Fatalf("expected not-found, got: %v", err)
	}

	// Round-trip a written version.
	const want uint32 = 1
	if err := WriteWavmSerializeVersion(db, want); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	got, err := ReadWavmSerializeVersion(db)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if got != want {
		t.Fatalf("read back %d, want %d", got, want)
	}

	// Writing a different version updates the stored value (idempotent overwrite).
	if err := WriteWavmSerializeVersion(db, 42); err != nil {
		t.Fatalf("overwrite failed: %v", err)
	}
	got, err = ReadWavmSerializeVersion(db)
	if err != nil {
		t.Fatalf("read after overwrite failed: %v", err)
	}
	if got != 42 {
		t.Fatalf("read back %d after overwrite, want 42", got)
	}
}

func TestReadWavmSerializeVersionShortBuffer(t *testing.T) {
	// A truncated/partial value at the version key must return an error
	// rather than panicking inside binary.BigEndian.Uint32 (which would crash
	// the node at startup before the operator-friendly purge path can run).
	db := memorydb.New()
	if err := db.Put(wavmSerializeVersionKey, []byte{0x01}); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	_, err := ReadWavmSerializeVersion(db)
	if err == nil {
		t.Fatalf("expected error reading a 1-byte WavmSerializeVersion value")
	}
}

func TestWasmerAndWavmVersionKeysAreDisjoint(t *testing.T) {
	// Both version keys share the wasmdb. They must address disjoint storage:
	// writing one must not affect the other. A regression that aliased them
	// would round-trip its own value (and hide itself from
	// TestWavmSerializeVersionAccessors) but cross-pollute the sibling key.
	db := memorydb.New()
	const wasmerVal uint32 = 5
	const wavmVal uint32 = 9
	if err := WriteWasmerSerializeVersion(db, wasmerVal); err != nil {
		t.Fatalf("write wasmer failed: %v", err)
	}
	if err := WriteWavmSerializeVersion(db, wavmVal); err != nil {
		t.Fatalf("write wavm failed: %v", err)
	}
	gotWasmer, err := ReadWasmerSerializeVersion(db)
	if err != nil {
		t.Fatalf("read wasmer failed: %v", err)
	}
	if gotWasmer != wasmerVal {
		t.Fatalf("wasmer key cross-polluted by wavm write: got %d, want %d", gotWasmer, wasmerVal)
	}
	gotWavm, err := ReadWavmSerializeVersion(db)
	if err != nil {
		t.Fatalf("read wavm failed: %v", err)
	}
	if gotWavm != wavmVal {
		t.Fatalf("wavm key cross-polluted by wasmer write: got %d, want %d", gotWavm, wavmVal)
	}
}

func TestWavmPrefixesIsOnlyWavm(t *testing.T) {
	prefixes := WavmPrefixes()
	if len(prefixes) != 1 {
		t.Fatalf("WavmPrefixes must return exactly one prefix, got %d", len(prefixes))
	}
	got := [WasmPrefixLen]byte{}
	copy(got[:], prefixes[0])
	if got != activatedAsmWavmPrefix {
		t.Fatalf("WavmPrefixes returned %v, want %v", got, activatedAsmWavmPrefix)
	}

	// And the Wasmer-version purge helper must NOT include the WAVM prefix —
	// the two version constants invalidate disjoint slices of the key space.
	for _, p := range WasmPrefixesExceptWavm() {
		if len(p) == WasmPrefixLen {
			var pp [WasmPrefixLen]byte
			copy(pp[:], p)
			if pp == activatedAsmWavmPrefix {
				t.Fatalf("WasmPrefixesExceptWavm must not contain activatedAsmWavmPrefix")
			}
		}
	}
}

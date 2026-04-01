package rawdb

import "testing"

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

func TestBaseTarget(t *testing.T) {
	tests := []struct {
		input    WasmTarget
		expected WasmTarget
	}{
		{TargetArm64Cranelift, TargetArm64},
		{TargetAmd64Cranelift, TargetAmd64},
		{TargetHostCranelift, TargetHost},
	}
	for _, tt := range tests {
		base, err := BaseTarget(tt.input)
		if err != nil {
			t.Fatalf("BaseTarget(%v) returned error: %v", tt.input, err)
		}
		if base != tt.expected {
			t.Fatalf("BaseTarget(%v) = %v, want %v", tt.input, base, tt.expected)
		}
	}

	// Non-cranelift targets should return an error.
	for _, target := range []WasmTarget{TargetArm64, TargetAmd64, TargetHost, TargetWasm} {
		_, err := BaseTarget(target)
		if err == nil {
			t.Fatalf("BaseTarget(%v) should return error for non-cranelift target", target)
		}
	}
}

func TestSplitAsmMap(t *testing.T) {
	asmMap := map[WasmTarget][]byte{
		TargetArm64:          []byte("sp"),
		TargetArm64Cranelift: []byte("cl"),
		TargetWasm:           []byte("wasm"),
		TargetWavm:           []byte("wavm"),
	}
	consensus, cranelift := SplitAsmMap(asmMap)

	if len(cranelift) != 1 {
		t.Fatalf("expected 1 cranelift entry, got %d", len(cranelift))
	}
	if string(cranelift[TargetArm64Cranelift]) != "cl" {
		t.Fatal("cranelift entry missing or wrong")
	}
	if len(consensus) != 3 {
		t.Fatalf("expected 3 consensus entries, got %d", len(consensus))
	}
	if string(consensus[TargetArm64]) != "sp" {
		t.Fatal("consensus entry for arm64 missing")
	}

	// Empty map.
	consensus, cranelift = SplitAsmMap(map[WasmTarget][]byte{})
	if len(consensus) != 0 || len(cranelift) != 0 {
		t.Fatal("expected empty maps for empty input")
	}
}

func TestDeduplicateAsmMap(t *testing.T) {
	// Both singlepass and cranelift exist: singlepass wins.
	asmMap := map[WasmTarget][]byte{
		TargetArm64:          []byte("singlepass"),
		TargetArm64Cranelift: []byte("cranelift"),
		TargetWavm:           []byte("wavm"),
	}
	result := DeduplicateAsmMap(asmMap)
	if string(result[TargetArm64]) != "singlepass" {
		t.Fatalf("expected singlepass to win, got %q", result[TargetArm64])
	}
	if string(result[TargetWavm]) != "wavm" {
		t.Fatal("wavm entry should be preserved")
	}
	if _, exists := result[TargetArm64Cranelift]; exists {
		t.Fatal("cranelift key should not appear in deduplicated map")
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}

	// Only cranelift exists: appears under base target key.
	asmMap = map[WasmTarget][]byte{
		TargetArm64Cranelift: []byte("cranelift-only"),
		TargetWasm:           []byte("wasm"),
	}
	result = DeduplicateAsmMap(asmMap)
	if string(result[TargetArm64]) != "cranelift-only" {
		t.Fatalf("expected cranelift under base key, got %q", result[TargetArm64])
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}

	// Only singlepass exists: no change.
	asmMap = map[WasmTarget][]byte{
		TargetAmd64: []byte("sp"),
	}
	result = DeduplicateAsmMap(asmMap)
	if string(result[TargetAmd64]) != "sp" {
		t.Fatalf("expected singlepass, got %q", result[TargetAmd64])
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}

	// Empty map.
	result = DeduplicateAsmMap(map[WasmTarget][]byte{})
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(result))
	}

	// Multiple targets, mixed: arm64 has both (singlepass wins), amd64 cranelift-only.
	asmMap = map[WasmTarget][]byte{
		TargetArm64:          []byte("arm-sp"),
		TargetArm64Cranelift: []byte("arm-cl"),
		TargetAmd64Cranelift: []byte("amd-cl"),
		TargetWavm:           []byte("wavm"),
	}
	result = DeduplicateAsmMap(asmMap)
	if string(result[TargetArm64]) != "arm-sp" {
		t.Fatalf("expected arm singlepass, got %q", result[TargetArm64])
	}
	if string(result[TargetAmd64]) != "amd-cl" {
		t.Fatalf("expected amd cranelift under base key, got %q", result[TargetAmd64])
	}
	if string(result[TargetWavm]) != "wavm" {
		t.Fatal("wavm entry should be preserved")
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
}

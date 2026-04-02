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

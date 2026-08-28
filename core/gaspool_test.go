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

package core

import (
	"errors"
	"math"
	"testing"
)

// TestGasPoolReportUnreservedGasCounts checks that reported-but-unreserved gas
// shows up in both used-gas accessors, since header.GasUsed is derived from
// Used() and receipt.CumulativeGasUsed from CumulativeUsed().
func TestGasPoolReportUnreservedGasCounts(t *testing.T) {
	gp := NewGasPool(1_000_000)
	if err := gp.ReportUnreservedGas(50_000); err != nil {
		t.Fatalf("ReportUnreservedGas: %v", err)
	}
	if got := gp.Used(); got != 50_000 {
		t.Errorf("Used() = %d, want 50000", got)
	}
	if got := gp.CumulativeUsed(); got != 50_000 {
		t.Errorf("CumulativeUsed() = %d, want 50000", got)
	}
	// The gas is reported, not reserved, so it stays available.
	if got := gp.Gas(); got != 1_000_000 {
		t.Errorf("Gas() = %d, want the pool to be undepleted (1000000)", got)
	}
}

// TestGasPoolReportUnreservedGasOversizedPool checks that a reported amount
// larger than the whole pool is accepted rather than rejected: the endTxNow tx
// is not reserving that gas, so the pool size does not bound it.
func TestGasPoolReportUnreservedGasOversizedPool(t *testing.T) {
	gp := NewGasPool(1 << 50)
	oversized := uint64(1<<50) + 1
	if err := gp.ReportUnreservedGas(oversized); err != nil {
		t.Fatalf("ReportUnreservedGas(%d) on a smaller pool: %v", oversized, err)
	}
	if got := gp.Used(); got != oversized {
		t.Errorf("Used() = %d, want %d", got, oversized)
	}
	if got := gp.Gas(); got != 1<<50 {
		t.Errorf("Gas() = %d, want the pool to be undepleted", got)
	}
}

func TestGasPoolReportUnreservedGasOverflow(t *testing.T) {
	gp := NewGasPool(1_000_000)
	if err := gp.ReportUnreservedGas(math.MaxUint64 - 10); err != nil {
		t.Fatalf("first report: %v", err)
	}
	err := gp.ReportUnreservedGas(100)
	if !errors.Is(err, ErrGasLimitOverflow) {
		t.Fatalf("overflowing report: got %v, want ErrGasLimitOverflow", err)
	}
	// The failed report must not have moved any counter.
	if got := gp.CumulativeUsed(); got != math.MaxUint64-10 {
		t.Errorf("CumulativeUsed() = %d, want the failed report to leave it unchanged", got)
	}
}

// TestGasPoolSnapshotRestoresUnreservedUsed guards the group-rollback path in
// arbos, which snapshots the pool before a tx and restores it if the tx (or a
// cascading redeem) is dropped.
func TestGasPoolSnapshotRestoresUnreservedUsed(t *testing.T) {
	gp := NewGasPool(1_000_000)
	if err := gp.ReportUnreservedGas(7_000); err != nil {
		t.Fatalf("ReportUnreservedGas: %v", err)
	}
	snapshot := gp.Snapshot()

	if err := gp.ReportUnreservedGas(11_000); err != nil {
		t.Fatalf("ReportUnreservedGas: %v", err)
	}
	if err := gp.CheckGasLegacy(3_000); err != nil {
		t.Fatalf("CheckGasLegacy: %v", err)
	}
	gp.Set(snapshot)

	if got := gp.Used(); got != 7_000 {
		t.Errorf("Used() after rollback = %d, want 7000", got)
	}
	if got := gp.CumulativeUsed(); got != 7_000 {
		t.Errorf("CumulativeUsed() after rollback = %d, want 7000", got)
	}
	if got := gp.Gas(); got != 1_000_000 {
		t.Errorf("Gas() after rollback = %d, want 1000000", got)
	}
}

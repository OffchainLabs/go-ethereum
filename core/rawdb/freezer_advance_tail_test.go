// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package rawdb

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/ethdb"
)

const truncateTailTestGroup = "testgroup"

var truncateTailTestTables = map[string]freezerTableConfig{
	"test": {noSnappy: true, tailGroup: truncateTailTestGroup},
}

func appendTruncateTailTestItems(t *testing.T, f ethdb.AncientStore, from, count uint64) {
	t.Helper()
	_, err := f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		for i := from; i < from+count; i++ {
			if err := op.AppendRaw("test", i, []byte(fmt.Sprintf("item-%d", i))); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkTruncateTailTestBounds(t *testing.T, f ethdb.AncientReader, tail, head uint64) {
	t.Helper()
	gotTail, err := f.Tail(truncateTailTestGroup)
	if err != nil {
		t.Fatal(err)
	}
	if gotTail != tail {
		t.Fatalf("unexpected tail: got %d want %d", gotTail, tail)
	}
	gotHead, err := f.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if gotHead != head {
		t.Fatalf("unexpected head: got %d want %d", gotHead, head)
	}
}

func checkTruncateTailTestItem(t *testing.T, f ethdb.AncientReader, item uint64) {
	t.Helper()
	blob, err := f.Ancient("test", item)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(blob, []byte(fmt.Sprintf("item-%d", item))) {
		t.Fatalf("unexpected item %d contents: %q", item, blob)
	}
}

func TestFreezerTruncateTailAboveHead(t *testing.T) {
	t.Parallel()

	f, dir := newFreezerForTesting(t, truncateTailTestTables)
	old, err := f.TruncateTail(truncateTailTestGroup, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if old != 0 {
		t.Fatalf("unexpected old tail: %d", old)
	}
	checkTruncateTailTestBounds(t, f, 1000, 1000)

	appendTruncateTailTestItems(t, f, 1000, 3)
	checkTruncateTailTestBounds(t, f, 1000, 1003)
	checkTruncateTailTestItem(t, f, 1000)
	checkTruncateTailTestItem(t, f, 1002)

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err = NewFreezer(dir, "", false, 2049, truncateTailTestTables)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	checkTruncateTailTestBounds(t, f, 1000, 1003)
	checkTruncateTailTestItem(t, f, 1001)
	appendTruncateTailTestItems(t, f, 1003, 1)
	checkTruncateTailTestItem(t, f, 1003)
}

func TestFreezerTruncateTailAboveHeadPersistsBeforeAppend(t *testing.T) {
	t.Parallel()

	f, dir := newFreezerForTesting(t, truncateTailTestTables)
	if _, err := f.TruncateTail(truncateTailTestGroup, 77); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := NewFreezer(dir, "", false, 2049, truncateTailTestTables)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	checkTruncateTailTestBounds(t, f, 77, 77)
	appendTruncateTailTestItems(t, f, 77, 2)
	checkTruncateTailTestItem(t, f, 78)
}

func TestFreezerTruncateTailAboveHeadResetsNonEmpty(t *testing.T) {
	t.Parallel()

	f, _ := newFreezerForTesting(t, truncateTailTestTables)
	defer f.Close()

	appendTruncateTailTestItems(t, f, 0, 2)
	if _, err := f.TruncateTail(truncateTailTestGroup, 10); err != nil {
		t.Fatal(err)
	}
	checkTruncateTailTestBounds(t, f, 10, 10)
	appendTruncateTailTestItems(t, f, 10, 1)
	checkTruncateTailTestItem(t, f, 10)
}

func TestFreezerTruncateTailAboveHeadAfterFullPrune(t *testing.T) {
	t.Parallel()

	f, dir := newFreezerForTesting(t, truncateTailTestTables)
	appendTruncateTailTestItems(t, f, 0, 4)
	if _, err := f.TruncateTail(truncateTailTestGroup, 4); err != nil {
		t.Fatal(err)
	}
	checkTruncateTailTestBounds(t, f, 4, 4)
	if _, err := f.TruncateTail(truncateTailTestGroup, 50); err != nil {
		t.Fatal(err)
	}
	checkTruncateTailTestBounds(t, f, 50, 50)
	appendTruncateTailTestItems(t, f, 50, 2)
	checkTruncateTailTestItem(t, f, 51)

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := NewFreezer(dir, "", false, 2049, truncateTailTestTables)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	checkTruncateTailTestBounds(t, f, 50, 52)
	checkTruncateTailTestItem(t, f, 50)
}

func TestFreezerTruncateTailAboveHeadNonPrunable(t *testing.T) {
	t.Parallel()

	f, _ := newFreezerForTesting(t, map[string]freezerTableConfig{
		"test":  {noSnappy: true, tailGroup: truncateTailTestGroup},
		"fixed": {noSnappy: true},
	})
	defer f.Close()

	if _, err := f.TruncateTail(truncateTailTestGroup, 10); err == nil {
		t.Fatal("expected error truncating tail above head of freezer with tables outside the group")
	}
}

func TestFreezerTruncateTailAboveHeadTooLarge(t *testing.T) {
	t.Parallel()

	f, _ := newFreezerForTesting(t, truncateTailTestTables)
	defer f.Close()

	if _, err := f.TruncateTail(truncateTailTestGroup, uint64(math.MaxUint32)+1); err == nil {
		t.Fatal("expected error truncating tail above uint32 offset limit")
	}
	checkTruncateTailTestBounds(t, f, 0, 0)
}

func TestMemoryFreezerTruncateTailAboveHead(t *testing.T) {
	t.Parallel()

	f := NewMemoryFreezer(false, truncateTailTestTables)
	defer f.Close()

	if _, err := f.TruncateTail(truncateTailTestGroup, 1000); err != nil {
		t.Fatal(err)
	}
	checkTruncateTailTestBounds(t, f, 1000, 1000)
	appendTruncateTailTestItems(t, f, 1000, 3)
	checkTruncateTailTestBounds(t, f, 1000, 1003)
	checkTruncateTailTestItem(t, f, 1002)

	if _, err := f.TruncateTail(truncateTailTestGroup, 1003); err != nil {
		t.Fatal(err)
	}
	if _, err := f.TruncateTail(truncateTailTestGroup, 2000); err != nil {
		t.Fatal(err)
	}
	checkTruncateTailTestBounds(t, f, 2000, 2000)
	appendTruncateTailTestItems(t, f, 2000, 1)
	checkTruncateTailTestItem(t, f, 2000)

	if _, err := f.TruncateTail(truncateTailTestGroup, 5000); err != nil {
		t.Fatal(err)
	}
	checkTruncateTailTestBounds(t, f, 5000, 5000)
	appendTruncateTailTestItems(t, f, 5000, 1)
	checkTruncateTailTestItem(t, f, 5000)
}

func TestFreezerTruncateTailAboveHeadMetadataAheadOfIndex(t *testing.T) {
	t.Parallel()

	f, dir := newFreezerForTesting(t, truncateTailTestTables)
	if _, err := f.TruncateTail(truncateTailTestGroup, 1000); err != nil {
		t.Fatal(err)
	}
	appendTruncateTailTestItems(t, f, 1000, 2)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	metaFile, err := openFreezerFileForAppend(filepath.Join(dir, "test.meta"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := newMetadata(metaFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.setVirtualTail(5000, true); err != nil {
		t.Fatal(err)
	}
	if err := metaFile.Close(); err != nil {
		t.Fatal(err)
	}

	f, err = NewFreezer(dir, "", false, 2049, truncateTailTestTables)
	if err != nil {
		t.Fatalf("freezer did not open after an interrupted tail reset: %v", err)
	}
	defer f.Close()

	tail, err := f.Tail(truncateTailTestGroup)
	if err != nil {
		t.Fatal(err)
	}
	head, err := f.Ancients()
	if err != nil {
		t.Fatal(err)
	}
	if tail != head {
		t.Fatalf("expected an empty window after winding the tail back, got tail %d head %d", tail, head)
	}
	appendTruncateTailTestItems(t, f, head, 1)
	checkTruncateTailTestItem(t, f, head)
}

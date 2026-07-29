// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package rawdb

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/ethdb"
)

var advanceTestTables = map[string]freezerTableConfig{
	"test": {noSnappy: true, prunable: true},
}

func appendAdvanceTestItems(t *testing.T, f ethdb.AncientStore, from, count uint64) {
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

func checkAdvanceTestBounds(t *testing.T, f ethdb.AncientReader, tail, head uint64) {
	t.Helper()
	gotTail, err := f.Tail()
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

func checkAdvanceTestItem(t *testing.T, f ethdb.AncientReader, item uint64) {
	t.Helper()
	blob, err := f.Ancient("test", item)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(blob, []byte(fmt.Sprintf("item-%d", item))) {
		t.Fatalf("unexpected item %d contents: %q", item, blob)
	}
}

func TestFreezerAdvanceEmptyTail(t *testing.T) {
	t.Parallel()

	f, dir := newFreezerForTesting(t, advanceTestTables)
	old, err := f.TruncateTail(1000)
	if err != nil {
		t.Fatal(err)
	}
	if old != 0 {
		t.Fatalf("unexpected old tail: %d", old)
	}
	checkAdvanceTestBounds(t, f, 1000, 1000)

	appendAdvanceTestItems(t, f, 1000, 3)
	checkAdvanceTestBounds(t, f, 1000, 1003)
	checkAdvanceTestItem(t, f, 1000)
	checkAdvanceTestItem(t, f, 1002)

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err = NewFreezer(dir, "", false, 2049, advanceTestTables)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	checkAdvanceTestBounds(t, f, 1000, 1003)
	checkAdvanceTestItem(t, f, 1001)
	appendAdvanceTestItems(t, f, 1003, 1)
	checkAdvanceTestItem(t, f, 1003)
}

func TestFreezerAdvanceEmptyTailPersistsBeforeAppend(t *testing.T) {
	t.Parallel()

	f, dir := newFreezerForTesting(t, advanceTestTables)
	if _, err := f.TruncateTail(77); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := NewFreezer(dir, "", false, 2049, advanceTestTables)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	checkAdvanceTestBounds(t, f, 77, 77)
	appendAdvanceTestItems(t, f, 77, 2)
	checkAdvanceTestItem(t, f, 78)
}

func TestFreezerAdvanceEmptyTailNonEmpty(t *testing.T) {
	t.Parallel()

	f, _ := newFreezerForTesting(t, advanceTestTables)
	defer f.Close()

	appendAdvanceTestItems(t, f, 0, 2)
	if _, err := f.TruncateTail(10); err == nil {
		t.Fatal("expected error advancing tail of non-empty freezer")
	}
	checkAdvanceTestBounds(t, f, 0, 2)
}

func TestFreezerAdvanceEmptyTailAfterFullPrune(t *testing.T) {
	t.Parallel()

	f, dir := newFreezerForTesting(t, advanceTestTables)
	appendAdvanceTestItems(t, f, 0, 4)
	if _, err := f.TruncateTail(4); err != nil {
		t.Fatal(err)
	}
	checkAdvanceTestBounds(t, f, 4, 4)
	if _, err := f.TruncateTail(50); err != nil {
		t.Fatal(err)
	}
	checkAdvanceTestBounds(t, f, 50, 50)
	appendAdvanceTestItems(t, f, 50, 2)
	checkAdvanceTestItem(t, f, 51)

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := NewFreezer(dir, "", false, 2049, advanceTestTables)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	checkAdvanceTestBounds(t, f, 50, 52)
	checkAdvanceTestItem(t, f, 50)
}

func TestFreezerAdvanceEmptyTailNonPrunable(t *testing.T) {
	t.Parallel()

	f, _ := newFreezerForTesting(t, map[string]freezerTableConfig{
		"test": {noSnappy: true, prunable: false},
	})
	defer f.Close()

	if _, err := f.TruncateTail(10); err == nil {
		t.Fatal("expected error advancing tail of freezer with non-prunable table")
	}
}

func TestMemoryFreezerAdvanceEmptyTail(t *testing.T) {
	t.Parallel()

	f := NewMemoryFreezer(false, advanceTestTables)
	defer f.Close()

	if _, err := f.TruncateTail(1000); err != nil {
		t.Fatal(err)
	}
	checkAdvanceTestBounds(t, f, 1000, 1000)
	appendAdvanceTestItems(t, f, 1000, 3)
	checkAdvanceTestBounds(t, f, 1000, 1003)
	checkAdvanceTestItem(t, f, 1002)

	if _, err := f.TruncateTail(1003); err != nil {
		t.Fatal(err)
	}
	if _, err := f.TruncateTail(2000); err != nil {
		t.Fatal(err)
	}
	checkAdvanceTestBounds(t, f, 2000, 2000)
	appendAdvanceTestItems(t, f, 2000, 1)
	checkAdvanceTestItem(t, f, 2000)

	if _, err := f.TruncateTail(5000); err == nil {
		t.Fatal("expected error advancing tail of non-empty freezer")
	}
}

func TestFreezerAdvanceEmptyTailMetadataAheadOfIndex(t *testing.T) {
	t.Parallel()

	f, dir := newFreezerForTesting(t, advanceTestTables)
	if _, err := f.TruncateTail(1000); err != nil {
		t.Fatal(err)
	}
	appendAdvanceTestItems(t, f, 1000, 2)
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

	f, err = NewFreezer(dir, "", false, 2049, advanceTestTables)
	if err != nil {
		t.Fatalf("freezer did not open after an interrupted tail advance: %v", err)
	}
	defer f.Close()

	tail, err := f.Tail()
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
	appendAdvanceTestItems(t, f, head, 1)
	checkAdvanceTestItem(t, f, head)
}

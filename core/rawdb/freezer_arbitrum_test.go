package rawdb

import (
	"bytes"
	"fmt"
	"maps"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/stretchr/testify/require"
)

const laggingBALItems, laggingChainItems = uint64(5), uint64(12)

var laggingTestPayload = bytes.Repeat([]byte{0xab}, 64)

func optionalBALConfigs() map[string]freezerTableConfig {
	tables := maps.Clone(chainFreezerTableConfigs)
	config := tables[ChainFreezerBALTable]
	config.optional = true
	tables[ChainFreezerBALTable] = config
	return tables
}

func appendChainItems(t *testing.T, f *Freezer, tables map[string]freezerTableConfig, from, to uint64, balPayload []byte) {
	t.Helper()
	_, err := f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		for i := from; i < to; i++ {
			for name := range tables {
				item := laggingTestPayload
				if name == ChainFreezerBALTable {
					item = balPayload
				}
				if err := op.AppendRaw(name, i, item); err != nil {
					return err
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
}

func writeLaggingBAL(t *testing.T, dir string, balTail uint64, balPayload []byte) {
	t.Helper()
	f, err := NewFreezer(dir, "", false, 2049, chainFreezerTableConfigs)
	require.NoError(t, err)
	appendChainItems(t, f, chainFreezerTableConfigs, 0, laggingBALItems, balPayload)
	_, err = f.TruncateTail(ChainFreezerBALGroup, balTail)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	withoutBAL := maps.Clone(chainFreezerTableConfigs)
	delete(withoutBAL, ChainFreezerBALTable)
	f, err = NewFreezer(dir, "", false, 2049, withoutBAL)
	require.NoError(t, err)
	appendChainItems(t, f, withoutBAL, laggingBALItems, laggingChainItems, nil)
	require.NoError(t, f.Close())
}

func TestFreezerResetsLaggingOptionalTable(t *testing.T) {
	for _, tc := range []struct {
		name       string
		balTail    uint64
		balPayload []byte
	}{
		{"nil entries", 0, nil},
		{"nil entries above a pruned tail", 3, nil},
		{"non-empty entries", 0, laggingTestPayload},
		{"non-empty entries all pruned", laggingBALItems, laggingTestPayload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLaggingBAL(t, dir, tc.balTail, tc.balPayload)
			f, err := NewFreezer(dir, "", false, 2049, optionalBALConfigs())
			require.NoError(t, err)
			defer f.Close()

			head, err := f.Ancients()
			require.NoError(t, err)
			require.Equal(t, laggingChainItems, head)
			got, err := f.Ancient(ChainFreezerBodiesTable, laggingChainItems-1)
			require.NoError(t, err)
			require.Equal(t, laggingTestPayload, got)
			tail, err := f.Tail(ChainFreezerBALGroup)
			require.NoError(t, err)
			require.Equal(t, laggingChainItems, tail)

			appendChainItems(t, f, chainFreezerTableConfigs, laggingChainItems, laggingChainItems+1, laggingTestPayload)
			got, err = f.Ancient(ChainFreezerBALTable, laggingChainItems)
			require.NoError(t, err)
			require.Equal(t, laggingTestPayload, got)
		})
	}
}

func TestFreezerTruncatesToLaggingRequiredBAL(t *testing.T) {
	dir := t.TempDir()
	writeLaggingBAL(t, dir, 0, nil)
	f, err := NewFreezer(dir, "", false, 2049, chainFreezerTableConfigs)
	require.NoError(t, err)
	defer f.Close()

	head, err := f.Ancients()
	require.NoError(t, err)
	require.Equal(t, laggingBALItems, head)
}

func TestFreezerOptionalTableDoesNotSetHead(t *testing.T) {
	for _, balTail := range []uint64{0, 2} {
		t.Run(fmt.Sprintf("tail %d", balTail), func(t *testing.T) {
			dir := t.TempDir()
			onlyBAL := map[string]freezerTableConfig{ChainFreezerBALTable: chainFreezerTableConfigs[ChainFreezerBALTable]}
			f, err := NewFreezer(dir, "", false, 2049, onlyBAL)
			require.NoError(t, err)
			appendChainItems(t, f, onlyBAL, 0, 3, laggingTestPayload)
			_, err = f.TruncateTail(ChainFreezerBALGroup, balTail)
			require.NoError(t, err)
			require.NoError(t, f.Close())

			f, err = NewFreezer(dir, "", false, 2049, optionalBALConfigs())
			require.NoError(t, err)
			defer f.Close()
			head, err := f.Ancients()
			require.NoError(t, err)
			require.Zero(t, head)
		})
	}
}

func TestOpenResetsLaggingBALOnceSetOptional(t *testing.T) {
	saved := maps.Clone(chainFreezerTableConfigs)
	t.Cleanup(func() { chainFreezerTableConfigs = saved })
	require.NoError(t, SetChainFreezerTableOptional(ChainFreezerBALTable))

	ancient := t.TempDir()
	writeLaggingBAL(t, filepath.Join(ancient, ChainFreezerName), 0, nil)
	db, err := Open(memorydb.New(), OpenOptions{Ancient: ancient})
	require.NoError(t, err)
	defer db.Close()

	head, err := db.Ancients()
	require.NoError(t, err)
	require.Equal(t, laggingChainItems, head)
	got, err := db.Ancient(ChainFreezerBodiesTable, laggingChainItems-1)
	require.NoError(t, err)
	require.Equal(t, laggingTestPayload, got)
}

func TestSetChainFreezerTableOptional(t *testing.T) {
	saved := maps.Clone(chainFreezerTableConfigs)
	t.Cleanup(func() { chainFreezerTableConfigs = saved })

	require.ErrorContains(t, SetChainFreezerTableOptional("unknown"), "unknown chain freezer table")
	require.ErrorContains(t, SetChainFreezerTableOptional(ChainFreezerHeaderTable), "not prunable")
	require.ErrorContains(t, SetChainFreezerTableOptional(ChainFreezerBodiesTable), "shares its tail group")
	require.NoError(t, SetChainFreezerTableOptional(ChainFreezerBALTable))
	require.True(t, chainFreezerTableConfigs[ChainFreezerBALTable].optional)
}

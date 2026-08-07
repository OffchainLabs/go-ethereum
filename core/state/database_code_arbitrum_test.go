package state

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
)

func TestArbNodeConfigSetOnce(t *testing.T) {
	db := NewCodeDB(rawdb.NewMemoryDatabase())
	want := new(int)

	db.SetArbNodeConfig(want)
	if got := db.ArbNodeConfig(); got != want {
		t.Fatalf("unexpected ArbNodeConfig: got %v, want %v", got, want)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("second SetArbNodeConfig call did not panic")
		}
	}()
	db.SetArbNodeConfig(want)
}

func TestArbNodeConfigNilCountsAsSet(t *testing.T) {
	db := NewCodeDB(rawdb.NewMemoryDatabase())

	db.SetArbNodeConfig(nil)
	if got := db.ArbNodeConfig(); got != nil {
		t.Fatalf("unexpected ArbNodeConfig: got %v, want nil", got)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("SetArbNodeConfig after setting nil did not panic")
		}
	}()
	db.SetArbNodeConfig(new(int))
}

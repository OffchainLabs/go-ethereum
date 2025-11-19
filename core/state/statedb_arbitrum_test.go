// Copyright 2016 The go-ethereum Authors
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

package state

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
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

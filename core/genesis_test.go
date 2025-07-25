// Copyright 2017 The go-ethereum Authors
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
	"bytes"
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

func TestSetupGenesis(t *testing.T) {
	testSetupGenesis(t, rawdb.HashScheme)
	testSetupGenesis(t, rawdb.PathScheme)
}

func testSetupGenesis(t *testing.T, scheme string) {
	var (
		customghash = common.HexToHash("0x89c99d90b79719238d2645c7642f2c9295246e80775b38cfd162b696817fbd50")
		customg     = Genesis{
			Config: &params.ChainConfig{HomesteadBlock: big.NewInt(3), Ethash: &params.EthashConfig{}},
			Alloc: types.GenesisAlloc{
				{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			},
		}
		oldcustomg = customg
	)
	oldcustomg.Config = &params.ChainConfig{HomesteadBlock: big.NewInt(2), Ethash: &params.EthashConfig{}}

	tests := []struct {
		name           string
		fn             func(ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error)
		wantConfig     *params.ChainConfig
		wantHash       common.Hash
		wantErr        error
		wantCompactErr *params.ConfigCompatError
	}{
		{
			name: "genesis without ChainConfig",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				return SetupGenesisBlock(db, triedb.NewDatabase(db, newDbConfig(scheme)), new(Genesis))
			},
			wantErr: errGenesisNoConfig,
		},
		{
			name: "no block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				return SetupGenesisBlock(db, triedb.NewDatabase(db, newDbConfig(scheme)), nil)
			},
			wantHash:   params.MainnetGenesisHash,
			wantConfig: params.MainnetChainConfig,
		},
		{
			name: "mainnet block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				DefaultGenesisBlock().MustCommit(db, triedb.NewDatabase(db, newDbConfig(scheme)))
				return SetupGenesisBlock(db, triedb.NewDatabase(db, newDbConfig(scheme)), nil)
			},
			wantHash:   params.MainnetGenesisHash,
			wantConfig: params.MainnetChainConfig,
		},
		{
			name: "custom block in DB, genesis == nil",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				tdb := triedb.NewDatabase(db, newDbConfig(scheme))
				customg.Commit(db, tdb)
				return SetupGenesisBlock(db, tdb, nil)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "custom block in DB, genesis == sepolia",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				tdb := triedb.NewDatabase(db, newDbConfig(scheme))
				customg.Commit(db, tdb)
				return SetupGenesisBlock(db, tdb, DefaultSepoliaGenesisBlock())
			},
			wantErr: &GenesisMismatchError{Stored: customghash, New: params.SepoliaGenesisHash},
		},
		{
			name: "custom block in DB, genesis == hoodi",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				tdb := triedb.NewDatabase(db, newDbConfig(scheme))
				customg.Commit(db, tdb)
				return SetupGenesisBlock(db, tdb, DefaultHoodiGenesisBlock())
			},
			wantErr: &GenesisMismatchError{Stored: customghash, New: params.HoodiGenesisHash},
		},
		{
			name: "compatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				tdb := triedb.NewDatabase(db, newDbConfig(scheme))
				oldcustomg.Commit(db, tdb)
				return SetupGenesisBlock(db, tdb, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
		},
		{
			name: "incompatible config in DB",
			fn: func(db ethdb.Database) (*params.ChainConfig, common.Hash, *params.ConfigCompatError, error) {
				// Commit the 'old' genesis block with Homestead transition at #2.
				// Advance to block #4, past the homestead transition block of customg.
				tdb := triedb.NewDatabase(db, newDbConfig(scheme))
				oldcustomg.Commit(db, tdb)

				bc, _ := NewBlockChain(db, DefaultCacheConfigWithScheme(scheme), nil, &oldcustomg, nil, ethash.NewFullFaker(), vm.Config{}, nil)
				defer bc.Stop()

				_, blocks, _ := GenerateChainWithGenesis(&oldcustomg, ethash.NewFaker(), 4, nil)
				bc.InsertChain(blocks)

				// This should return a compatibility error.
				return SetupGenesisBlock(db, tdb, &customg)
			},
			wantHash:   customghash,
			wantConfig: customg.Config,
			wantCompactErr: &params.ConfigCompatError{
				What:          "Homestead fork block",
				StoredBlock:   big.NewInt(2),
				NewBlock:      big.NewInt(3),
				RewindToBlock: 1,
			},
		},
	}

	for _, test := range tests {
		db := rawdb.NewMemoryDatabase()
		config, hash, compatErr, err := test.fn(db)
		// Check the return values.
		if !reflect.DeepEqual(err, test.wantErr) {
			spew := spew.ConfigState{DisablePointerAddresses: true, DisableCapacities: true}
			t.Errorf("%s: returned error %#v, want %#v", test.name, spew.NewFormatter(err), spew.NewFormatter(test.wantErr))
		}
		if !reflect.DeepEqual(compatErr, test.wantCompactErr) {
			spew := spew.ConfigState{DisablePointerAddresses: true, DisableCapacities: true}
			t.Errorf("%s: returned error %#v, want %#v", test.name, spew.NewFormatter(compatErr), spew.NewFormatter(test.wantCompactErr))
		}
		if !reflect.DeepEqual(config, test.wantConfig) {
			t.Errorf("%s:\nreturned %v\nwant     %v", test.name, config, test.wantConfig)
		}
		if hash != test.wantHash {
			t.Errorf("%s: returned hash %s, want %s", test.name, hash.Hex(), test.wantHash.Hex())
		} else if err == nil {
			// Check database content.
			stored := rawdb.ReadBlock(db, test.wantHash, 0)
			if stored.Hash() != test.wantHash {
				t.Errorf("%s: block in DB has hash %s, want %s", test.name, stored.Hash(), test.wantHash)
			}
		}
	}
}

// TestGenesisHashes checks the congruity of default genesis data to
// corresponding hardcoded genesis hash values.
func TestGenesisHashes(t *testing.T) {
	for i, c := range []struct {
		genesis *Genesis
		want    common.Hash
	}{
		{DefaultGenesisBlock(), params.MainnetGenesisHash},
		{DefaultSepoliaGenesisBlock(), params.SepoliaGenesisHash},
		{DefaultHoleskyGenesisBlock(), params.HoleskyGenesisHash},
		{DefaultHoodiGenesisBlock(), params.HoodiGenesisHash},
	} {
		// Test via MustCommit
		db := rawdb.NewMemoryDatabase()
		if have := c.genesis.MustCommit(db, triedb.NewDatabase(db, triedb.HashDefaults)).Hash(); have != c.want {
			t.Errorf("case: %d a), want: %s, got: %s", i, c.want.Hex(), have.Hex())
		}
		// Test via ToBlock
		if have := c.genesis.ToBlock().Hash(); have != c.want {
			t.Errorf("case: %d a), want: %s, got: %s", i, c.want.Hex(), have.Hex())
		}
	}
}

func TestGenesisCommit(t *testing.T) {
	genesis := &Genesis{
		BaseFee: big.NewInt(params.InitialBaseFee),
		Config:  params.TestChainConfig,
		// difficulty is nil
	}

	db := rawdb.NewMemoryDatabase()
	genesisBlock := genesis.MustCommit(db, triedb.NewDatabase(db, triedb.HashDefaults))

	if genesis.Difficulty != nil {
		t.Fatalf("assumption wrong")
	}

	// This value should have been set as default in the ToBlock method.
	if genesisBlock.Difficulty().Cmp(params.GenesisDifficulty) != 0 {
		t.Errorf("assumption wrong: want: %d, got: %v", params.GenesisDifficulty, genesisBlock.Difficulty())
	}
}

func TestReadWriteGenesisAlloc(t *testing.T) {
	var (
		db    = rawdb.NewMemoryDatabase()
		alloc = &types.GenesisAlloc{
			{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
			{2}: {Balance: big.NewInt(2), Storage: map[common.Hash]common.Hash{{2}: {2}}},
		}
		hash, _ = hashAlloc(alloc, false)
	)
	blob, _ := json.Marshal(alloc)
	rawdb.WriteGenesisStateSpec(db, hash, blob)

	var reload types.GenesisAlloc
	err := reload.UnmarshalJSON(rawdb.ReadGenesisStateSpec(db, hash))
	if err != nil {
		t.Fatalf("Failed to load genesis state %v", err)
	}
	if len(reload) != len(*alloc) {
		t.Fatal("Unexpected genesis allocation")
	}
	for addr, account := range reload {
		want, ok := (*alloc)[addr]
		if !ok {
			t.Fatal("Account is not found")
		}
		if !reflect.DeepEqual(want, account) {
			t.Fatal("Unexpected account")
		}
	}
}

func newDbConfig(scheme string) *triedb.Config {
	if scheme == rawdb.HashScheme {
		return triedb.HashDefaults
	}
	return &triedb.Config{PathDB: pathdb.Defaults}
}

func TestVerkleGenesisCommit(t *testing.T) {
	var verkleTime uint64 = 0
	verkleConfig := &params.ChainConfig{
		ChainID:                 big.NewInt(1),
		HomesteadBlock:          big.NewInt(0),
		DAOForkBlock:            nil,
		DAOForkSupport:          false,
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		MuirGlacierBlock:        big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		ArrowGlacierBlock:       big.NewInt(0),
		GrayGlacierBlock:        big.NewInt(0),
		MergeNetsplitBlock:      nil,
		ShanghaiTime:            &verkleTime,
		CancunTime:              &verkleTime,
		PragueTime:              &verkleTime,
		OsakaTime:               &verkleTime,
		VerkleTime:              &verkleTime,
		TerminalTotalDifficulty: big.NewInt(0),
		EnableVerkleAtGenesis:   true,
		Ethash:                  nil,
		Clique:                  nil,
		BlobScheduleConfig: &params.BlobScheduleConfig{
			Cancun: params.DefaultCancunBlobConfig,
			Prague: params.DefaultPragueBlobConfig,
			Osaka:  params.DefaultOsakaBlobConfig,
			Verkle: params.DefaultPragueBlobConfig,
		},
	}

	genesis := &Genesis{
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Config:     verkleConfig,
		Timestamp:  verkleTime,
		Difficulty: big.NewInt(0),
		Alloc: types.GenesisAlloc{
			{1}: {Balance: big.NewInt(1), Storage: map[common.Hash]common.Hash{{1}: {1}}},
		},
	}

	expected := common.FromHex("018d20eebb130b5e2b796465fe36aafab650650729a92435aec071bf2386f080")
	got := genesis.ToBlock().Root().Bytes()
	if !bytes.Equal(got, expected) {
		t.Fatalf("invalid genesis state root, expected %x, got %x", expected, got)
	}

	db := rawdb.NewMemoryDatabase()
	triedb := triedb.NewDatabase(db, triedb.VerkleDefaults)
	block := genesis.MustCommit(db, triedb)
	if !bytes.Equal(block.Root().Bytes(), expected) {
		t.Fatalf("invalid genesis state root, expected %x, got %x", expected, block.Root())
	}

	// Test that the trie is verkle
	if !triedb.IsVerkle() {
		t.Fatalf("expected trie to be verkle")
	}
	vdb := rawdb.NewTable(db, string(rawdb.VerklePrefix))
	if !rawdb.HasAccountTrieNode(vdb, nil) {
		t.Fatal("could not find node")
	}
}

// Copyright 2021 The go-ethereum Authors
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

package pruner

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/harbour-tech/go-ethereum-arbitrum/common"
	"github.com/harbour-tech/go-ethereum-arbitrum/core/rawdb"
	"github.com/harbour-tech/go-ethereum-arbitrum/log"
	"github.com/harbour-tech/go-ethereum-arbitrum/rlp"
	bloomfilter "github.com/holiman/bloomfilter/v2"
)

// stateBloomHash is used to convert a trie hash or contract code hash into a 64 bit mini hash.
func stateBloomHash(f []byte) uint64 {
	return binary.BigEndian.Uint64(f)
}

// stateBloom is a bloom filter used during the state conversion(snapshot->state).
// The keys of all generated entries will be recorded here so that in the pruning
// stage the entries belong to the specific version can be avoided for deletion.
//
// The false-positive is allowed here. The "false-positive" entries means they
// actually don't belong to the specific version but they are not deleted in the
// pruning. The downside of the false-positive allowance is we may leave some "dangling"
// nodes in the disk. But in practice the it's very unlike the dangling node is
// state root. So in theory this pruned state shouldn't be visited anymore. Another
// potential issue is for fast sync. If we do another fast sync upon the pruned
// database, it's problematic which will stop the expansion during the syncing.
// TODO address it @rjl493456442 @holiman @karalabe.
//
// After the entire state is generated, the bloom filter should be persisted into
// the disk. It indicates the whole generation procedure is finished.
type stateBloom struct {
	bloom *bloomfilter.Filter
}

// newStateBloomWithSize creates a brand new state bloom for state generation.
// The bloom filter will be created by the passing bloom filter size. According
// to the https://hur.st/bloomfilter/?n=600000000&p=&m=2048MB&k=4, the parameters
// are picked so that the false-positive rate for mainnet is low enough.
func newStateBloomWithSize(size uint64) (*stateBloom, error) {
	bloom, err := bloomfilter.New(size*1024*1024*8, 4)
	if err != nil {
		return nil, err
	}
	log.Info("Initialized state bloom", "size", common.StorageSize(float64(bloom.M()/8)))
	return &stateBloom{bloom: bloom}, nil
}

// NewStateBloomFromDisk loads the state bloom from the given file.
// In this case the assumption is held the bloom filter is complete.
func NewStateBloomFromDisk(filename string) (*stateBloom, []common.Hash, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	version := []byte{0}
	_, err = io.ReadFull(r, version)
	if err != nil {
		return nil, nil, err
	}
	if version[0] != 0 {
		return nil, nil, fmt.Errorf("unknown state bloom filter version %v", version[0])
	}
	var roots []common.Hash
	err = rlp.Decode(r, &roots)
	if err != nil {
		return nil, nil, err
	}
	bloom, _, err := bloomfilter.ReadFrom(r)
	if err != nil {
		return nil, nil, err
	}
	return &stateBloom{bloom: bloom}, roots, nil
}

// Commit flushes the bloom filter content into the disk and marks the bloom
// as complete.
func (bloom *stateBloom) Commit(filename, tempname string, roots []common.Hash) error {
	f, err := os.OpenFile(tempname, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte{0}) // version
	if err != nil {
		return err
	}
	err = rlp.Encode(f, roots)
	if err != nil {
		return err
	}
	// Write the bloom out into a temporary file
	_, err = bloom.bloom.WriteTo(f)
	if err != nil {
		return err
	}
	// Ensure the file is synced to disk
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()

	// Move the temporary file into it's final location
	return os.Rename(tempname, filename)
}

// Put implements the KeyValueWriter interface. But here only the key is needed.
func (bloom *stateBloom) Put(key []byte, value []byte) error {
	// If the key length is not 32bytes, ensure it's contract code
	// entry with new scheme.
	if len(key) != common.HashLength {
		isCode, codeKey := rawdb.IsCodeKey(key)
		if !isCode {
			return errors.New("invalid entry")
		}
		bloom.bloom.AddHash(stateBloomHash(codeKey))
		return nil
	}
	bloom.bloom.AddHash(stateBloomHash(key))
	return nil
}

// Delete removes the key from the key-value data store.
func (bloom *stateBloom) Delete(key []byte) error { panic("not supported") }

// Contain is the wrapper of the underlying contains function which
// reports whether the key is contained.
// - If it says yes, the key may be contained
// - If it says no, the key is definitely not contained.
func (bloom *stateBloom) Contain(key []byte) bool {
	return bloom.bloom.ContainsHash(stateBloomHash(key))
}

func (bloom *stateBloom) FalsePosititveProbability() float64 {
	return bloom.bloom.FalsePosititveProbability()
}

func (bloom *stateBloom) Size() uint64 {
	return bloom.bloom.M()
}

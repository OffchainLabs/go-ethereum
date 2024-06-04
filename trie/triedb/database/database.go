// Copyright 2024 The go-ethereum Authors
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

package database

import (
	"github.com/ethereum/go-ethereum/common"
)

// Reader wraps the Node method of a backing trie store.
type Reader interface {
	// Node retrieves the trie node blob with the provided trie identifier, node path and
	// the corresponding node hash. No error will be returned if the node is not found.
	//
	// When looking up nodes in the account trie, 'owner' is the zero hash. For contract
	// storage trie nodes, 'owner' is the hash of the account address that containing the
	// storage.
	//
	// TODO(rjl493456442): remove the 'hash' parameter, it's redundant in PBSS.
	Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error)
}

type readerWithRecording struct {
	reader          Reader
	fallbackReader  Reader
	accessedEntries map[common.Hash][]byte
}

func ReaderWithRecording(
	reader Reader,
	fallbackReader Reader,
	accessedEntries map[common.Hash][]byte,
) *readerWithRecording {
	return &readerWithRecording{
		reader:          reader,
		fallbackReader:  fallbackReader,
		accessedEntries: accessedEntries,
	}
}

func (r *readerWithRecording) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	blob, err := r.reader.Node(owner, path, hash)
	if err != nil {
		return nil, err
	}
	if len(blob) == 0 {
		blob, err = r.fallbackReader.Node(owner, path, hash)
		if err != nil {
			return nil, err
		}
		r.accessedEntries[hash] = blob
	}
	return blob, err
}

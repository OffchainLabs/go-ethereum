// Copyright 2014 The go-ethereum Authors
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

// Package ethdb defines the interfaces for an Ethereum data store.
package ethdb

import (
	"errors"
	"io"
)

// KeyValueReader wraps the Has and Get method of a backing data store.
type KeyValueReader interface {
	// Has retrieves if a key is present in the key-value data store.
	Has(key []byte) (bool, error)

	// Get retrieves the given key if it's present in the key-value data store.
	Get(key []byte) ([]byte, error)
}

// KeyValueWriter wraps the Put method of a backing data store.
type KeyValueWriter interface {
	// Put inserts the given value into the key-value data store.
	Put(key []byte, value []byte) error

	// Delete removes the key from the key-value data store.
	Delete(key []byte) error
}

var ErrTooManyKeys = errors.New("too many keys in deleted range")

// KeyValueRangeDeleter wraps the DeleteRange method of a backing data store.
type KeyValueRangeDeleter interface {
	// DeleteRange deletes all of the keys (and values) in the range [start,end)
	// (inclusive on start, exclusive on end).
	// Some implementations of DeleteRange may return ErrTooManyKeys after
	// partially deleting entries in the given range.
	DeleteRange(start, end []byte) error
}

// KeyValueStater wraps the Stat method of a backing data store.
type KeyValueStater interface {
	// Stat returns the statistic data of the database.
	Stat() (string, error)
}

// Compacter wraps the Compact method of a backing data store.
type Compacter interface {
	// Compact flattens the underlying data store for the given key range. In essence,
	// deleted and overwritten versions are discarded, and the data is rearranged to
	// reduce the cost of operations needed to access them.
	//
	// A nil start is treated as a key before all keys in the data store; a nil limit
	// is treated as a key after all keys in the data store. If both is nil then it
	// will compact entire data store.
	Compact(start []byte, limit []byte) error
}

// KeyValueStore contains all the methods required to allow handling different
// key-value data stores backing the high level database.
type KeyValueStore interface {
	KeyValueReader
	KeyValueWriter
	KeyValueStater
	KeyValueRangeDeleter
	Batcher
	Iteratee
	Compacter
	io.Closer
}

// AncientReaderOp contains the methods required to read from immutable ancient data.
type AncientReaderOp interface {
	// HasAncient returns an indicator whether the specified data exists in the
	// ancient store.
	HasAncient(kind string, number uint64) (bool, error)

	// Ancient retrieves an ancient binary blob from the append-only immutable files.
	Ancient(kind string, number uint64) ([]byte, error)

	// AncientRange retrieves multiple items in sequence, starting from the index 'start'.
	// It will return
	//   - at most 'count' items,
	//   - if maxBytes is specified: at least 1 item (even if exceeding the maxByteSize),
	//     but will otherwise return as many items as fit into maxByteSize.
	//   - if maxBytes is not specified, 'count' items will be returned if they are present
	AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error)

	// Ancients returns the ancient item numbers in the ancient store.
	Ancients() (uint64, error)

	// Tail returns the number of first stored item in the ancient store.
	// This number can also be interpreted as the total deleted items.
	Tail() (uint64, error)

	// AncientSize returns the ancient size of the specified category.
	AncientSize(kind string) (uint64, error)
}

// AncientReader is the extended ancient reader interface including 'batched' or 'atomic' reading.
type AncientReader interface {
	AncientReaderOp

	// ReadAncients runs the given read operation while ensuring that no writes take place
	// on the underlying ancient store.
	ReadAncients(fn func(AncientReaderOp) error) (err error)
}

// AncientWriter contains the methods required to write to immutable ancient data.
type AncientWriter interface {
	// ModifyAncients runs a write operation on the ancient store.
	// If the function returns an error, any changes to the underlying store are reverted.
	// The integer return value is the total size of the written data.
	ModifyAncients(func(AncientWriteOp) error) (int64, error)

	// TruncateHead discards all but the first n ancient data from the ancient store.
	// After the truncation, the latest item can be accessed it item_n-1(start from 0).
	TruncateHead(n uint64) (uint64, error)

	// TruncateTail discards the first n ancient data from the ancient store. The already
	// deleted items are ignored. After the truncation, the earliest item can be accessed
	// is item_n(start from 0). The deleted items may not be removed from the ancient store
	// immediately, but only when the accumulated deleted data reach the threshold then
	// will be removed all together.
	//
	// Note that data marked as non-prunable will still be retained and remain accessible.
	TruncateTail(n uint64) (uint64, error)

	// Sync flushes all in-memory ancient store data to disk.
	Sync() error
}

// AncientWriteOp is given to the function argument of ModifyAncients.
type AncientWriteOp interface {
	// Append adds an RLP-encoded item.
	Append(kind string, number uint64, item interface{}) error

	// AppendRaw adds an item without RLP-encoding it.
	AppendRaw(kind string, number uint64, item []byte) error
}

// AncientStater wraps the Stat method of a backing ancient store.
type AncientStater interface {
	// AncientDatadir returns the path of the ancient store directory.
	//
	// If the ancient store is not activated, an error is returned.
	// If an ephemeral ancient store is used, an empty path is returned.
	//
	// The path returned by AncientDatadir can be used as the root path
	// of the ancient store to construct paths for other sub ancient stores.
	AncientDatadir() (string, error)
}

// Reader contains the methods required to read data from both key-value as well as
// immutable ancient data.
type Reader interface {
	KeyValueReader
	AncientReader
}

// AncientStore contains all the methods required to allow handling different
// ancient data stores backing immutable data store.
type AncientStore interface {
	AncientReader
	AncientWriter
	AncientStater
	io.Closer
}

type WasmDataBaseRetriever interface {
	WasmDataBase() KeyValueStore
}

// ResettableAncientStore extends the AncientStore interface by adding a Reset method.
type ResettableAncientStore interface {
	AncientStore

	// Reset is designed to reset the entire ancient store to its default state.
	Reset() error
}

// Database contains all the methods required by the high level database to not
// only access the key-value data store but also the ancient chain store.
type Database interface {
	KeyValueStore
	AncientStore
	WasmDataBaseRetriever
}

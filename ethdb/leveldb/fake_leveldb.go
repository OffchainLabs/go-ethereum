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

//go:build wasm
// +build wasm

// Package leveldb implements the key-value database layer based on LevelDB.
package leveldb

import (
	"errors"

	"github.com/ethereum/go-ethereum/ethdb"
)

type Database struct {
	unconstructable struct{}
}

// New returns a wrapped LevelDB object. The namespace is the prefix that the
// metrics reporting should use for surfacing internal stats.
func New(file string, cache int, handles int, namespace string, readonly bool) (*Database, error) {
	return nil, errors.New("leveldb is unavailable on JS platforms")
}

/*
// NewCustom returns a wrapped LevelDB object. The namespace is the prefix that the
// metrics reporting should use for surfacing internal stats.
// The customize function allows the caller to modify the leveldb options.
func NewCustom(file string, namespace string, customize func(options *opt.Options)) (*Database, error) {
	return nil, errors.New("leveldb is unavailable on JS platforms")
}

// configureOptions sets some default options, then runs the provided setter.
func configureOptions(customizeFn func(*opt.Options)) *opt.Options {
	// Set default options
	options := &opt.Options{
		Filter:                 filter.NewBloomFilter(10),
		DisableSeeksCompaction: true,
	}
	// Allow caller to make custom modifications to the options
	if customizeFn != nil {
		customizeFn(options)
	}
	return options
}
*/

// Close stops the metrics collection, flushes any pending data to disk and closes
// all io accesses to the underlying key-value store.
func (db *Database) Close() error {
	panic("Method called on unconstructable leveldb database")
}

// Has retrieves if a key is present in the key-value store.
func (db *Database) Has(key []byte) (bool, error) {
	panic("Method called on unconstructable leveldb database")
}

// Get retrieves the given key if it's present in the key-value store.
func (db *Database) Get(key []byte) ([]byte, error) {
	panic("Method called on unconstructable leveldb database")
}

// Put inserts the given value into the key-value store.
func (db *Database) Put(key []byte, value []byte) error {
	panic("Method called on unconstructable leveldb database")
}

// Delete removes the key from the key-value store.
func (db *Database) Delete(key []byte) error {
	panic("Method called on unconstructable leveldb database")
}

// NewBatch creates a write-only key-value store that buffers changes to its host
// database until a final write is called.
func (db *Database) NewBatch() ethdb.Batch {
	panic("Method called on unconstructable leveldb database")
}

func (db *Database) NewBatchWithSize(size int) ethdb.Batch {
	panic("Method called on unconstructable leveldb database")
}

// NewIterator creates a binary-alphabetical iterator over a subset
// of database content with a particular key prefix, starting at a particular
// initial key (or after, if it does not exist).
func (db *Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	panic("Method called on unconstructable leveldb database")
}

func (db *Database) NewSnapshot() (ethdb.Snapshot, error) {
	panic("Method called on unconstructable leveldb database")
}

// Stat returns a particular internal stat of the database.
func (db *Database) Stat(property string) (string, error) {
	panic("Method called on unconstructable leveldb database")
}

// Compact flattens the underlying data store for the given key range. In essence,
// deleted and overwritten versions are discarded, and the data is rearranged to
// reduce the cost of operations needed to access them.
//
// A nil start is treated as a key before all keys in the data store; a nil limit
// is treated as a key after all keys in the data store. If both is nil then it
// will compact entire data store.
func (db *Database) Compact(start []byte, limit []byte) error {
	panic("Method called on unconstructable leveldb database")
}

// Path returns the path to the database directory.
func (db *Database) Path() string {
	panic("Method called on unconstructable leveldb database")
}

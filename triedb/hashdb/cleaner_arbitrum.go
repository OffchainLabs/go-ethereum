package hashdb

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

type delayedCleaner struct {
	db      *Database
	cleaner *cleaner

	hashes []common.Hash // to-be-deleted hashes
}

func newDelayedCleaner(db *Database) *delayedCleaner {
	return &delayedCleaner{
		db:      db,
		cleaner: &cleaner{db: db},
	}
}

// Put can be called holding read lock
// Write lock is not required here as Put doesn't modify dirties
// The value put is ignored as it can be read later on from dirties
func (c *delayedCleaner) Put(key []byte, _ []byte) error {
	hash := common.BytesToHash(key)
	// If the node does not exist, we're done on this path
	_, ok := c.db.dirties[hash]
	if !ok {
		return nil
	}
	// add key to to-be-deleted keys
	c.hashes = append(c.hashes, hash)
	return nil
}

func (c *delayedCleaner) Delete(key []byte) error {
	panic("not implemented")
}

// Clean removes the buffered to-be-deleted keys from dirties
// Write lock must be held by the caller
func (c *delayedCleaner) Clean() {
	for _, hash := range c.hashes {
		node, ok := c.db.dirties[hash]
		if !ok {
			// node no longer in dirties
			continue
		}
		rawdb.WriteLegacyTrieNode(c.cleaner, hash, node.node)
	}
}

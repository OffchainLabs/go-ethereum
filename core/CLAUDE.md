# go-ethereum/core

## hashdb Dirty Cache Lifecycle

The hashdb (`triedb/hashdb/database.go`) manages trie nodes in three layers:
1. **Clean cache** (fastcache): Per-instance. Populated on disk reads and during Commit.
2. **Dirty cache** (`dirties` map): Per-instance. In-memory nodes not yet written to disk.
3. **Disk** (pebble/leveldb via rawdb): Persistent storage, shared across instances using the same DB.

### Node resolution (`node()`, line 196)
Checks in order: clean cache → dirty cache → disk. When found on disk, adds to clean cache.

### Node insertion (`insert()`, line 166)
Called by `hashdb.Update()` during `statedb.CommitWithUpdate()`. Skips if node already present. New nodes start with `parents = 0`. `forChildren` iterates child hashes; for each child already in `dirties`, `child.parents++`.

### Reference counting (`reference()`, line 278)
- `Reference(root, common.Hash{})`: Called from `writeBlockWithState`. Increments `root.parents` by 1 (metadata reference for GC). State roots end up with `parents == 1`.
- `reference(account.Root, parent)`: Links storage trie roots to account trie nodes.
- `insert().forChildren`: Children already in dirties get `parents++` when a new parent is inserted.

### Dereference (lines 302-373)
Decrements `node.parents`. If `parents == 0`: removes from dirties, recursively dereferences children. `gcnodes` counter tracks removals but **resets to 0 after each Commit** (line 510).

### Commit (lines 459-538)
Writes all reachable nodes from a root to disk + clean cache, then removes from dirty cache. Shared nodes between committed and non-committed roots are removed from dirty cache (they're now on disk).

### Cap (lines 380-458)
Walks the flush-list from oldest, writing ALL nodes to disk regardless of reference count. Triggered when `nodes > TrieDirtyLimit || imgs > 4MB`. Makes state ACCESSIBLE, not inaccessible.

## Block Processing Flow (writeBlockWithState)

Location: `blockchain.go` lines 1716-1839

1. Write block/receipts to disk (lines 1724-1730)
2. `statedb.CommitWithUpdate` → `hashdb.Update()` inserts nodes into dirty cache (line 1732)
3. Sparse archive check: if commit counter expired, commit root and return early (lines 1749-1773)
4. Otherwise: `Reference(root)` + push to triegc queue (lines 1776-1777)
5. GC loop: process old entries from triegc (lines 1782-1832)
6. In `writeBlockAndSetHead`: set canonical head, write tx lookup entries (line 1858)

**Receipt availability**: Tx lookup entries are written AFTER the GC loop, so `EnsureTxSucceeded` guarantees GC completed for that block.

## GC Loop Detail

For each non-committed block, the root is pushed onto a max-priority queue (ascending block order via negated priorities). The loop pops entries where both conditions hold:
- `blockNumber <= blockLimit` (blockLimit = currentBlock - TriesInMemory)
- `timestamp <= timeLimit` (timeLimit = time.Now() - TrieRetention)

Each popped entry is dereferenced, removing its nodes from the dirty cache.

**Timestamp sensitivity**: The break condition `timestamp > uint64(timeLimit)` depends on wall-clock time. Under heavy CPU load (e.g., 625+ parallel tests), this timing relationship can be disrupted, leaving entries in the queue and nodes in the dirty cache.

**Dirty cache can survive GC**: Even when the GC loop runs, roots may remain in the dirty cache. This is transient state that would be lost on restart and does not indicate committed-to-disk data. Tests that need to verify "state is not persisted" must check `NodeSource` (see below), not `StateAt`, because `StateAt` succeeds for both dirty and disk nodes.

## NodeSource (triedb/database.go, triedb/hashdb/database.go)

`NodeSource(hash)` returns where a trie node is found: `"clean"`, `"dirty"`, `"disk"`, or `"missing"`.

- **"dirty"**: In-memory only, transient, lost on restart. Not committed to persistent storage.
- **"clean"**: In the fastcache read cache, backed by disk. Indicates the node is persisted.
- **"disk"**: Found on disk (pebble/leveldb). Persisted.
- **"missing"**: Not found anywhere.

Use `NodeSource` in tests to distinguish between transient dirty-cache presence and actual disk persistence. `StateAt` cannot make this distinction — it succeeds for both dirty and disk nodes.

## Sparse Archive Commit Pattern

With `MaxNumberOfBlocksToSkipStateSaving = N`, a commit counter counts down from N. When it reaches 0, the root is committed to disk and the counter resets. Committed blocks skip the GC loop entirely. Between commits, blocks go through the Reference → triegc → Dereference path.

## Blocks Reexecutor Interaction

The blocks reexecutor (`blocks_reexecutor/blocks_reexecutor.go`) creates its **own** hashdb instance sharing the same underlying pebble database but with **separate** dirty/clean caches. `CommitStateToDisk = true` writes through to the shared pebble DB; `false` only updates the reexecutor's private dirty cache.

## FlushTrieDB (blockchain_arbitrum.go)

Called by `MaintenanceRunner` (disabled by default in tests). Commits the oldest triegc entry to disk, then calls `Cap(capLimit)`. Not relevant in most test scenarios.

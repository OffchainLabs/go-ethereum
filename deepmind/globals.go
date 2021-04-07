package deepmind

// Enabled determines if deep mind instrumentation is enabled. Controlling
// deepmind behavior is then controlled via other flag like.
var Enabled = false

// SyncInstrumentationEnabled determines if standard block syncing prints to standard
// console or if it discards it entirely and does not print nothing.
//
// This feature is enabled by default, can be disabled on a miner node used for
// speculative execution.
var SyncInstrumentationEnabled = true

// MiningEnabled determines if mining code should stay enabled even when deep mind
// is active. In normal production setup, we always activate deep mind on syncer node
// only. However, on local development setup, one might need to test speculative execution
// code locally. To achieve this, it's possible to enable deep mind on miner, disable
// sync instrumentation and enable mining. This way, new blocks are mined, sync logs are
// not printed and speculative execution log can be accumulated.
var MiningEnabled = false

// BlockProgressEnabled enable output of finalize block line only.
//
// Currently, when taking backups, the best way to know about current
// last block seen is to track deep mind logging. However, while doing
// the actual backups, it's not necessary to print all deep mind logs.
//
// This settings will only affect printing the finalized block log line
// and will not impact any other deep mind logs. If you need deep mind
// instrumentation, activate deep mind. The deep mind setting has
// precedence over this setting.
var BlockProgressEnabled = false

// CompactionDisabled disables all leveldb table compaction that could
// happen.
//
// It does so mainly be increasing to their maximum all the settings that
// causes compaction to happen as well as disabling some code path inside
// Geth where manual compaction can be triggered.
var CompactionDisabled = false

// ArchiveBlocksToKeep defines how many blocks our node should keep up prior
// pruning the state. This is actually used to override `core/blockchain.go` `TriesInMemory`
// variable.
//
// A value of 0 means use the Geth default value.
var ArchiveBlocksToKeep = uint64(0)

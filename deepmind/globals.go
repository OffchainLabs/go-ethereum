package deepmind

// Enabled determines if deep mind instrumentation are output
// to the stdout of the running process
var Enabled = false

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

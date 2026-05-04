package state

// Geth treats the config value as opaque; Nitro asserts it back to its typed config struct
// at the read site.
func (db *CodeDB) ArbNodeConfig() any       { return db.arbNodeConfig }
func (db *CodeDB) SetArbNodeConfig(cfg any) { db.arbNodeConfig = cfg }

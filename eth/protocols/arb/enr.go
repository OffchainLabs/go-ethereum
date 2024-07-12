package arb

import "github.com/ethereum/go-ethereum/rlp"

// enrEntry is the ENR entry which advertises `snap` protocol on the discovery.
type enrEntry struct {
	// Ignore additional fields (for forward compatibility).
	Rest []rlp.RawValue `rlp:"tail"`
}

// ENRKey implements enr.Entry.
func (e enrEntry) ENRKey() string {
	return "arb"
}

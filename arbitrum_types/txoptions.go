package arbitrum_types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
)

type RootHashOrSlots struct {
	RootHash  *common.Hash
	SlotValue map[common.Hash]common.Hash
}

func (r *RootHashOrSlots) UnmarshalJSON(data []byte) error {
	var hash common.Hash
	var err error
	if err = json.Unmarshal(data, &hash); err == nil {
		r.RootHash = &hash
		return nil
	}
	return json.Unmarshal(data, &r.SlotValue)
}

func (r RootHashOrSlots) MarshalJSON() ([]byte, error) {
	if r.RootHash != nil {
		return json.Marshal(*r.RootHash)
	}
	return json.Marshal(r.SlotValue)
}

// TODO rename to just "Options" ?
type ConditionalOptions struct {
	KnownAccounts  map[common.Address]RootHashOrSlots `json:"knownAccounts"`
	BlockNumberMin *hexutil.Uint64                    `json:"blockNumberMin,omitempty"`
	BlockNumberMax *hexutil.Uint64                    `json:"blockNumberMax,omitempty"`
	TimestampMin   *hexutil.Uint64                    `json:"timestampMin,omitempty"`
	TimestampMax   *hexutil.Uint64                    `json:"timestampMax,omitempty"`
}

func (o *ConditionalOptions) Check(blockNumber uint64, statedb *state.StateDB) error {
	if o.TimestampMin != nil || o.TimestampMax != nil {
		now := uint64(time.Now().Unix())
		if o.TimestampMin != nil && now < uint64(*o.TimestampMin) {
			return fmt.Errorf("TimestampMin condition not met, want: %d, have: %d", *o.TimestampMin, now)
		}
		if o.TimestampMax != nil && now > uint64(*o.TimestampMax) {
			return fmt.Errorf("TimestampMax condition not met, want: %d, have: %d", *o.TimestampMax, now)
		}
	}
	if o.BlockNumberMin != nil && blockNumber < uint64(*o.BlockNumberMin) {
		return fmt.Errorf("BlockNumberMin condition not met, want: %d, have: %d", *o.BlockNumberMin, blockNumber)
	}
	if o.BlockNumberMax != nil && blockNumber > uint64(*o.BlockNumberMax) {
		return fmt.Errorf("BlockNumberMax condition not met, want: %d, have: %d", *o.BlockNumberMax, blockNumber)
	}
	for address, rootHashOrSlots := range o.KnownAccounts {
		if rootHashOrSlots.RootHash != nil {
			trie := statedb.StorageTrie(address)
			if trie == nil {
				return fmt.Errorf("Storage trie not found for address key in knownAccounts option, address: %s", address.String())
			}
			if trie.Hash() != *rootHashOrSlots.RootHash {
				return fmt.Errorf("Storage root hash condition not met for address: %s", address.String())
			}
		} else if len(rootHashOrSlots.SlotValue) > 0 {
			for slot, value := range rootHashOrSlots.SlotValue {
				stored := statedb.GetState(address, slot)
				if !bytes.Equal(stored.Bytes(), value.Bytes()) {
					return fmt.Errorf("Storage slot value condition not met for address: %s, slot: %s", address.String(), slot.String())
				}
			}
		} // else rootHashOrSlots.SlotValue is empty - ignore it and check the rest of conditions
	}
	return nil
}

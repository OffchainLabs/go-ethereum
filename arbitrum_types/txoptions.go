package arbitrum_types

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
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
	KnownAccounts map[common.Address]RootHashOrSlots `json:"knownAccounts"`
}

func (o *ConditionalOptions) Check(statedb *state.StateDB) error {
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

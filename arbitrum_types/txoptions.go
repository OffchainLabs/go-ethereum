package arbitrum_types

import (
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
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
	if len(r.SlotValue) > 0 {
		slotValue := r.SlotValue
		return json.Marshal(slotValue)
	}
	return nil, nil
}

// TODO rename to just "Options" ?
type ConditionalOptions struct {
	KnownAccounts map[common.Address]RootHashOrSlots `json:"knownAccounts"`
}

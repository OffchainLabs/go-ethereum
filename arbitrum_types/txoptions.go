package arbitrum_types

import (
	"bytes"
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
)

type rejectedError struct {
	msg string
}

func NewRejectedError(msg string) *rejectedError {
	return &rejectedError{msg: msg}
}
func (e rejectedError) Error() string { return e.msg }
func (rejectedError) ErrorCode() int  { return -32003 }

type limitExceededError struct {
	msg string
}

func NewLimitExceededError(msg string) *limitExceededError {
	return &limitExceededError{msg: msg}
}
func (e limitExceededError) Error() string { return e.msg }
func (limitExceededError) ErrorCode() int  { return -32005 }

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

func (o *ConditionalOptions) Check(l1BlockNumber uint64, l1Timestamp uint64, statedb *state.StateDB) error {
	if o.BlockNumberMin != nil && l1BlockNumber < uint64(*o.BlockNumberMin) {
		return NewRejectedError("BlockNumberMin condition not met")
	}
	if o.BlockNumberMax != nil && l1BlockNumber > uint64(*o.BlockNumberMax) {
		return NewRejectedError("BlockNumberMax condition not met")
	}
	if o.TimestampMin != nil && l1Timestamp < uint64(*o.TimestampMin) {
		return NewRejectedError("TimestampMin condition not met")
	}
	if o.TimestampMax != nil && l1Timestamp > uint64(*o.TimestampMax) {
		return NewRejectedError("TimestampMax condition not met")
	}
	return o.CheckOnlyStorage(statedb)
}

func (o *ConditionalOptions) PreCheck(l1BlockNumberLowerBound uint64, statedb *state.StateDB) error {
	if o.BlockNumberMax != nil && l1BlockNumberLowerBound > uint64(*o.BlockNumberMax) {
		return NewRejectedError("BlockNumberMax condition not met")
	}
	// TODO can we get a timestamp LowerBound?
	//if o.TimestampMin != nil && l1TimestampLowerBound > uint64(*o.TimestampMax) {
	//	return NewRejectedError("TimestampMax condition not met")
	//}
	return o.CheckOnlyStorage(statedb)
}

func (o *ConditionalOptions) CheckOnlyStorage(statedb *state.StateDB) error {
	for address, rootHashOrSlots := range o.KnownAccounts {
		if rootHashOrSlots.RootHash != nil {
			trie := statedb.StorageTrie(address)
			if trie == nil {
				return NewRejectedError("Storage trie not found for address key in knownAccounts option")
			}
			if trie.Hash() != *rootHashOrSlots.RootHash {
				return NewRejectedError("Storage root hash condition not met")
			}
		} else if len(rootHashOrSlots.SlotValue) > 0 {
			for slot, value := range rootHashOrSlots.SlotValue {
				stored := statedb.GetState(address, slot)
				if !bytes.Equal(stored.Bytes(), value.Bytes()) {
					return NewRejectedError("Storage slot value condition not met")
				}
			}
		} // else rootHashOrSlots.SlotValue is empty - ignore it and check the rest of conditions
	}
	return nil
}

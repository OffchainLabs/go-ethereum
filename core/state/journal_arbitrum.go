package state

import "github.com/ethereum/go-ethereum/common"

type wasmCodeChange struct {
	account    *common.Address
	moduleHash common.Hash
}

func (ch wasmCodeChange) revert(s *StateDB) {
	s.getStateObject(*ch.account).setActivation(ch.moduleHash, nil, nil)
}

func (ch wasmCodeChange) dirtied() *common.Address {
	return ch.account
}

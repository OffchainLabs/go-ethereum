package state

import "github.com/ethereum/go-ethereum/common"

type wasmCodeChange struct {
	account *common.Address
	version uint16
}

func (ch wasmCodeChange) revert(s *StateDB) {
	s.getStateObject(*ch.account).cacheActivation(ch.version, nil, nil)
}

func (ch wasmCodeChange) dirtied() *common.Address {
	return ch.account
}

package state

import "github.com/ethereum/go-ethereum/common"

type wasmCodeChange struct {
	account *common.Address
	version uint32
}

func (ch wasmCodeChange) revert(s *StateDB) {
	s.getStateObject(*ch.account).setWASMCode(nil, ch.version)
}

func (ch wasmCodeChange) dirtied() *common.Address {
	return ch.account
}

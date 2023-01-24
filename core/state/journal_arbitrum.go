package state

import "github.com/ethereum/go-ethereum/common"

type wasmCodeChange struct {
	account            *common.Address
	prevcode, prevhash []byte
}

func (ch wasmCodeChange) revert(s *StateDB) {
	s.getStateObject(*ch.account).setWASMCode(common.BytesToHash(ch.prevhash), ch.prevcode)
}

func (ch wasmCodeChange) dirtied() *common.Address {
	return ch.account
}

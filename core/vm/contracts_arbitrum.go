package vm

import "github.com/ethereum/go-ethereum/common"

var (
	ExtraPrecompiles = make(map[common.Address]PrecompiledContract)
	PrecompiledAddressesArbitrum []common.Address
)

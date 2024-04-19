package vm

import "github.com/ethereum/go-ethereum/common"

var (
	PrecompiledContractsArbitrum = make(map[common.Address]PrecompiledContract)
	PrecompiledAddressesArbitrum []common.Address
	PrecompiledContractsArbOS30  = make(map[common.Address]PrecompiledContract)
	PrecompiledAddressesArbOS30  []common.Address
)

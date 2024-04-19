package vm

import "github.com/ethereum/go-ethereum/common"

var (
	PrecompiledContractsArbitrumNitro  = make(map[common.Address]PrecompiledContract)
	PrecompiledContractsArbitrumStylus = make(map[common.Address]PrecompiledContract)
	PrecompiledAddressesArbitrumNitro  []common.Address
	PrecompiledAddressesArbitrumStylus []common.Address
)

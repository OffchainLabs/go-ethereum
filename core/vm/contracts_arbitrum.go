package vm

import "github.com/ethereum/go-ethereum-arbitrum/common"

var (
	PrecompiledContractsArbitrum = make(map[common.Address]PrecompiledContract)
	PrecompiledAddressesArbitrum []common.Address
)

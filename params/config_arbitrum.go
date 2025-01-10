// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package params

import (
	"math/big"

	"github.com/paxosglobal/go-ethereum-arbitrum/common"
)

const ArbosVersion_2 = uint64(2)
const ArbosVersion_3 = uint64(3)
const ArbosVersion_4 = uint64(4)
const ArbosVersion_5 = uint64(5)
const ArbosVersion_6 = uint64(6)
const ArbosVersion_7 = uint64(7)
const ArbosVersion_8 = uint64(8)
const ArbosVersion_9 = uint64(9)
const ArbosVersion_10 = uint64(10)
const ArbosVersion_11 = uint64(11)
const ArbosVersion_20 = uint64(20)
const ArbosVersion_30 = uint64(30)
const ArbosVersion_31 = uint64(31)
const ArbosVersion_32 = uint64(32)

const ArbosVersion_FixRedeemGas = ArbosVersion_11
const ArbosVersion_Stylus = ArbosVersion_30
const ArbosVersion_StylusFixes = ArbosVersion_31
const ArbosVersion_StylusChargingFixes = ArbosVersion_32
const MaxArbosVersionSupported = ArbosVersion_32
const MaxDebugArbosVersionSupported = ArbosVersion_32

type ArbitrumChainParams struct {
	EnableArbOS               bool
	AllowDebugPrecompiles     bool
	DataAvailabilityCommittee bool
	InitialArbOSVersion       uint64
	InitialChainOwner         common.Address
	GenesisBlockNum           uint64
	MaxCodeSize               uint64 `json:"MaxCodeSize,omitempty"`     // Maximum bytecode to permit for a contract. 0 value implies params.DefaultMaxCodeSize
	MaxInitCodeSize           uint64 `json:"MaxInitCodeSize,omitempty"` // Maximum initcode to permit in a creation transaction and create instructions. 0 value implies params.DefaultMaxInitCodeSize
}

func (c *ChainConfig) IsArbitrum() bool {
	return c.ArbitrumChainParams.EnableArbOS
}

func (c *ChainConfig) IsArbitrumNitro(num *big.Int) bool {
	return c.IsArbitrum() && isBlockForked(new(big.Int).SetUint64(c.ArbitrumChainParams.GenesisBlockNum), num)
}

func (c *ChainConfig) MaxCodeSize() uint64 {
	if c.ArbitrumChainParams.MaxCodeSize == 0 {
		return DefaultMaxCodeSize
	}
	return c.ArbitrumChainParams.MaxCodeSize
}

func (c *ChainConfig) MaxInitCodeSize() uint64 {
	if c.ArbitrumChainParams.MaxInitCodeSize == 0 {
		return c.MaxCodeSize() * 2
	}
	return c.ArbitrumChainParams.MaxInitCodeSize
}

func (c *ChainConfig) DebugMode() bool {
	return c.ArbitrumChainParams.AllowDebugPrecompiles
}

func (c *ChainConfig) checkArbitrumCompatible(newcfg *ChainConfig, head *big.Int) *ConfigCompatError {
	if c.IsArbitrum() != newcfg.IsArbitrum() {
		// This difference applies to the entire chain, so report that the genesis block is where the difference appears.
		return newBlockCompatError("isArbitrum", common.Big0, common.Big0)
	}
	if !c.IsArbitrum() {
		return nil
	}
	cArb := &c.ArbitrumChainParams
	newArb := &newcfg.ArbitrumChainParams
	if cArb.GenesisBlockNum != newArb.GenesisBlockNum {
		return newBlockCompatError("genesisblocknum", new(big.Int).SetUint64(cArb.GenesisBlockNum), new(big.Int).SetUint64(newArb.GenesisBlockNum))
	}
	return nil
}

func DisableArbitrumParams() ArbitrumChainParams {
	return ArbitrumChainParams{
		EnableArbOS:               false,
		AllowDebugPrecompiles:     false,
		DataAvailabilityCommittee: false,
		InitialArbOSVersion:       0,
		InitialChainOwner:         common.Address{},
	}
}

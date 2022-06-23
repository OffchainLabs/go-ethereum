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

	"github.com/ethereum/go-ethereum/common"
)

type ArbitrumChainParams struct {
	EnableArbOS               bool
	AllowDebugPrecompiles     bool
	DataAvailabilityCommittee bool
	InitialArbOSVersion       uint64
	InitialChainOwner         common.Address
	GenesisBlockNum           uint64
}

func (c *ChainConfig) IsArbitrum() bool {
	return c.ArbitrumChainParams.EnableArbOS
}

func (c *ChainConfig) DebugMode() bool {
	return c.ArbitrumChainParams.AllowDebugPrecompiles
}

func (c *ChainConfig) checkArbitrumCompatible(newcfg *ChainConfig, head *big.Int) *ConfigCompatError {
	boolToBig := func(b bool) *big.Int {
		if b {
			return common.Big1
		}
		return common.Big0
	}

	if c.IsArbitrum() != newcfg.IsArbitrum() {
		return newCompatError("isArbitrum", boolToBig(c.IsArbitrum()), boolToBig(newcfg.IsArbitrum()))
	}
	if !c.IsArbitrum() {
		return nil
	}
	cArb := &c.ArbitrumChainParams
	newArb := &newcfg.ArbitrumChainParams
	if cArb.GenesisBlockNum != newArb.GenesisBlockNum {
		return newCompatError("genesisblocknum", new(big.Int).SetUint64(cArb.GenesisBlockNum), new(big.Int).SetUint64(newArb.GenesisBlockNum))
	}
	return nil
}

func ArbitrumOneParams() ArbitrumChainParams {
	return ArbitrumChainParams{
		EnableArbOS:               true,
		AllowDebugPrecompiles:     false,
		DataAvailabilityCommittee: false,
		// Not used as arbitrum one has init data
		InitialArbOSVersion: 1,
		InitialChainOwner:   common.Address{},
	}
}

func ArbitrumAnytrustTBDParams() ArbitrumChainParams {
	return ArbitrumChainParams{
		EnableArbOS:               true,
		AllowDebugPrecompiles:     false,
		DataAvailabilityCommittee: true,
		InitialArbOSVersion:       1,
		InitialChainOwner:         common.Address{},
	}
}

func ArbitrumDevnetParams() ArbitrumChainParams {
	return ArbitrumChainParams{
		EnableArbOS:               true,
		AllowDebugPrecompiles:     false,
		DataAvailabilityCommittee: false,
		InitialArbOSVersion:       1,
		InitialChainOwner:         common.HexToAddress("0x186B56023d42B2B4E7616589a5C62EEf5FCa21DD"),
	}
}

func ArbitrumDevTestParams() ArbitrumChainParams {
	return ArbitrumChainParams{
		EnableArbOS:               true,
		AllowDebugPrecompiles:     true,
		DataAvailabilityCommittee: false,
		InitialArbOSVersion:       1,
		InitialChainOwner:         common.Address{},
	}
}

func ArbitrumDevTestDASParams() ArbitrumChainParams {
	return ArbitrumChainParams{
		EnableArbOS:               true,
		AllowDebugPrecompiles:     true,
		DataAvailabilityCommittee: true,
		InitialArbOSVersion:       1,
		InitialChainOwner:         common.Address{},
	}
}

func ArbitrumDevnetDASParams() ArbitrumChainParams {
	return ArbitrumChainParams{
		EnableArbOS:               true,
		AllowDebugPrecompiles:     false,
		DataAvailabilityCommittee: true,
		InitialArbOSVersion:       1,
		InitialChainOwner:         common.HexToAddress("0x186B56023d42B2B4E7616589a5C62EEf5FCa21DD"),
	}
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

func ArbitrumOneChainConfig() *ChainConfig {
	return &ChainConfig{
		ChainID:             big.NewInt(42161),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.Hash{},
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		ArbitrumChainParams: ArbitrumOneParams(),
		Clique: &CliqueConfig{
			Period: 0,
			Epoch:  0,
		},
	}
}

func ArbitrumAnytrustTBDChainConfig() *ChainConfig {
	return &ChainConfig{
		ChainID:             big.NewInt(42170),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.Hash{},
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		ArbitrumChainParams: ArbitrumAnytrustTBDParams(),
		Clique: &CliqueConfig{
			Period: 0,
			Epoch:  0,
		},
	}
}

func ArbitrumDevnetChainConfig() *ChainConfig {
	return &ChainConfig{
		ChainID:             big.NewInt(421613),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.Hash{},
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		ArbitrumChainParams: ArbitrumDevnetParams(),
		Clique: &CliqueConfig{
			Period: 0,
			Epoch:  0,
		},
	}
}

func ArbitrumDevTestChainConfig() *ChainConfig {
	return &ChainConfig{
		ChainID:             big.NewInt(412346),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.Hash{},
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		ArbitrumChainParams: ArbitrumDevTestParams(),
		Clique: &CliqueConfig{
			Period: 0,
			Epoch:  0,
		},
	}
}

func ArbitrumDevTestDASChainConfig() *ChainConfig {
	return &ChainConfig{
		ChainID:             big.NewInt(412347),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.Hash{},
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		ArbitrumChainParams: ArbitrumDevTestDASParams(),
		Clique: &CliqueConfig{
			Period: 0,
			Epoch:  0,
		},
	}
}

func ArbitrumDevnetDASChainConfig() *ChainConfig {
	return &ChainConfig{
		ChainID:             big.NewInt(421703),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil,
		DAOForkSupport:      true,
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.Hash{},
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		ArbitrumChainParams: ArbitrumDevnetDASParams(),
		Clique: &CliqueConfig{
			Period: 0,
			Epoch:  0,
		},
	}
}

var ArbitrumSupportedChainConfigs = []*ChainConfig{
	ArbitrumOneChainConfig(),
	ArbitrumAnytrustTBDChainConfig(),
	ArbitrumDevnetChainConfig(),
	ArbitrumDevTestChainConfig(),
	ArbitrumDevTestDASChainConfig(),
	ArbitrumDevnetDASChainConfig(),
}

package arbitrum

import (
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	flag "github.com/spf13/pflag"
)
import "time"

type Config struct {
	// RPCGasCap is the global gas cap for eth-call variants.
	// Store as uint64, but default and value read from config as signed so that it can be cast to int.
	RPCGasCap uint64

	// RPCTxFeeCap is the global transaction fee(price * gaslimit) cap for
	// send-transction variants. The unit is ether.
	RPCTxFeeCap float64

	// RPCEVMTimeout is the global timeout for eth-call.
	RPCEVMTimeout time.Duration
}

func ConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Int64(prefix+".rpc-gas-cap", int64(DefaultConfig.RPCGasCap), "gas cap for eth-call variants")
	f.Float64(prefix+".rpc-tx-fee-cap", DefaultConfig.RPCTxFeeCap, "transaction fee cap for send-transaction variants in ETH")
	f.Duration(prefix+".rpc-evm-timeout", DefaultConfig.RPCEVMTimeout, "timeout for eth-call")
}

var DefaultConfig = Config{
	RPCGasCap:     ethconfig.Defaults.RPCGasCap,     // 50,000,000
	RPCTxFeeCap:   ethconfig.Defaults.RPCTxFeeCap,   // 1 ether
	RPCEVMTimeout: ethconfig.Defaults.RPCEVMTimeout, // 5 seconds
}

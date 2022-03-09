package arbitrum

import (
	"math"

	flag "github.com/spf13/pflag"
)
import "time"

type Config struct {
	// RPCGasCap is the global gas cap for eth-call variants.
	RPCGasCap uint64

	// RPCTxFeeCap is the global transaction fee(price * gaslimit) cap for
	// send-transction variants. The unit is ether.
	RPCTxFeeCap float64

	// RPCEVMTimeout is the global timeout for eth-call.
	RPCEVMTimeout time.Duration
}

func ConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Uint64(prefix+".rpc-gas-cap", DefaultConfig.RPCGasCap, "gas cap for eth-call variants")
	f.Float64(prefix+".rpc-tx-fee-cap", DefaultConfig.RPCTxFeeCap, "transaction fee cap for send-transaction variants in ETH")
	f.Duration(prefix+".rpc-evm-timeout", DefaultConfig.RPCEVMTimeout, "timeout for eth-call")
}

var DefaultConfig = Config{
	RPCGasCap:     math.MaxUint64, // no real limit, can cast to int
	RPCTxFeeCap:   1,              // 1 ether
	RPCEVMTimeout: 0,              // disable timeout
}

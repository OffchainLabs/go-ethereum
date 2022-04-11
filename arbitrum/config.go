package arbitrum

import (
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	flag "github.com/spf13/pflag"
)
import "time"

type Config struct {
	// RPCGasCap is the global gas cap for eth-call variants.
	RPCGasCap uint64 `koanf:"gas-cap"`

	// RPCTxFeeCap is the global transaction fee(price * gaslimit) cap for
	// send-transction variants. The unit is ether.
	RPCTxFeeCap float64 `koanf:"tx-fee-cap"`

	// RPCEVMTimeout is the global timeout for eth-call.
	RPCEVMTimeout time.Duration `koanf:"evm-timeout"`
}

func ConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Uint64(prefix+".gas-cap", DefaultConfig.RPCGasCap, "cap on computation gas that can be used in eth_call/estimateGas (0=infinite)")
	f.Float64(prefix+".tx-fee-cap", DefaultConfig.RPCTxFeeCap, "cap on transaction fee (in ether) that can be sent via the RPC APIs (0 = no cap)")
	f.Duration(prefix+".evm-timeout", DefaultConfig.RPCEVMTimeout, "timeout used for eth_call (0=infinite)")
}

var DefaultConfig = Config{
	RPCGasCap:     ethconfig.Defaults.RPCGasCap,     // 50,000,000
	RPCTxFeeCap:   ethconfig.Defaults.RPCTxFeeCap,   // 1 ether
	RPCEVMTimeout: ethconfig.Defaults.RPCEVMTimeout, // 5 seconds
}

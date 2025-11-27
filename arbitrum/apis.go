package arbitrum

import (
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/rpc"
)

type APIs struct {
	EthAPIs           ethapi.APIs
	FilterAPI         rpc.API
	ArbTransactionAPI rpc.API
	PublicNetAPI      rpc.API
	PublicTxPoolAPI   rpc.API
	TracersAPIs       []rpc.API
}

func (a APIs) Slice() []rpc.API {
	var apis []rpc.API

	apis = append(apis, a.EthAPIs.Slice()...)
	apis = append(apis, a.FilterAPI)
	apis = append(apis, a.ArbTransactionAPI)
	apis = append(apis, a.PublicNetAPI)
	apis = append(apis, a.PublicTxPoolAPI)
	apis = append(apis, a.TracersAPIs...)

	return apis
}

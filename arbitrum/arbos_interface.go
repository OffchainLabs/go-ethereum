package arbitrum

import (
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
)

type TransactionPublisher interface {
	PublishTransaction(tx *types.Transaction) error
	BlockChain() *core.BlockChain
	Start() error
	Stop() error
}

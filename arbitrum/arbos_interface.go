package arbitrum

import (
	"github.com/ethereum/go-ethereum/core/types"
)

type TransactionPublisher interface {
	PublishTransaction(tx *types.Transaction) error
}

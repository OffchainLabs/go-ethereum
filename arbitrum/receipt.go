package arbitrum

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

type ReceiptFetcher interface {
	GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error)
}

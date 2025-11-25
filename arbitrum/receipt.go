package arbitrum

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/params"
)

type ReceiptFetcher interface {
	GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error)
}

// Exposing the internal function, this should make maintaining the upstream code easier
func MarshalReceipt(receipt *types.Receipt, blockHash common.Hash, blockNumber uint64, signer types.Signer, tx *types.Transaction, txIndex uint64, chainConfig *params.ChainConfig, header *types.Header, blockMetadata common.BlockMetadata) map[string]interface{} {
	return ethapi.MarshalReceipt(receipt, blockHash, blockNumber, signer, tx, txIndex, chainConfig, header, blockMetadata)
}

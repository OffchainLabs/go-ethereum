package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Data as received from Arb1 chain
type ArbitrumLegacyTransactionResult struct {
	BlockHash        *common.Hash    `json:"blockHash"`
	BlockNumber      *hexutil.Big    `json:"blockNumber"`
	From             common.Address  `json:"from"`
	Gas              hexutil.Uint64  `json:"gas"`
	GasPrice         *hexutil.Big    `json:"gasPrice"`
	Hash             common.Hash     `json:"hash"`
	Input            hexutil.Bytes   `json:"input"`
	Nonce            hexutil.Uint64  `json:"nonce"`
	To               *common.Address `json:"to"`
	TransactionIndex *hexutil.Uint64 `json:"transactionIndex"`
	Value            *hexutil.Big    `json:"value"`
	V                *hexutil.Big    `json:"v"`
	R                *hexutil.Big    `json:"r"`
	S                *hexutil.Big    `json:"s"`

	// Arbitrum Specific Fields
	L1SeqNum        *hexutil.Big    `json:"l1SequenceNumber"`
	ParentRequestId *common.Hash    `json:"parentRequestId"`
	IndexInParent   *hexutil.Big    `json:"indexInParent"`
	ArbType         hexutil.Uint64  `json:"arbType"`
	ArbSubType      *hexutil.Uint64 `json:"arbSubType"`
	L1BlockNumber   *hexutil.Big    `json:"l1BlockNumber"`
}

type ArbitrumLegacyTxData struct {
	Gas      uint64
	GasPrice *big.Int
	Hash     common.Hash // Hash cannot be locally computed from other fields
	Data     []byte
	Nonce    uint64
	To       *common.Address `rlp:"nil"` // nil means contract creation
	Value    *big.Int
	V, R, S  *big.Int
}

func (tx *ArbitrumLegacyTxData) copy() TxData {
	cpy := &ArbitrumLegacyTxData{
		Nonce: tx.Nonce,
		To:    copyAddressPtr(tx.To),
		Data:  common.CopyBytes(tx.Data),
		Gas:   tx.Gas,
		Hash:  tx.Hash,
		// These are initialized below.
		Value:    new(big.Int),
		GasPrice: new(big.Int),
		V:        new(big.Int),
		R:        new(big.Int),
		S:        new(big.Int),
	}
	if tx.Value != nil {
		cpy.Value.Set(tx.Value)
	}
	if tx.GasPrice != nil {
		cpy.GasPrice.Set(tx.GasPrice)
	}
	if tx.V != nil {
		cpy.V.Set(tx.V)
	}
	if tx.R != nil {
		cpy.R.Set(tx.R)
	}
	if tx.S != nil {
		cpy.S.Set(tx.S)
	}
	return cpy
}

func ArbitrumLegacyFromTransactionResult(result ArbitrumLegacyTransactionResult) *Transaction {
	gas := uint64(result.Gas)
	nonce := uint64(result.Nonce)
	gasPrice := (*big.Int)(result.GasPrice)
	value := (*big.Int)(result.Value)
	v := (*big.Int)(result.V)
	r := (*big.Int)(result.R)
	s := (*big.Int)(result.S)
	hash := common.Hash(result.Hash)
	to := copyAddressPtr(result.To)
	var data []byte = result.Input
	arblegacy := ArbitrumLegacyTxData{
		Gas:      gas,
		GasPrice: gasPrice,
		Hash:     hash,
		Data:     data,
		Nonce:    nonce,
		To:       to,
		Value:    value,
		V:        v,
		R:        r,
		S:        s,
	}
	return NewTx(&arblegacy)
}

// accessors for innerTx.
func (tx *ArbitrumLegacyTxData) txType() byte           { return ArbitrumLegacyTxType }
func (tx *ArbitrumLegacyTxData) chainID() *big.Int      { return deriveChainId(tx.V) }
func (tx *ArbitrumLegacyTxData) accessList() AccessList { return nil }
func (tx *ArbitrumLegacyTxData) data() []byte           { return tx.Data }
func (tx *ArbitrumLegacyTxData) gas() uint64            { return tx.Gas }
func (tx *ArbitrumLegacyTxData) gasPrice() *big.Int     { return tx.GasPrice }
func (tx *ArbitrumLegacyTxData) gasTipCap() *big.Int    { return tx.GasPrice }
func (tx *ArbitrumLegacyTxData) gasFeeCap() *big.Int    { return tx.GasPrice }
func (tx *ArbitrumLegacyTxData) value() *big.Int        { return tx.Value }
func (tx *ArbitrumLegacyTxData) nonce() uint64          { return tx.Nonce }
func (tx *ArbitrumLegacyTxData) to() *common.Address    { return tx.To }

func (tx *ArbitrumLegacyTxData) isFake() bool { return false }

func (tx *ArbitrumLegacyTxData) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

func (tx *ArbitrumLegacyTxData) setSignatureValues(chainID, v, r, s *big.Int) {
	tx.V, tx.R, tx.S = v, r, s
}

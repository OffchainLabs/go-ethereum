package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

var bigZero = big.NewInt(0)

func (tx *LegacyTx) isFake() bool     { return false }
func (tx *AccessListTx) isFake() bool { return false }
func (tx *DynamicFeeTx) isFake() bool { return false }

type ArbitrumUnsignedTx struct {
	ChainId *big.Int
	From    common.Address

	Nonce    uint64          // nonce of sender account
	GasPrice *big.Int        // wei per gas
	Gas      uint64          // gas limit
	To       *common.Address `rlp:"nil"` // nil means contract creation
	Value    *big.Int        // wei amount
	Data     []byte          // contract invocation input data
}

func (tx *ArbitrumUnsignedTx) txType() byte { return ArbitrumUnsignedTxType }

func (tx *ArbitrumUnsignedTx) copy() TxData {
	cpy := &ArbitrumUnsignedTx{
		ChainId:  new(big.Int),
		Nonce:    tx.Nonce,
		GasPrice: new(big.Int),
		Gas:      tx.Gas,
		To:       nil,
		Value:    new(big.Int),
		Data:     common.CopyBytes(tx.Data),
	}
	if tx.ChainId != nil {
		cpy.ChainId.Set(tx.ChainId)
	}
	if tx.GasPrice != nil {
		cpy.GasPrice.Set(tx.GasPrice)
	}
	if tx.To != nil {
		tmp := *tx.To
		cpy.To = &tmp
	}
	if tx.Value != nil {
		cpy.Value.Set(tx.Value)
	}
	return cpy
}

func (tx *ArbitrumUnsignedTx) chainID() *big.Int      { return tx.ChainId }
func (tx *ArbitrumUnsignedTx) accessList() AccessList { return nil }
func (tx *ArbitrumUnsignedTx) data() []byte           { return tx.Data }
func (tx *ArbitrumUnsignedTx) gas() uint64            { return tx.Gas }
func (tx *ArbitrumUnsignedTx) gasPrice() *big.Int     { return tx.GasPrice }
func (tx *ArbitrumUnsignedTx) gasTipCap() *big.Int    { return tx.GasPrice }
func (tx *ArbitrumUnsignedTx) gasFeeCap() *big.Int    { return tx.GasPrice }
func (tx *ArbitrumUnsignedTx) value() *big.Int        { return tx.Value }
func (tx *ArbitrumUnsignedTx) nonce() uint64          { return tx.Nonce }
func (tx *ArbitrumUnsignedTx) to() *common.Address    { return tx.To }
func (tx *ArbitrumUnsignedTx) isFake() bool           { return false }

func (tx *ArbitrumUnsignedTx) rawSignatureValues() (v, r, s *big.Int) {
	return bigZero, bigZero, bigZero
}

func (tx *ArbitrumUnsignedTx) setSignatureValues(chainID, v, r, s *big.Int) {

}

type ArbitrumContractTx struct {
	ChainId   *big.Int
	RequestId common.Hash
	From      common.Address

	GasPrice *big.Int        // wei per gas
	Gas      uint64          // gas limit
	To       *common.Address `rlp:"nil"` // nil means contract creation
	Value    *big.Int        // wei amount
	Data     []byte          // contract invocation input data
}

func (tx *ArbitrumContractTx) txType() byte { return ArbitrumContractTxType }

func (tx *ArbitrumContractTx) copy() TxData {
	cpy := &ArbitrumContractTx{
		ChainId:   new(big.Int),
		RequestId: tx.RequestId,
		GasPrice:  new(big.Int),
		Gas:       tx.Gas,
		To:        nil,
		Value:     new(big.Int),
		Data:      common.CopyBytes(tx.Data),
	}
	if tx.ChainId != nil {
		cpy.ChainId.Set(tx.ChainId)
	}
	if tx.GasPrice != nil {
		cpy.GasPrice.Set(tx.GasPrice)
	}
	if tx.To != nil {
		tmp := *tx.To
		cpy.To = &tmp
	}
	if tx.Value != nil {
		cpy.Value.Set(tx.Value)
	}
	return cpy
}

func (tx *ArbitrumContractTx) chainID() *big.Int      { return tx.ChainId }
func (tx *ArbitrumContractTx) accessList() AccessList { return nil }
func (tx *ArbitrumContractTx) data() []byte           { return tx.Data }
func (tx *ArbitrumContractTx) gas() uint64            { return tx.Gas }
func (tx *ArbitrumContractTx) gasPrice() *big.Int     { return tx.GasPrice }
func (tx *ArbitrumContractTx) gasTipCap() *big.Int    { return tx.GasPrice }
func (tx *ArbitrumContractTx) gasFeeCap() *big.Int    { return tx.GasPrice }
func (tx *ArbitrumContractTx) value() *big.Int        { return tx.Value }
func (tx *ArbitrumContractTx) nonce() uint64          { return 0 }
func (tx *ArbitrumContractTx) to() *common.Address    { return tx.To }
func (tx *ArbitrumContractTx) rawSignatureValues() (v, r, s *big.Int) {
	return bigZero, bigZero, bigZero
}
func (tx *ArbitrumContractTx) setSignatureValues(chainID, v, r, s *big.Int) {}
func (tx *ArbitrumContractTx) isFake() bool                                 { return true }

type ArbitrumDepositTx struct {
	ChainId     *big.Int
	L1RequestId common.Hash
	To          common.Address
	Value       *big.Int
}

func (d *ArbitrumDepositTx) txType() byte {
	return ArbitrumDepositTxType
}

func (d *ArbitrumDepositTx) copy() TxData {
	tx := &ArbitrumDepositTx{
		ChainId: new(big.Int),
		To:      d.To,
		Value:   new(big.Int),
	}
	if d.ChainId != nil {
		tx.ChainId.Set(d.ChainId)
	}
	if d.Value != nil {
		tx.Value.Set(d.Value)
	}
	return tx
}

func (d *ArbitrumDepositTx) chainID() *big.Int      { return d.ChainId }
func (d *ArbitrumDepositTx) accessList() AccessList { return nil }
func (d *ArbitrumDepositTx) data() []byte           { return nil }
func (d ArbitrumDepositTx) gas() uint64             { return 0 }
func (d *ArbitrumDepositTx) gasPrice() *big.Int     { return bigZero }
func (d *ArbitrumDepositTx) gasTipCap() *big.Int    { return bigZero }
func (d *ArbitrumDepositTx) gasFeeCap() *big.Int    { return bigZero }
func (d *ArbitrumDepositTx) value() *big.Int        { return d.Value }
func (d *ArbitrumDepositTx) nonce() uint64          { return 0 }
func (d *ArbitrumDepositTx) to() *common.Address    { return &d.To }
func (d *ArbitrumDepositTx) isFake() bool           { return true }

func (d *ArbitrumDepositTx) rawSignatureValues() (v, r, s *big.Int) {
	return bigZero, bigZero, bigZero
}

func (d *ArbitrumDepositTx) setSignatureValues(chainID, v, r, s *big.Int) {

}

type ArbitrumWrappedTx struct {
	l1Calldata uint64
	TxData
}

func (tx *ArbitrumWrappedTx) copy() TxData {
	cpy := &ArbitrumWrappedTx{
		l1Calldata: tx.l1Calldata,
		TxData:     tx.TxData.copy(),
	}
	return cpy
}

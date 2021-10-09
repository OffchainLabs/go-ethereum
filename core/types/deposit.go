package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

var bigZero = big.NewInt(0)

type DepositTx struct {
	ChainId *big.Int
	L1RequestId common.Hash
	To    common.Address
	Value *big.Int
}

func (d *DepositTx) txType() byte {
	return ArbitrumDepositTxType
}

func (d *DepositTx) copy() TxData {
	tx := &DepositTx{
		ChainId: new(big.Int),
		To:    d.To,
		Value: new(big.Int),
	}
	if d.ChainId != nil {
		tx.ChainId.Set(d.ChainId)
	}
	if d.Value != nil {
		tx.Value.Set(d.Value)
	}
	return tx
}

func (d *DepositTx) chainID() *big.Int {
	return d.ChainId
}

func (d *DepositTx) accessList() AccessList {
	return nil
}

func (d *DepositTx) data() []byte {
	return nil
}

func (d DepositTx) gas() uint64 {
	return 0
}

func (d *DepositTx) gasPrice() *big.Int {
	return bigZero
}

func (d *DepositTx) gasTipCap() *big.Int {
	return bigZero
}

func (d *DepositTx) gasFeeCap() *big.Int {
	return bigZero
}

func (d *DepositTx) value() *big.Int {
	return d.Value
}

func (d *DepositTx) nonce() uint64 {
	return 0
}

func (d *DepositTx) to() *common.Address {
	return &d.To
}

func (d *DepositTx) rawSignatureValues() (v, r, s *big.Int) {
	return bigZero, bigZero, bigZero
}

func (d *DepositTx) setSignatureValues(chainID, v, r, s *big.Int) {

}


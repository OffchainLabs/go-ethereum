package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

var arbAddress = common.HexToAddress("0xabc")

type arbitrumSigner struct { eip2930Signer }

func NewArbitrumSigner(chainId *big.Int) Signer {
	return arbitrumSigner{eip2930Signer{NewEIP155Signer(chainId)}}
}

func (s arbitrumSigner) Sender(tx *Transaction) (common.Address, error) {
	switch tx.inner.(type) {
	case *DepositTx:
		return arbAddress, nil
	default:
		return s.eip2930Signer.Sender(tx)
	}
}

func (s arbitrumSigner) Equal(s2 Signer) bool {
	x, ok := s2.(londonSigner)
	return ok && x.chainId.Cmp(s.chainId) == 0
}

func (s arbitrumSigner) SignatureValues(tx *Transaction, sig []byte) (R, S, V *big.Int, err error) {
	_, ok := tx.inner.(*DepositTx)
	if !ok {
		return s.eip2930Signer.SignatureValues(tx, sig)
	}
	return bigZero, bigZero, bigZero, nil
}

// Hash returns the hash to be signed by the sender.
// It does not uniquely identify the transaction.
func (s arbitrumSigner) Hash(tx *Transaction) common.Hash {
	if tx.Type() != ArbitrumDepositTxType {
		return s.eip2930Signer.Hash(tx)
	}
	return prefixedRlpHash(
		tx.Type(),
		[]interface{}{
			s.chainId,
			tx.Nonce(),
			tx.GasTipCap(),
			tx.GasFeeCap(),
			tx.Gas(),
			tx.To(),
			tx.Value(),
			tx.Data(),
			tx.AccessList(),
		})
}

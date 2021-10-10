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
	switch inner := tx.inner.(type) {
	case *ArbitrumUnsignedTx:
		return inner.From, nil
	case *ArbitrumContractTx:
		return inner.From, nil
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
	switch tx.inner.(type) {
	case *ArbitrumUnsignedTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumContractTx:
		return bigZero, bigZero, bigZero, nil
	case *DepositTx:
		return bigZero, bigZero, bigZero, nil
	default:
		return s.eip2930Signer.SignatureValues(tx, sig)
	}
}

// Hash returns the hash to be signed by the sender.
// It does not uniquely identify the transaction.
func (s arbitrumSigner) Hash(tx *Transaction) common.Hash {
	return s.eip2930Signer.Hash(tx)
}

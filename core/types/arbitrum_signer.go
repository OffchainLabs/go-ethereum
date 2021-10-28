package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

var arbAddress = common.HexToAddress("0xabc")

type arbitrumSigner struct{ Signer }

func NewArbitrumSigner(signer Signer) Signer {
	return arbitrumSigner{Signer: signer}
}

func (s arbitrumSigner) Sender(tx *Transaction) (common.Address, error) {
	switch inner := tx.inner.(type) {
	case *ArbitrumUnsignedTx:
		return inner.From, nil
	case *ArbitrumContractTx:
		return inner.From, nil
	case *ArbitrumDepositTx:
		return arbAddress, nil
	case *ArbitrumRetryTx:
		return inner.From, nil
	default:
		return s.Signer.Sender(tx)
	}
}

func (s arbitrumSigner) Equal(s2 Signer) bool {
	x, ok := s2.(arbitrumSigner)
	return ok && x.Signer.Equal(s.Signer)
}

func (s arbitrumSigner) SignatureValues(tx *Transaction, sig []byte) (R, S, V *big.Int, err error) {
	switch tx.inner.(type) {
	case *ArbitrumUnsignedTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumContractTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumDepositTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumRetryTx:
		return bigZero, bigZero, bigZero, nil
	default:
		return s.Signer.SignatureValues(tx, sig)
	}
}

// Hash returns the hash to be signed by the sender.
// It does not uniquely identify the transaction.
func (s arbitrumSigner) Hash(tx *Transaction) common.Hash {
	return s.Signer.Hash(tx)
}

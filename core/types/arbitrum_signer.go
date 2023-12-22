package types

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

var ArbosAddress = common.HexToAddress("0xa4b05")
var ArbSysAddress = common.HexToAddress("0x64")
var ArbGasInfoAddress = common.HexToAddress("0x6c")
var ArbRetryableTxAddress = common.HexToAddress("0x6e")
var NodeInterfaceAddress = common.HexToAddress("0xc8")
var NodeInterfaceDebugAddress = common.HexToAddress("0xc9")

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
		return inner.From, nil
	case *ArbitrumInternalTx:
		return ArbosAddress, nil
	case *ArbitrumRetryTx:
		return inner.From, nil
	case *ArbitrumSubmitRetryableTx:
		return inner.From, nil
	case *ArbitrumLegacyTxData:
		legacyData := tx.inner.(*ArbitrumLegacyTxData)
		if legacyData.Sender != nil {
			return *legacyData.Sender, nil
		}
		fakeTx := NewTx(&legacyData.LegacyTx)
		return s.Signer.Sender(fakeTx)
	case *ArbitrumSubtypedTx:
		switch inner.TxData.(type) {
		case *ArbitrumTippingTx:
			V, R, S := tx.RawSignatureValues()
			// DynamicFee txs are defined to use 0 and 1 as their recovery
			// id, add 27 to become equivalent to unprotected Homestead signatures.
			V = new(big.Int).Add(V, big.NewInt(27))
			if tx.ChainId().Cmp(s.Signer.ChainID()) != 0 {
				return common.Address{}, fmt.Errorf("%w: have %d want %d", ErrInvalidChainId, tx.ChainId(), s.Signer.ChainID())
			}
			return recoverPlain(s.Hash(tx), R, S, V, true)
		default:
			return common.Address{}, ErrTxTypeNotSupported
		}
	default:
		return s.Signer.Sender(tx)
	}
}

func (s arbitrumSigner) Equal(s2 Signer) bool {
	x, ok := s2.(arbitrumSigner)
	return ok && x.Signer.Equal(s.Signer)
}

func (s arbitrumSigner) SignatureValues(tx *Transaction, sig []byte) (R, S, V *big.Int, err error) {
	switch inner := tx.inner.(type) {
	case *ArbitrumUnsignedTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumContractTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumDepositTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumInternalTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumRetryTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumSubmitRetryableTx:
		return bigZero, bigZero, bigZero, nil
	case *ArbitrumLegacyTxData:
		fakeTx := NewTx(&inner.LegacyTx)
		return s.Signer.SignatureValues(fakeTx, sig)
	case *ArbitrumSubtypedTx:
		switch txdata := inner.TxData.(type) {
		case *ArbitrumTippingTx:
			// Check that chain ID of tx matches the signer. We also accept ID zero here,
			// because it indicates that the chain ID was not specified in the tx.
			if txdata.ChainID.Sign() != 0 && txdata.ChainID.Cmp(s.Signer.ChainID()) != 0 {
				return nil, nil, nil, fmt.Errorf("%w: have %d want %d", ErrInvalidChainId, txdata.ChainID, s.Signer.ChainID())
			}
			R, S, _ = decodeSignature(sig)
			V = big.NewInt(int64(sig[64]))
			return R, S, V, nil
		default:
			return nil, nil, nil, ErrTxTypeNotSupported
		}
	default:
		return s.Signer.SignatureValues(tx, sig)
	}
}

// Hash returns the hash to be signed by the sender.
// It does not uniquely identify the transaction.
func (s arbitrumSigner) Hash(tx *Transaction) common.Hash {
	switch inner := tx.inner.(type) {
	case *ArbitrumLegacyTxData:
		fakeTx := NewTx(&inner.LegacyTx)
		return s.Signer.Hash(fakeTx)
	case *ArbitrumSubtypedTx:
		switch inner.TxData.(type) {
		case *ArbitrumTippingTx:
			return prefixedRlpHash(
				tx.Type(),
				[]interface{}{
					inner.TxSubtype(),
					s.Signer.ChainID(),
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
	}
	return s.Signer.Hash(tx)
}

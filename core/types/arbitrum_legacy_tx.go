package types

import (
	"bytes"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

type ArbitrumLegacyTxData struct {
	LegacyTx
	HashOverride      common.Hash // Hash cannot be locally computed from other fields
	EffectiveGasPrice uint64
	L1BlockNumber     uint64
}

func NewArbitrumLegacyTx(origTx *Transaction, hashOverride common.Hash, effectiveGas uint64, l1Block uint64) (*Transaction, error) {
	if origTx.Type() != LegacyTxType {
		return nil, errors.New("attempt to arbitrum-wrap non-legacy transaction")
	}
	legacyPtr := origTx.GetInner().(*LegacyTx)
	inner := ArbitrumLegacyTxData{
		LegacyTx:          *legacyPtr,
		HashOverride:      hashOverride,
		EffectiveGasPrice: effectiveGas,
		L1BlockNumber:     l1Block,
	}
	return NewTx(&inner), nil
}

func (tx *ArbitrumLegacyTxData) copy() TxData {
	legacyCopy := tx.LegacyTx.copy().(*LegacyTx)
	return &ArbitrumLegacyTxData{
		LegacyTx:          *legacyCopy,
		HashOverride:      tx.HashOverride,
		EffectiveGasPrice: tx.EffectiveGasPrice,
		L1BlockNumber:     tx.L1BlockNumber,
	}
}

func (tx *ArbitrumLegacyTxData) txType() byte { return ArbitrumLegacyTxType }

func (tx *ArbitrumLegacyTxData) EncodeOnlyLegacyInto(w *bytes.Buffer) {
	rlp.Encode(w, tx.LegacyTx)
}

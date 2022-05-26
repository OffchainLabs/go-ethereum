package types

import (
	"bytes"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

type ArbitrumLegacyTxData struct {
	LegacyTx
	HashOverride common.Hash // Hash cannot be locally computed from other fields
}

func NewArbitrumLegacyTx(origTx *Transaction, hashOverride common.Hash) (*Transaction, error) {
	if origTx.Type() != LegacyTxType {
		return nil, errors.New("attempt to arbitrum-wrap non-legacy transaction")
	}
	legacyCopy := origTx.GetInner().(*LegacyTx)
	inner := ArbitrumLegacyTxData{
		LegacyTx:     *legacyCopy,
		HashOverride: hashOverride,
	}
	return NewTx(&inner), nil
}

func (tx *ArbitrumLegacyTxData) copy() TxData {
	legacyCopy := tx.LegacyTx.copy().(*LegacyTx)
	return &ArbitrumLegacyTxData{
		LegacyTx:     *legacyCopy,
		HashOverride: tx.HashOverride,
	}
}

func (tx *ArbitrumLegacyTxData) txType() byte { return ArbitrumLegacyTxType }

func (tx *ArbitrumLegacyTxData) EncodeOnlyLegacyInto(w *bytes.Buffer) {
	rlp.Encode(w, tx.LegacyTx)
}

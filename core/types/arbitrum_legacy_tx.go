package types

import (
	"bytes"
	"errors"
	"io"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

type ArbitrumLegacyTxData struct {
	LegacyTx
	HashOverride      common.Hash // Hash cannot be locally computed from other fields
	EffectiveGasPrice uint64
	L1BlockNumber     uint64
	Sender            *common.Address // only used in unsigned Txs
}

type arbitrumLegacyTxDataNoSenderRLP struct {
	LegacyTx
	HashOverride      common.Hash // Hash cannot be locally computed from other fields
	EffectiveGasPrice uint64
	L1BlockNumber     uint64
}

type arbitrumLegacyTxDataWithSenderRLP struct {
	LegacyTx
	HashOverride      common.Hash // Hash cannot be locally computed from other fields
	EffectiveGasPrice uint64
	L1BlockNumber     uint64
	Sender            common.Address // only used in unsigned Txs
}

func NewArbitrumLegacyTx(origTx *Transaction, hashOverride common.Hash, effectiveGas uint64, l1Block uint64, senderOverride *common.Address) (*Transaction, error) {
	if origTx.Type() != LegacyTxType {
		return nil, errors.New("attempt to arbitrum-wrap non-legacy transaction")
	}
	legacyPtr := origTx.GetInner().(*LegacyTx)
	inner := ArbitrumLegacyTxData{
		LegacyTx:          *legacyPtr,
		HashOverride:      hashOverride,
		EffectiveGasPrice: effectiveGas,
		L1BlockNumber:     l1Block,
		Sender:            senderOverride,
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
		Sender:            tx.Sender,
	}
}

func (tx *ArbitrumLegacyTxData) txType() byte { return ArbitrumLegacyTxType }

func (tx *ArbitrumLegacyTxData) EncodeOnlyLegacyInto(w *bytes.Buffer) {
	rlp.Encode(w, tx.LegacyTx)
}

func (tx *ArbitrumLegacyTxData) EncodeRLP(w io.Writer) error {
	if tx.Sender == nil {
		forRLP := arbitrumLegacyTxDataNoSenderRLP{
			LegacyTx:          tx.LegacyTx,
			HashOverride:      tx.HashOverride,
			EffectiveGasPrice: tx.EffectiveGasPrice,
			L1BlockNumber:     tx.L1BlockNumber,
		}
		return rlp.Encode(w, forRLP)
	}
	forRLP := arbitrumLegacyTxDataWithSenderRLP{
		LegacyTx:          tx.LegacyTx,
		HashOverride:      tx.HashOverride,
		EffectiveGasPrice: tx.EffectiveGasPrice,
		L1BlockNumber:     tx.L1BlockNumber,
		Sender:            *tx.Sender,
	}
	return rlp.Encode(w, forRLP)
}

// DecodeRLP implements rlp.Decoder
func (tx *ArbitrumLegacyTxData) DecodeRLP(s *rlp.Stream) error {
	raw, err := s.Raw()
	if err != nil {
		return err
	}
	var withSender arbitrumLegacyTxDataWithSenderRLP
	rawcopy := make([]byte, len(raw))
	copy(rawcopy, raw)
	err = rlp.DecodeBytes(rawcopy, &withSender)
	if err == nil {
		tx.LegacyTx = withSender.LegacyTx
		tx.HashOverride = withSender.HashOverride
		tx.EffectiveGasPrice = withSender.EffectiveGasPrice
		tx.L1BlockNumber = withSender.L1BlockNumber
		tx.Sender = new(common.Address)
		*tx.Sender = withSender.Sender
		return nil
	}
	var noSender arbitrumLegacyTxDataNoSenderRLP
	err = rlp.DecodeBytes(raw[:], &noSender)
	if err == nil {
		tx.LegacyTx = noSender.LegacyTx
		tx.HashOverride = noSender.HashOverride
		tx.EffectiveGasPrice = noSender.EffectiveGasPrice
		tx.L1BlockNumber = noSender.L1BlockNumber
		tx.Sender = nil
		return nil
	}
	return err
}

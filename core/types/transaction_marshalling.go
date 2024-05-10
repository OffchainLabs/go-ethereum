// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package types

import (
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/holiman/uint256"
)

// txJSON is the JSON representation of transactions.
type txJSON struct {
	Type hexutil.Uint64 `json:"type"`

	ChainID              *hexutil.Big    `json:"chainId,omitempty"`
	Nonce                *hexutil.Uint64 `json:"nonce"`
	To                   *common.Address `json:"to"`
	Gas                  *hexutil.Uint64 `json:"gas"`
	GasPrice             *hexutil.Big    `json:"gasPrice"`
	MaxPriorityFeePerGas *hexutil.Big    `json:"maxPriorityFeePerGas"`
	MaxFeePerGas         *hexutil.Big    `json:"maxFeePerGas"`
	MaxFeePerBlobGas     *hexutil.Big    `json:"maxFeePerBlobGas,omitempty"`
	Value                *hexutil.Big    `json:"value"`
	Input                *hexutil.Bytes  `json:"input"`
	AccessList           *AccessList     `json:"accessList,omitempty"`
	BlobVersionedHashes  []common.Hash   `json:"blobVersionedHashes,omitempty"`
	V                    *hexutil.Big    `json:"v"`
	R                    *hexutil.Big    `json:"r"`
	S                    *hexutil.Big    `json:"s"`
	YParity              *hexutil.Uint64 `json:"yParity,omitempty"`

	// Blob transaction sidecar encoding:
	Blobs       []kzg4844.Blob       `json:"blobs,omitempty"`
	Commitments []kzg4844.Commitment `json:"commitments,omitempty"`
	Proofs      []kzg4844.Proof      `json:"proofs,omitempty"`

	// Arbitrum fields:
	From                *common.Address `json:"from,omitempty"`                // Contract SubmitRetryable Unsigned Retry
	RequestId           *common.Hash    `json:"requestId,omitempty"`           // Contract SubmitRetryable Deposit
	TicketId            *common.Hash    `json:"ticketId,omitempty"`            // Retry
	MaxRefund           *hexutil.Big    `json:"maxRefund,omitempty"`           // Retry
	SubmissionFeeRefund *hexutil.Big    `json:"submissionFeeRefund,omitempty"` // Retry
	RefundTo            *common.Address `json:"refundTo,omitempty"`            // SubmitRetryable Retry
	L1BaseFee           *hexutil.Big    `json:"l1BaseFee,omitempty"`           // SubmitRetryable
	DepositValue        *hexutil.Big    `json:"depositValue,omitempty"`        // SubmitRetryable
	RetryTo             *common.Address `json:"retryTo,omitempty"`             // SubmitRetryable
	RetryValue          *hexutil.Big    `json:"retryValue,omitempty"`          // SubmitRetryable
	RetryData           *hexutil.Bytes  `json:"retryData,omitempty"`           // SubmitRetryable
	Beneficiary         *common.Address `json:"beneficiary,omitempty"`         // SubmitRetryable
	MaxSubmissionFee    *hexutil.Big    `json:"maxSubmissionFee,omitempty"`    // SubmitRetryable
	EffectiveGasPrice   *hexutil.Uint64 `json:"effectiveGasPrice,omitempty"`   // ArbLegacy
	L1BlockNumber       *hexutil.Uint64 `json:"l1BlockNumber,omitempty"`       // ArbLegacy

	// Only used for encoding - and for ArbLegacy
	Hash common.Hash `json:"hash"`
}

// yParityValue returns the YParity value from JSON. For backwards-compatibility reasons,
// this can be given in the 'v' field or the 'yParity' field. If both exist, they must match.
func (tx *txJSON) yParityValue() (*big.Int, error) {
	if tx.YParity != nil {
		val := uint64(*tx.YParity)
		if val != 0 && val != 1 {
			return nil, errInvalidYParity
		}
		bigval := new(big.Int).SetUint64(val)
		if tx.V != nil && tx.V.ToInt().Cmp(bigval) != 0 {
			return nil, errVYParityMismatch
		}
		return bigval, nil
	}
	if tx.V != nil {
		return tx.V.ToInt(), nil
	}
	return nil, errVYParityMissing
}

// MarshalJSON marshals as JSON with a hash.
func (tx *Transaction) MarshalJSON() ([]byte, error) {
	var enc txJSON
	// These are set for all tx types.
	enc.Hash = tx.Hash()
	enc.Type = hexutil.Uint64(tx.Type())

	// Arbitrum: set to 0 for compatibility
	var zero uint64
	enc.Nonce = (*hexutil.Uint64)(&zero)
	enc.Gas = (*hexutil.Uint64)(&zero)
	enc.GasPrice = (*hexutil.Big)(common.Big0)
	enc.Value = (*hexutil.Big)(common.Big0)
	enc.Input = (*hexutil.Bytes)(&[]byte{})
	enc.V = (*hexutil.Big)(common.Big0)
	enc.R = (*hexutil.Big)(common.Big0)
	enc.S = (*hexutil.Big)(common.Big0)

	// Other fields are set conditionally depending on tx type.
	switch itx := tx.inner.(type) {
	case *LegacyTx:
		enc.Nonce = (*hexutil.Uint64)(&itx.Nonce)
		enc.To = tx.To()
		enc.Gas = (*hexutil.Uint64)(&itx.Gas)
		enc.GasPrice = (*hexutil.Big)(itx.GasPrice)
		enc.Value = (*hexutil.Big)(itx.Value)
		enc.Input = (*hexutil.Bytes)(&itx.Data)
		enc.V = (*hexutil.Big)(itx.V)
		enc.R = (*hexutil.Big)(itx.R)
		enc.S = (*hexutil.Big)(itx.S)
		if tx.Protected() {
			enc.ChainID = (*hexutil.Big)(tx.ChainId())
		}

	case *AccessListTx:
		enc.ChainID = (*hexutil.Big)(itx.ChainID)
		enc.Nonce = (*hexutil.Uint64)(&itx.Nonce)
		enc.To = tx.To()
		enc.Gas = (*hexutil.Uint64)(&itx.Gas)
		enc.GasPrice = (*hexutil.Big)(itx.GasPrice)
		enc.Value = (*hexutil.Big)(itx.Value)
		enc.Input = (*hexutil.Bytes)(&itx.Data)
		enc.AccessList = &itx.AccessList
		enc.V = (*hexutil.Big)(itx.V)
		enc.R = (*hexutil.Big)(itx.R)
		enc.S = (*hexutil.Big)(itx.S)
		yparity := itx.V.Uint64()
		enc.YParity = (*hexutil.Uint64)(&yparity)

	case *DynamicFeeTx:
		enc.ChainID = (*hexutil.Big)(itx.ChainID)
		enc.Nonce = (*hexutil.Uint64)(&itx.Nonce)
		enc.To = tx.To()
		enc.Gas = (*hexutil.Uint64)(&itx.Gas)
		enc.MaxFeePerGas = (*hexutil.Big)(itx.GasFeeCap)
		enc.MaxPriorityFeePerGas = (*hexutil.Big)(itx.GasTipCap)
		enc.Value = (*hexutil.Big)(itx.Value)
		enc.Input = (*hexutil.Bytes)(&itx.Data)
		enc.AccessList = &itx.AccessList
		enc.V = (*hexutil.Big)(itx.V)
		enc.R = (*hexutil.Big)(itx.R)
		enc.S = (*hexutil.Big)(itx.S)
		yparity := itx.V.Uint64()
		enc.YParity = (*hexutil.Uint64)(&yparity)

	case *ArbitrumLegacyTxData:
		enc.Nonce = (*hexutil.Uint64)(&itx.Nonce)
		enc.Gas = (*hexutil.Uint64)(&itx.Gas)
		enc.GasPrice = (*hexutil.Big)(itx.GasPrice)
		enc.Value = (*hexutil.Big)(itx.Value)
		enc.Input = (*hexutil.Bytes)(&itx.Data)
		enc.To = tx.To()
		enc.V = (*hexutil.Big)(itx.V)
		enc.R = (*hexutil.Big)(itx.R)
		enc.S = (*hexutil.Big)(itx.S)
		enc.EffectiveGasPrice = (*hexutil.Uint64)(&itx.EffectiveGasPrice)
		enc.L1BlockNumber = (*hexutil.Uint64)(&itx.L1BlockNumber)
		enc.From = itx.Sender
	case *ArbitrumInternalTx:
		enc.ChainID = (*hexutil.Big)(itx.ChainId)
		enc.Input = (*hexutil.Bytes)(&itx.Data)
	case *ArbitrumDepositTx:
		enc.RequestId = &itx.L1RequestId
		enc.From = &itx.From
		enc.ChainID = (*hexutil.Big)(itx.ChainId)
		enc.Value = (*hexutil.Big)(itx.Value)
		enc.To = tx.To()
	case *ArbitrumUnsignedTx:
		enc.From = &itx.From
		enc.ChainID = (*hexutil.Big)(itx.ChainId)
		enc.Nonce = (*hexutil.Uint64)(&itx.Nonce)
		enc.Gas = (*hexutil.Uint64)(&itx.Gas)
		enc.MaxFeePerGas = (*hexutil.Big)(itx.GasFeeCap)
		enc.Value = (*hexutil.Big)(itx.Value)
		enc.Input = (*hexutil.Bytes)(&itx.Data)
		enc.To = tx.To()
	case *ArbitrumContractTx:
		enc.RequestId = &itx.RequestId
		enc.From = &itx.From
		enc.ChainID = (*hexutil.Big)(itx.ChainId)
		enc.Gas = (*hexutil.Uint64)(&itx.Gas)
		enc.MaxFeePerGas = (*hexutil.Big)(itx.GasFeeCap)
		enc.Value = (*hexutil.Big)(itx.Value)
		enc.Input = (*hexutil.Bytes)(&itx.Data)
		enc.To = tx.To()
	case *ArbitrumRetryTx:
		enc.From = &itx.From
		enc.TicketId = &itx.TicketId
		enc.RefundTo = &itx.RefundTo
		enc.ChainID = (*hexutil.Big)(itx.ChainId)
		enc.Nonce = (*hexutil.Uint64)(&itx.Nonce)
		enc.Gas = (*hexutil.Uint64)(&itx.Gas)
		enc.MaxFeePerGas = (*hexutil.Big)(itx.GasFeeCap)
		enc.Value = (*hexutil.Big)(itx.Value)
		enc.Input = (*hexutil.Bytes)(&itx.Data)
		enc.MaxRefund = (*hexutil.Big)(itx.MaxRefund)
		enc.SubmissionFeeRefund = (*hexutil.Big)(itx.SubmissionFeeRefund)
		enc.To = tx.To()
	case *ArbitrumSubmitRetryableTx:
		enc.RequestId = &itx.RequestId
		enc.From = &itx.From
		enc.L1BaseFee = (*hexutil.Big)(itx.L1BaseFee)
		enc.DepositValue = (*hexutil.Big)(itx.DepositValue)
		enc.Beneficiary = &itx.Beneficiary
		enc.RefundTo = &itx.FeeRefundAddr
		enc.MaxSubmissionFee = (*hexutil.Big)(itx.MaxSubmissionFee)
		enc.ChainID = (*hexutil.Big)(itx.ChainId)
		enc.Gas = (*hexutil.Uint64)(&itx.Gas)
		enc.MaxFeePerGas = (*hexutil.Big)(itx.GasFeeCap)
		enc.RetryTo = itx.RetryTo
		enc.RetryValue = (*hexutil.Big)(itx.RetryValue)
		enc.RetryData = (*hexutil.Bytes)(&itx.RetryData)
		data := itx.data()
		enc.Input = (*hexutil.Bytes)(&data)
		enc.To = tx.To()

	case *BlobTx:
		enc.ChainID = (*hexutil.Big)(itx.ChainID.ToBig())
		enc.Nonce = (*hexutil.Uint64)(&itx.Nonce)
		enc.Gas = (*hexutil.Uint64)(&itx.Gas)
		enc.MaxFeePerGas = (*hexutil.Big)(itx.GasFeeCap.ToBig())
		enc.MaxPriorityFeePerGas = (*hexutil.Big)(itx.GasTipCap.ToBig())
		enc.MaxFeePerBlobGas = (*hexutil.Big)(itx.BlobFeeCap.ToBig())
		enc.Value = (*hexutil.Big)(itx.Value.ToBig())
		enc.Input = (*hexutil.Bytes)(&itx.Data)
		enc.AccessList = &itx.AccessList
		enc.BlobVersionedHashes = itx.BlobHashes
		enc.To = tx.To()
		enc.V = (*hexutil.Big)(itx.V.ToBig())
		enc.R = (*hexutil.Big)(itx.R.ToBig())
		enc.S = (*hexutil.Big)(itx.S.ToBig())
		yparity := itx.V.Uint64()
		enc.YParity = (*hexutil.Uint64)(&yparity)
		if sidecar := itx.Sidecar; sidecar != nil {
			enc.Blobs = itx.Sidecar.Blobs
			enc.Commitments = itx.Sidecar.Commitments
			enc.Proofs = itx.Sidecar.Proofs
		}
	}
	return json.Marshal(&enc)
}

// UnmarshalJSON unmarshals from JSON.
func (tx *Transaction) UnmarshalJSON(input []byte) error {
	var dec txJSON
	err := json.Unmarshal(input, &dec)
	if err != nil {
		return err
	}

	// Decode / verify fields according to transaction type.
	var inner TxData
	switch dec.Type {
	case LegacyTxType:
		var itx LegacyTx
		inner = &itx
		if dec.Nonce == nil {
			return errors.New("missing required field 'nonce' in transaction")
		}
		itx.Nonce = uint64(*dec.Nonce)
		if dec.To != nil {
			itx.To = dec.To
		}
		if dec.Gas == nil {
			return errors.New("missing required field 'gas' in transaction")
		}
		itx.Gas = uint64(*dec.Gas)
		if dec.GasPrice == nil {
			return errors.New("missing required field 'gasPrice' in transaction")
		}
		itx.GasPrice = (*big.Int)(dec.GasPrice)
		if dec.Value == nil {
			return errors.New("missing required field 'value' in transaction")
		}
		itx.Value = (*big.Int)(dec.Value)
		if dec.Input == nil {
			return errors.New("missing required field 'input' in transaction")
		}
		itx.Data = *dec.Input

		// signature R
		if dec.R == nil {
			return errors.New("missing required field 'r' in transaction")
		}
		itx.R = (*big.Int)(dec.R)
		// signature S
		if dec.S == nil {
			return errors.New("missing required field 's' in transaction")
		}
		itx.S = (*big.Int)(dec.S)
		// signature V
		if dec.V == nil {
			return errors.New("missing required field 'v' in transaction")
		}
		itx.V = (*big.Int)(dec.V)
		if itx.V.Sign() != 0 || itx.R.Sign() != 0 || itx.S.Sign() != 0 {
			if err := sanityCheckSignature(itx.V, itx.R, itx.S, true); err != nil {
				return err
			}
		}

	case AccessListTxType:
		var itx AccessListTx
		inner = &itx
		if dec.ChainID == nil {
			return errors.New("missing required field 'chainId' in transaction")
		}
		itx.ChainID = (*big.Int)(dec.ChainID)
		if dec.Nonce == nil {
			return errors.New("missing required field 'nonce' in transaction")
		}
		itx.Nonce = uint64(*dec.Nonce)
		if dec.To != nil {
			itx.To = dec.To
		}
		if dec.Gas == nil {
			return errors.New("missing required field 'gas' in transaction")
		}
		itx.Gas = uint64(*dec.Gas)
		if dec.GasPrice == nil {
			return errors.New("missing required field 'gasPrice' in transaction")
		}
		itx.GasPrice = (*big.Int)(dec.GasPrice)
		if dec.Value == nil {
			return errors.New("missing required field 'value' in transaction")
		}
		itx.Value = (*big.Int)(dec.Value)
		if dec.Input == nil {
			return errors.New("missing required field 'input' in transaction")
		}
		itx.Data = *dec.Input
		if dec.AccessList != nil {
			itx.AccessList = *dec.AccessList
		}

		// signature R
		if dec.R == nil {
			return errors.New("missing required field 'r' in transaction")
		}
		itx.R = (*big.Int)(dec.R)
		// signature S
		if dec.S == nil {
			return errors.New("missing required field 's' in transaction")
		}
		itx.S = (*big.Int)(dec.S)
		// signature V
		itx.V, err = dec.yParityValue()
		if err != nil {
			return err
		}
		if itx.V.Sign() != 0 || itx.R.Sign() != 0 || itx.S.Sign() != 0 {
			if err := sanityCheckSignature(itx.V, itx.R, itx.S, false); err != nil {
				return err
			}
		}

	case DynamicFeeTxType:
		var itx DynamicFeeTx
		inner = &itx
		if dec.ChainID == nil {
			return errors.New("missing required field 'chainId' in transaction")
		}
		itx.ChainID = (*big.Int)(dec.ChainID)
		if dec.Nonce == nil {
			return errors.New("missing required field 'nonce' in transaction")
		}
		itx.Nonce = uint64(*dec.Nonce)
		if dec.To != nil {
			itx.To = dec.To
		}
		if dec.Gas == nil {
			return errors.New("missing required field 'gas' for txdata")
		}
		itx.Gas = uint64(*dec.Gas)
		if dec.MaxPriorityFeePerGas == nil {
			return errors.New("missing required field 'maxPriorityFeePerGas' for txdata")
		}
		itx.GasTipCap = (*big.Int)(dec.MaxPriorityFeePerGas)
		if dec.MaxFeePerGas == nil {
			return errors.New("missing required field 'maxFeePerGas' for txdata")
		}
		itx.GasFeeCap = (*big.Int)(dec.MaxFeePerGas)
		if dec.Value == nil {
			return errors.New("missing required field 'value' in transaction")
		}
		itx.Value = (*big.Int)(dec.Value)
		if dec.Input == nil {
			return errors.New("missing required field 'input' in transaction")
		}
		itx.Data = *dec.Input
		if dec.AccessList != nil {
			itx.AccessList = *dec.AccessList
		}

		// signature R
		if dec.R == nil {
			return errors.New("missing required field 'r' in transaction")
		}
		itx.R = (*big.Int)(dec.R)
		// signature S
		if dec.S == nil {
			return errors.New("missing required field 's' in transaction")
		}
		itx.S = (*big.Int)(dec.S)
		// signature V
		itx.V, err = dec.yParityValue()
		if err != nil {
			return err
		}
		if itx.V.Sign() != 0 || itx.R.Sign() != 0 || itx.S.Sign() != 0 {
			if err := sanityCheckSignature(itx.V, itx.R, itx.S, false); err != nil {
				return err
			}
		}

	case ArbitrumLegacyTxType:
		var itx LegacyTx
		if dec.To != nil {
			itx.To = dec.To
		}
		if dec.Nonce == nil {
			return errors.New("missing required field 'nonce' in transaction")
		}
		itx.Nonce = uint64(*dec.Nonce)
		if dec.GasPrice == nil {
			return errors.New("missing required field 'gasPrice' in transaction")
		}
		itx.GasPrice = (*big.Int)(dec.GasPrice)
		if dec.Gas == nil {
			return errors.New("missing required field 'gas' in transaction")
		}
		itx.Gas = uint64(*dec.Gas)
		if dec.Value == nil {
			return errors.New("missing required field 'value' in transaction")
		}
		itx.Value = (*big.Int)(dec.Value)
		if dec.Input == nil {
			return errors.New("missing required field 'input' in transaction")
		}
		itx.Data = *dec.Input
		if dec.V == nil {
			return errors.New("missing required field 'v' in transaction")
		}
		itx.V = (*big.Int)(dec.V)
		if dec.R == nil {
			return errors.New("missing required field 'r' in transaction")
		}
		itx.R = (*big.Int)(dec.R)
		if dec.S == nil {
			return errors.New("missing required field 's' in transaction")
		}
		itx.S = (*big.Int)(dec.S)
		withSignature := itx.V.Sign() != 0 || itx.R.Sign() != 0 || itx.S.Sign() != 0
		if withSignature {
			if err := sanityCheckSignature(itx.V, itx.R, itx.S, true); err != nil {
				return err
			}
		}
		if dec.EffectiveGasPrice == nil {
			return errors.New("missing required field 'EffectiveGasPrice' in transaction")
		}
		if dec.L1BlockNumber == nil {
			return errors.New("missing required field 'L1BlockNumber' in transaction")
		}
		inner = &ArbitrumLegacyTxData{
			LegacyTx:          itx,
			HashOverride:      dec.Hash,
			EffectiveGasPrice: uint64(*dec.EffectiveGasPrice),
			L1BlockNumber:     uint64(*dec.L1BlockNumber),
			Sender:            dec.From,
		}

	case ArbitrumInternalTxType:
		if dec.ChainID == nil {
			return errors.New("missing required field 'chainId' in transaction")
		}
		if dec.Input == nil {
			return errors.New("missing required field 'input' in transaction")
		}
		inner = &ArbitrumInternalTx{
			ChainId: (*big.Int)(dec.ChainID),
			Data:    *dec.Input,
		}

	case ArbitrumDepositTxType:
		if dec.ChainID == nil {
			return errors.New("missing required field 'chainId' in transaction")
		}
		if dec.RequestId == nil {
			return errors.New("missing required field 'requestId' in transaction")
		}
		if dec.To == nil {
			return errors.New("missing required field 'to' in transaction")
		}
		if dec.From == nil {
			return errors.New("missing required field 'from' in transaction")
		}
		if dec.Value == nil {
			return errors.New("missing required field 'value' in transaction")
		}
		inner = &ArbitrumDepositTx{
			ChainId:     (*big.Int)(dec.ChainID),
			L1RequestId: *dec.RequestId,
			To:          *dec.To,
			From:        *dec.From,
			Value:       (*big.Int)(dec.Value),
		}

	case ArbitrumUnsignedTxType:
		if dec.ChainID == nil {
			return errors.New("missing required field 'chainId' in transaction")
		}
		if dec.From == nil {
			return errors.New("missing required field 'from' in transaction")
		}
		if dec.Nonce == nil {
			return errors.New("missing required field 'nonce' in transaction")
		}
		if dec.MaxFeePerGas == nil {
			return errors.New("missing required field 'maxFeePerGas' for txdata")
		}
		if dec.Gas == nil {
			return errors.New("missing required field 'gas' in txdata")
		}
		if dec.Value == nil {
			return errors.New("missing required field 'value' in transaction")
		}
		if dec.Input == nil {
			return errors.New("missing required field 'input' in transaction")
		}
		inner = &ArbitrumUnsignedTx{
			ChainId:   (*big.Int)(dec.ChainID),
			From:      *dec.From,
			Nonce:     uint64(*dec.Nonce),
			GasFeeCap: (*big.Int)(dec.MaxFeePerGas),
			Gas:       uint64(*dec.Gas),
			To:        dec.To,
			Value:     (*big.Int)(dec.Value),
			Data:      *dec.Input,
		}

	case ArbitrumContractTxType:
		if dec.ChainID == nil {
			return errors.New("missing required field 'chainId' in transaction")
		}
		if dec.RequestId == nil {
			return errors.New("missing required field 'requestId' in transaction")
		}
		if dec.From == nil {
			return errors.New("missing required field 'from' in transaction")
		}
		if dec.MaxFeePerGas == nil {
			return errors.New("missing required field 'maxFeePerGas' for txdata")
		}
		if dec.Gas == nil {
			return errors.New("missing required field 'gas' in txdata")
		}
		if dec.Value == nil {
			return errors.New("missing required field 'value' in transaction")
		}
		if dec.Input == nil {
			return errors.New("missing required field 'input' in transaction")
		}
		inner = &ArbitrumContractTx{
			ChainId:   (*big.Int)(dec.ChainID),
			RequestId: *dec.RequestId,
			From:      *dec.From,
			GasFeeCap: (*big.Int)(dec.MaxFeePerGas),
			Gas:       uint64(*dec.Gas),
			To:        dec.To,
			Value:     (*big.Int)(dec.Value),
			Data:      *dec.Input,
		}

	case ArbitrumRetryTxType:
		if dec.ChainID == nil {
			return errors.New("missing required field 'chainId' in transaction")
		}
		if dec.Nonce == nil {
			return errors.New("missing required field 'nonce' in transaction")
		}
		if dec.From == nil {
			return errors.New("missing required field 'from' in transaction")
		}
		if dec.MaxFeePerGas == nil {
			return errors.New("missing required field 'maxFeePerGas' for txdata")
		}
		if dec.Gas == nil {
			return errors.New("missing required field 'gas' in txdata")
		}
		if dec.Value == nil {
			return errors.New("missing required field 'value' in transaction")
		}
		if dec.Input == nil {
			return errors.New("missing required field 'input' in transaction")
		}
		if dec.TicketId == nil {
			return errors.New("missing required field 'ticketId' in transaction")
		}
		if dec.RefundTo == nil {
			return errors.New("missing required field 'refundTo' in transaction")
		}
		if dec.MaxRefund == nil {
			return errors.New("missing required field 'maxRefund' in transaction")
		}
		if dec.SubmissionFeeRefund == nil {
			return errors.New("missing required field 'submissionFeeRefund' in transaction")
		}
		inner = &ArbitrumRetryTx{
			ChainId:             (*big.Int)(dec.ChainID),
			Nonce:               uint64(*dec.Nonce),
			From:                *dec.From,
			GasFeeCap:           (*big.Int)(dec.MaxFeePerGas),
			Gas:                 uint64(*dec.Gas),
			To:                  dec.To,
			Value:               (*big.Int)(dec.Value),
			Data:                *dec.Input,
			TicketId:            *dec.TicketId,
			RefundTo:            *dec.RefundTo,
			MaxRefund:           (*big.Int)(dec.MaxRefund),
			SubmissionFeeRefund: (*big.Int)(dec.SubmissionFeeRefund),
		}

	case ArbitrumSubmitRetryableTxType:
		if dec.ChainID == nil {
			return errors.New("missing required field 'chainId' in transaction")
		}
		if dec.RequestId == nil {
			return errors.New("missing required field 'requestId' in transaction")
		}
		if dec.From == nil {
			return errors.New("missing required field 'from' in transaction")
		}
		if dec.L1BaseFee == nil {
			return errors.New("missing required field 'l1BaseFee' in transaction")
		}
		if dec.DepositValue == nil {
			return errors.New("missing required field 'depositValue' in transaction")
		}
		if dec.MaxFeePerGas == nil {
			return errors.New("missing required field 'maxFeePerGas' for txdata")
		}
		if dec.Gas == nil {
			return errors.New("missing required field 'gas' in txdata")
		}
		if dec.Beneficiary == nil {
			return errors.New("missing required field 'beneficiary' in transaction")
		}
		if dec.MaxSubmissionFee == nil {
			return errors.New("missing required field 'maxSubmissionFee' in transaction")
		}
		if dec.RefundTo == nil {
			return errors.New("missing required field 'refundTo' in transaction")
		}
		if dec.RetryValue == nil {
			return errors.New("missing required field 'retryValue' in transaction")
		}
		if dec.RetryData == nil {
			return errors.New("missing required field 'retryData' in transaction")
		}
		inner = &ArbitrumSubmitRetryableTx{
			ChainId:          (*big.Int)(dec.ChainID),
			RequestId:        *dec.RequestId,
			From:             *dec.From,
			L1BaseFee:        (*big.Int)(dec.L1BaseFee),
			DepositValue:     (*big.Int)(dec.DepositValue),
			GasFeeCap:        (*big.Int)(dec.MaxFeePerGas),
			Gas:              uint64(*dec.Gas),
			RetryTo:          dec.RetryTo,
			RetryValue:       (*big.Int)(dec.RetryValue),
			Beneficiary:      *dec.Beneficiary,
			MaxSubmissionFee: (*big.Int)(dec.MaxSubmissionFee),
			FeeRefundAddr:    *dec.RefundTo,
			RetryData:        *dec.RetryData,
		}

	case BlobTxType:
		var itx BlobTx
		inner = &itx
		if dec.ChainID == nil {
			return errors.New("missing required field 'chainId' in transaction")
		}
		itx.ChainID = uint256.MustFromBig((*big.Int)(dec.ChainID))

		if dec.Nonce == nil {
			return errors.New("missing required field 'nonce' in transaction")
		}
		itx.Nonce = uint64(*dec.Nonce)
		if dec.To == nil {
			return errors.New("missing required field 'to' in transaction")
		}
		itx.To = *dec.To
		if dec.Gas == nil {
			return errors.New("missing required field 'gas' for txdata")
		}
		itx.Gas = uint64(*dec.Gas)
		if dec.MaxPriorityFeePerGas == nil {
			return errors.New("missing required field 'maxPriorityFeePerGas' for txdata")
		}
		itx.GasTipCap = uint256.MustFromBig((*big.Int)(dec.MaxPriorityFeePerGas))
		if dec.MaxFeePerGas == nil {
			return errors.New("missing required field 'maxFeePerGas' for txdata")
		}
		itx.GasFeeCap = uint256.MustFromBig((*big.Int)(dec.MaxFeePerGas))
		if dec.MaxFeePerBlobGas == nil {
			return errors.New("missing required field 'maxFeePerBlobGas' for txdata")
		}
		itx.BlobFeeCap = uint256.MustFromBig((*big.Int)(dec.MaxFeePerBlobGas))
		if dec.Value == nil {
			return errors.New("missing required field 'value' in transaction")
		}
		itx.Value = uint256.MustFromBig((*big.Int)(dec.Value))
		if dec.Input == nil {
			return errors.New("missing required field 'input' in transaction")
		}
		itx.Data = *dec.Input
		if dec.AccessList != nil {
			itx.AccessList = *dec.AccessList
		}
		if dec.BlobVersionedHashes == nil {
			return errors.New("missing required field 'blobVersionedHashes' in transaction")
		}
		itx.BlobHashes = dec.BlobVersionedHashes

		// signature R
		var overflow bool
		if dec.R == nil {
			return errors.New("missing required field 'r' in transaction")
		}
		itx.R, overflow = uint256.FromBig((*big.Int)(dec.R))
		if overflow {
			return errors.New("'r' value overflows uint256")
		}
		// signature S
		if dec.S == nil {
			return errors.New("missing required field 's' in transaction")
		}
		itx.S, overflow = uint256.FromBig((*big.Int)(dec.S))
		if overflow {
			return errors.New("'s' value overflows uint256")
		}
		// signature V
		vbig, err := dec.yParityValue()
		if err != nil {
			return err
		}
		itx.V, overflow = uint256.FromBig(vbig)
		if overflow {
			return errors.New("'v' value overflows uint256")
		}
		if itx.V.Sign() != 0 || itx.R.Sign() != 0 || itx.S.Sign() != 0 {
			if err := sanityCheckSignature(vbig, itx.R.ToBig(), itx.S.ToBig(), false); err != nil {
				return err
			}
		}

	default:
		return ErrTxTypeNotSupported
	}

	// Now set the inner transaction.
	tx.setDecoded(inner, 0)

	// TODO: check hash here?
	return nil
}

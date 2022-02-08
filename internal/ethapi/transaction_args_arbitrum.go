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

package ethapi

import (
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// Assumes tx is an unsigned, Arbitrum-specific transaction. Use on other types is unsafe and erroneous
func TransactArgsFromArbitrumTx(tx *types.Transaction) (TransactionArgs, error) {
	msg, err := tx.AsMessage(types.NewArbitrumSigner(nil), tx.GasPrice())
	if err != nil {
		return TransactionArgs{}, err
	}
	from := msg.From()
	gas := hexutil.Uint64(msg.Gas())
	nonce := hexutil.Uint64(msg.Nonce())
	input := hexutil.Bytes(tx.Data())
	accessList := tx.AccessList()
	return TransactionArgs{
		From:                 &from,
		To:                   msg.To(),
		Gas:                  &gas,
		MaxFeePerGas:         (*hexutil.Big)(msg.GasFeeCap()),
		MaxPriorityFeePerGas: (*hexutil.Big)(msg.GasTipCap()),
		Value:                (*hexutil.Big)(msg.Value()),
		Nonce:                &nonce,
		Input:                &input,
		AccessList:           &accessList,
		ChainID:              (*hexutil.Big)(tx.ChainId()),
	}, nil
}

// Copyright 2023 The go-ethereum Authors
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

package catalyst

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

type UnlimitedCapChannel[T any] struct {
	SendSide    chan T
	ReceiveSide chan T
}

func NewUnlimitedCapChannel[T any](size int) *UnlimitedCapChannel[T] {
	if size == 0 {
		panic("trying to create UnlimitedCapChannel with 0 initial size, need size to be atleast 1")
	}
	u := &UnlimitedCapChannel[T]{
		SendSide:    make(chan T),
		ReceiveSide: make(chan T, size),
	}
	go func(uc *UnlimitedCapChannel[T]) {
		for {
			if len(uc.ReceiveSide) == cap(u.ReceiveSide) {
				tmp := make(chan T, cap(u.ReceiveSide)*2)
				for len(u.ReceiveSide) > 0 {
					tmp <- <-u.ReceiveSide
				}
				u.ReceiveSide = tmp
			}
			u.ReceiveSide <- <-uc.SendSide
		}
	}(u)
	return u
}

type api struct {
	sim *SimulatedBeacon
}

func (a *api) loop() {
	// Arbitrum: we make a channel with dynamic capacity (UnlimitedCapChannel[core.NewTxsEvent]) and subscribe to tx-events on the SendSide
	// and read events on the ReceiveSide because by the current design of simulated beacon
	// it would deadlock with this cycle a.sim.Commit() -> txpool.Sync() -> subpools reset -> update feeds (newTxs is one of the receivers)
	uc := NewUnlimitedCapChannel[core.NewTxsEvent](5)
	sub := a.sim.eth.TxPool().SubscribeTransactions(uc.SendSide, true)
	defer sub.Unsubscribe()

	for {
		select {
		case <-a.sim.shutdownCh:
			return
		case w := <-a.sim.withdrawals.pending:
			withdrawals := append(a.sim.withdrawals.gatherPending(9), w)
			if err := a.sim.sealBlock(withdrawals, uint64(time.Now().Unix())); err != nil {
				log.Warn("Error performing sealing work", "err", err)
			}
		case <-uc.ReceiveSide:
			a.sim.Commit()
		}
	}
}

func (a *api) AddWithdrawal(ctx context.Context, withdrawal *types.Withdrawal) error {
	return a.sim.withdrawals.add(withdrawal)
}

func (a *api) SetFeeRecipient(ctx context.Context, feeRecipient common.Address) {
	a.sim.setFeeRecipient(feeRecipient)
}

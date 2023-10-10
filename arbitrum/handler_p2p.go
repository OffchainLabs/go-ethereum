// Copyright 2020 The go-ethereum Authors
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

package arbitrum

import (
	"fmt"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/eth/protocols/snap"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

type protocolHandler struct {
	chain      *core.BlockChain
	eventMux   *event.TypeMux
	downloader *downloader.Downloader
	db         ethdb.Database

	done atomic.Bool
}

func NewProtocolHandler(db ethdb.Database, bc *core.BlockChain) *protocolHandler {
	evMux := new(event.TypeMux)
	p := &protocolHandler{
		chain:    bc,
		eventMux: evMux,
		db:       db,
	}
	peerDrop := func(id string) {
		log.Info("dropping peer", "id", id)
	}
	success := func() {
		p.done.Store(true)
		log.Info("DOWNLOADER DONE")
	}
	p.downloader = downloader.New(db, evMux, bc, nil, peerDrop, success)
	return p
}

func (h *protocolHandler) MakeProtocols(dnsdisc enode.Iterator) []p2p.Protocol {
	protos := eth.MakeProtocols((*ethHandler)(h), h.chain.Config().ChainID.Uint64(), dnsdisc)
	protos = append(protos, snap.MakeProtocols((*snapHandler)(h), dnsdisc)...)
	return protos
}

// ethHandler implements the eth.Backend interface to handle the various network
// packets that are sent as replies or broadcasts.
type ethHandler protocolHandler

func (h *ethHandler) Chain() *core.BlockChain { return h.chain }

type dummyTxPool struct{}

func (d *dummyTxPool) Get(hash common.Hash) *types.Transaction {
	return nil
}

func (h *ethHandler) TxPool() eth.TxPool { return &dummyTxPool{} }

// RunPeer is invoked when a peer joins on the `eth` protocol.
func (h *ethHandler) RunPeer(peer *eth.Peer, hand eth.Handler) error {
	if err := h.downloader.RegisterPeer(peer.ID(), peer.Version(), peer); err != nil {
		peer.Log().Error("Failed to register peer in eth syncer", "err", err)
		return err
	}
	log.Info("eth peer")
	return hand(peer)
}

// PeerInfo retrieves all known `eth` information about a peer.
func (h *ethHandler) PeerInfo(id enode.ID) interface{} {
	return nil
}

// AcceptTxs retrieves whether transaction processing is enabled on the node
// or if inbound transactions should simply be dropped.
func (h *ethHandler) AcceptTxs() bool {
	return false
}

// Handle is invoked from a peer's message handler when it receives a new remote
// message that the handler couldn't consume and serve itself.
func (h *ethHandler) Handle(peer *eth.Peer, packet eth.Packet) error {
	// Consume any broadcasts and announces, forwarding the rest to the downloader
	switch packet := packet.(type) {
	case *eth.NewBlockHashesPacket:
		return fmt.Errorf("unexpected eth packet type for nitro: %T", packet)

	case *eth.NewBlockPacket:
		return fmt.Errorf("unexpected eth packet type for nitro: %T", packet)

	case *eth.NewPooledTransactionHashesPacket66:
		return fmt.Errorf("unexpected eth packet type for nitro: %T", packet)

	case *eth.NewPooledTransactionHashesPacket68:
		return fmt.Errorf("unexpected eth packet type for nitro: %T", packet)

	case *eth.TransactionsPacket:
		return fmt.Errorf("unexpected eth packet type for nitro: %T", packet)

	case *eth.PooledTransactionsPacket:
		return fmt.Errorf("unexpected eth packet type for nitro: %T", packet)
	default:
		return fmt.Errorf("unexpected eth packet type for nitro: %T", packet)
	}
}

type snapHandler protocolHandler

func (h *snapHandler) ContractCodeWithPrefix(codeHash common.Hash) ([]byte, error) {
	return h.chain.ContractCodeWithPrefix(codeHash)
}

func (h *snapHandler) TrieDB() *trie.Database {
	return h.chain.StateCache().TrieDB()
}

func (h *snapHandler) Snapshot(root common.Hash) snapshot.Snapshot {
	return nil
}

type trieIteratorWrapper struct {
	val *trie.Iterator
}

func (i trieIteratorWrapper) Next() bool        { return i.val.Next() }
func (i trieIteratorWrapper) Error() error      { return i.val.Err }
func (i trieIteratorWrapper) Hash() common.Hash { return common.BytesToHash(i.val.Key) }
func (i trieIteratorWrapper) Release()          {}

type trieAccountIterator struct {
	trieIteratorWrapper
}

func (i trieAccountIterator) Account() []byte { return i.val.Value }

func (h *snapHandler) AccountIterator(root, account common.Hash) (snapshot.AccountIterator, error) {
	triedb := trie.NewDatabase(h.db)
	t, err := trie.NewStateTrie(trie.StateTrieID(root), triedb)
	if err != nil {
		log.Error("Failed to open trie", "root", root, "err", err)
		return nil, err
	}
	accIter := t.NodeIterator(account[:])
	return trieAccountIterator{trieIteratorWrapper{trie.NewIterator((accIter))}}, nil
}

type trieStoreageIterator struct {
	trieIteratorWrapper
}

func (i trieStoreageIterator) Slot() []byte { return i.val.Value }

type nilStoreageIterator struct{}

func (i nilStoreageIterator) Next() bool        { return false }
func (i nilStoreageIterator) Error() error      { return nil }
func (i nilStoreageIterator) Hash() common.Hash { return types.EmptyRootHash }
func (i nilStoreageIterator) Release()          {}
func (i nilStoreageIterator) Slot() []byte      { return nil }

func (h *snapHandler) StorageIterator(root, account, origin common.Hash) (snapshot.StorageIterator, error) {
	triedb := trie.NewDatabase(h.db)
	t, err := trie.NewStateTrie(trie.StateTrieID(root), triedb)
	if err != nil {
		log.Error("Failed to open trie", "root", root, "err", err)
		return nil, err
	}
	val, _, err := t.GetNode(account[:])
	if err != nil {
		log.Error("Failed to find account in trie", "root", root, "account", account, "err", err)
		return nil, err
	}
	var acc types.StateAccount
	if err := rlp.DecodeBytes(val, &acc); err != nil {
		log.Error("Invalid account encountered during traversal", "err", err)
		return nil, err
	}
	if acc.Root == types.EmptyRootHash {
		return nilStoreageIterator{}, nil
	}
	id := trie.StorageTrieID(root, account, acc.Root)
	storageTrie, err := trie.NewStateTrie(id, triedb)
	if err != nil {
		log.Error("Failed to open storage trie", "root", acc.Root, "err", err)
		return nil, err
	}
	return trieStoreageIterator{trieIteratorWrapper{trie.NewIterator(storageTrie.NodeIterator(origin[:]))}}, nil
}

// RunPeer is invoked when a peer joins on the `snap` protocol.
func (h *snapHandler) RunPeer(peer *snap.Peer, hand snap.Handler) error {
	if err := h.downloader.SnapSyncer.Register(peer); err != nil {
		peer.Log().Error("Failed to register peer in snap syncer", "err", err)
		return err
	}
	log.Info("snap peer")
	return hand(peer)
}

// PeerInfo retrieves all known `snap` information about a peer.
func (h *snapHandler) PeerInfo(id enode.ID) interface{} {
	return nil
}

// Handle is invoked from a peer's message handler when it receives a new remote
// message that the handler couldn't consume and serve itself.
func (h *snapHandler) Handle(peer *snap.Peer, packet snap.Packet) error {
	return h.downloader.DeliverSnapPacket(peer, packet)
}

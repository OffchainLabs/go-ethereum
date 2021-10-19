package arbitrum

import (
	"math/big"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/node"
)

type ArbBackend struct {
	arbos       ArbosWrapper
	blockChain  *core.BlockChain
	stack       *node.Node
	chainId     *big.Int
	apiBackend  *ArbAPIBackend
	ethConfig   *ethconfig.Config
	ethDatabase ethdb.Database

	chanTxs      chan *types.Transaction
	chanClose    chan struct{} //close coroutine
	chanNewBlock chan struct{} //create new L2 block unless empty
}

func NewBackend(stack *node.Node, config *ethconfig.Config, ethDatabase ethdb.Database, blockChain *core.BlockChain, chainId *big.Int, arbos ArbosWrapper) (*ArbBackend, error) {
	backend := &ArbBackend{
		arbos:        arbos,
		blockChain:   blockChain,
		stack:        stack,
		chainId:      chainId,
		ethConfig:    config,
		ethDatabase:  ethDatabase,
		chanTxs:      make(chan *types.Transaction, 100),
		chanClose:    make(chan struct{}, 1),
		chanNewBlock: make(chan struct{}, 1),
	}
	stack.RegisterLifecycle(backend)
	go backend.segmentQueueRutine()

	createRegisterAPIBackend(backend)
	return backend, nil
}

func (b *ArbBackend) APIBackend() *ArbAPIBackend {
	return b.apiBackend
}

func (b *ArbBackend) EnqueueL2Message(tx *types.Transaction) error {
	b.chanTxs <- tx
	return nil
}

func (b *ArbBackend) CloseBlock() {
	b.chanNewBlock <- struct{}{}
}

func (b *ArbBackend) enqueueBlock(block *types.Block, reciepts types.Receipts, state *state.StateDB) {
	if block == nil {
		return
	}
	logs := make([]*types.Log, 0)
	for _, receipt := range reciepts {
		logs = append(logs, receipt.Logs...)
	}
	b.blockChain.WriteBlockWithState(block, reciepts, logs, state, true)
}

func (b *ArbBackend) segmentQueueRutine() {
	for {
		select {
		case tx := <-b.chanTxs:
			b.arbos.EnqueueSequencerTx(tx)
		case <-b.chanNewBlock:
			b.enqueueBlock(b.arbos.BuildBlock(true))
		case <-b.chanClose:
			return
		}
	}
}

//TODO: this is used when registering backend as lifecycle in stack
func (b *ArbBackend) Start() error {
	return nil
}

func (b *ArbBackend) Stop() error {

	b.blockChain.Stop()

	return nil
}

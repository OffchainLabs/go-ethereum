package arbitrum

import (
	"math/big"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/node"
)

type Backend struct {
	publisher   TransactionPublisher
	blockChain  *core.BlockChain
	stack       *node.Node
	chainId     *big.Int
	apiBackend  *APIBackend
	ethConfig   *ethconfig.Config
	ethDatabase ethdb.Database

	txFeed event.Feed
	scope  event.SubscriptionScope

	chanTxs      chan *types.Transaction
	chanClose    chan struct{} //close coroutine
	chanNewBlock chan struct{} //create new L2 block unless empty
}

func NewBackend(stack *node.Node, config *ethconfig.Config, ethDatabase ethdb.Database, blockChain *core.BlockChain, chainId *big.Int, publisher TransactionPublisher) (*Backend, error) {
	backend := &Backend{
		publisher:    publisher,
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

	createRegisterAPIBackend(backend)
	return backend, nil
}

func (b *Backend) APIBackend() *APIBackend {
	return b.apiBackend
}

func (b *Backend) EnqueueL2Message(tx *types.Transaction) error {
	return b.publisher.PublishTransaction(tx)
}

func (b *Backend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.scope.Track(b.txFeed.Subscribe(ch))
}

func (b *Backend) enqueueBlock(block *types.Block, reciepts types.Receipts, state *state.StateDB) {
	if block == nil {
		return
	}
	logs := make([]*types.Log, 0)
	for _, receipt := range reciepts {
		logs = append(logs, receipt.Logs...)
	}
	b.blockChain.WriteBlockWithState(block, reciepts, logs, state, true)
}

//TODO: this is used when registering backend as lifecycle in stack
func (b *Backend) Start() error {
	return nil
}

func (b *Backend) Stop() error {

	b.scope.Close()
	b.blockChain.Stop()

	return nil
}

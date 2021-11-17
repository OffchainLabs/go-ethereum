package arbitrum

import (
	"context"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/node"
)

type Backend struct {
	arb        ArbInterface
	stack      *node.Node
	apiBackend *APIBackend
	config     *Config
	chainDb    ethdb.Database

	txFeed event.Feed
	scope  event.SubscriptionScope

	chanTxs      chan *types.Transaction
	chanClose    chan struct{} //close coroutine
	chanNewBlock chan struct{} //create new L2 block unless empty
}

type Config struct {
	// RPCGasCap is the global gas cap for eth-call variants.
	RPCGasCap uint64

	// RPCTxFeeCap is the global transaction fee(price * gaslimit) cap for
	// send-transction variants. The unit is ether.
	RPCTxFeeCap float64
}

var DefaultConfig = Config{
	RPCGasCap:   50000000,
	RPCTxFeeCap: 1, // 1 ether
}

func NewBackend(stack *node.Node, config *Config, chainDb ethdb.Database, blockChain *core.BlockChain, publisher ArbInterface) (*Backend, error) {
	backend := &Backend{
		arb:          publisher,
		stack:        stack,
		config:       config,
		chainDb:      chainDb,
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

func (b *Backend) ChainDb() ethdb.Database {
	return b.chainDb
}

func (b *Backend) EnqueueL2Message(ctx context.Context, tx *types.Transaction) error {
	return b.arb.PublishTransaction(ctx, tx)
}

func (b *Backend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.scope.Track(b.txFeed.Subscribe(ch))
}

func (b *Backend) Stack() *node.Node {
	return b.stack
}

func (b *Backend) ArbInterface() ArbInterface {
	return b.arb
}

//TODO: this is used when registering backend as lifecycle in stack
func (b *Backend) Start() error {
	return nil
}

func (b *Backend) Stop() error {

	b.scope.Close()

	return nil
}

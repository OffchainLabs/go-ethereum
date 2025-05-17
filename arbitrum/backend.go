package arbitrum

import (
	"context"

	"github.com/curtis0505/arbitrum/core"
	"github.com/curtis0505/arbitrum/core/bloombits"
	"github.com/curtis0505/arbitrum/core/types"
	"github.com/curtis0505/arbitrum/ethdb"
	"github.com/curtis0505/arbitrum/event"
	"github.com/curtis0505/arbitrum/internal/shutdowncheck"
	"github.com/curtis0505/arbitrum/node"
)

type Backend struct {
	arb        ArbInterface
	stack      *node.Node
	apiBackend *APIBackend
	config     *Config
	chainDb    ethdb.Database

	txFeed event.Feed
	scope  event.SubscriptionScope

	bloomRequests chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer  *core.ChainIndexer             // Bloom indexer operating during block imports

	shutdownTracker *shutdowncheck.ShutdownTracker

	chanTxs      chan *types.Transaction
	chanClose    chan struct{} //close coroutine
	chanNewBlock chan struct{} //create new L2 block unless empty
}

func NewBackend(stack *node.Node, config *Config, chainDb ethdb.Database, publisher ArbInterface, sync SyncProgressBackend) (*Backend, error) {
	backend := &Backend{
		arb:     publisher,
		stack:   stack,
		config:  config,
		chainDb: chainDb,

		bloomRequests: make(chan chan *bloombits.Retrieval),
		bloomIndexer:  core.NewBloomIndexer(chainDb, config.BloomBitsBlocks, config.BloomConfirms),

		shutdownTracker: shutdowncheck.NewShutdownTracker(chainDb),

		chanTxs:      make(chan *types.Transaction, 100),
		chanClose:    make(chan struct{}),
		chanNewBlock: make(chan struct{}, 1),
	}

	backend.bloomIndexer.Start(backend.arb.BlockChain())
	err := createRegisterAPIBackend(backend, sync, config.ClassicRedirect, config.ClassicRedirectTimeout)
	if err != nil {
		return nil, err
	}
	backend.shutdownTracker.MarkStartup()
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

// TODO: this is used when registering backend as lifecycle in stack
func (b *Backend) Start() error {
	b.startBloomHandlers(b.config.BloomBitsBlocks)
	b.shutdownTracker.Start()

	return nil
}

func (b *Backend) Stop() error {
	b.scope.Close()
	b.bloomIndexer.Close()
	b.shutdownTracker.Stop()
	b.chainDb.Close()
	close(b.chanClose)
	return nil
}

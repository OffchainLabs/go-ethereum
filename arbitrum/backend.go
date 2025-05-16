package arbitrum

import (
	"context"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/filtermaps"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/internal/shutdowncheck"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
)

type Backend struct {
	arb        ArbInterface
	stack      *node.Node
	apiBackend *APIBackend
	config     *Config
	chainDb    ethdb.Database

	txFeed event.Feed
	scope  event.SubscriptionScope

	filterMaps *filtermaps.FilterMaps

	shutdownTracker *shutdowncheck.ShutdownTracker

	chanTxs      chan *types.Transaction
	chanClose    chan struct{} //close coroutine
	chanNewBlock chan struct{} //create new L2 block unless empty

	filterSystem *filters.FilterSystem
}

func NewBackend(stack *node.Node, config *Config, chainDb ethdb.Database, publisher ArbInterface, filterConfig filters.Config) (*Backend, *filters.FilterSystem, error) {
	backend := &Backend{
		arb:     publisher,
		stack:   stack,
		config:  config,
		chainDb: chainDb,

		shutdownTracker: shutdowncheck.NewShutdownTracker(chainDb),

		chanTxs:      make(chan *types.Transaction, 100),
		chanClose:    make(chan struct{}),
		chanNewBlock: make(chan struct{}, 1),
	}

	scheme, err := rawdb.ParseStateScheme(config.StateScheme, chainDb)
	if err != nil {
		return nil, nil, err
	}
	// Initialize filtermaps log index.
	fmConfig := filtermaps.Config{
		History:        config.LogHistory,
		Disabled:       config.LogNoHistory,
		ExportFileName: config.LogExportCheckpoints,
		HashScheme:     scheme == rawdb.HashScheme,
	}
	chainView := backend.newChainView(backend.arb.BlockChain().CurrentBlock())
	historyCutoff, _ := backend.arb.BlockChain().HistoryPruningCutoff()
	var finalBlock uint64
	if fb := backend.arb.BlockChain().CurrentFinalBlock(); fb != nil {
		finalBlock = fb.Number.Uint64()
	}
	backend.filterMaps = filtermaps.NewFilterMaps(chainDb, chainView, historyCutoff, finalBlock, filtermaps.DefaultParams, fmConfig)
	if len(config.AllowMethod) > 0 {
		rpcFilter := make(map[string]bool)
		for _, method := range config.AllowMethod {
			rpcFilter[method] = true
		}
		backend.stack.ApplyAPIFilter(rpcFilter)
	}

	filterSystem, err := createRegisterAPIBackend(backend, filterConfig, config.ClassicRedirect, config.ClassicRedirectTimeout)
	if err != nil {
		return nil, nil, err
	}
	backend.filterSystem = filterSystem
	return backend, filterSystem, nil
}
func (b *Backend) newChainView(head *types.Header) *filtermaps.ChainView {
	if head == nil {
		return nil
	}
	return filtermaps.NewChainView(b.arb.BlockChain(), head.Number.Uint64(), head.Hash())
}

func (b *Backend) AccountManager() *accounts.Manager { return b.stack.AccountManager() }
func (b *Backend) APIBackend() *APIBackend           { return b.apiBackend }
func (b *Backend) APIs() []rpc.API                   { return b.apiBackend.GetAPIs(b.filterSystem) }
func (b *Backend) ArbInterface() ArbInterface        { return b.arb }
func (b *Backend) BlockChain() *core.BlockChain      { return b.arb.BlockChain() }
func (b *Backend) ChainDb() ethdb.Database           { return b.chainDb }
func (b *Backend) Engine() consensus.Engine          { return b.arb.BlockChain().Engine() }
func (b *Backend) Stack() *node.Node                 { return b.stack }

func (b *Backend) ResetWithGenesisBlock(gb *types.Block) {
	b.arb.BlockChain().ResetWithGenesisBlock(gb)
}

func (b *Backend) EnqueueL2Message(ctx context.Context, tx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	return b.arb.PublishTransaction(ctx, tx, options)
}

func (b *Backend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.scope.Track(b.txFeed.Subscribe(ch))
}

// TODO: this is used when registering backend as lifecycle in stack
func (b *Backend) Start() error {
	b.filterMaps.Start()
	b.shutdownTracker.MarkStartup()
	b.shutdownTracker.Start()
	go b.updateFilterMapsHeads()
	return nil
}

func (b *Backend) updateFilterMapsHeads() {
	headEventCh := make(chan core.ChainEvent, 10)
	blockProcCh := make(chan bool, 10)
	sub := b.arb.BlockChain().SubscribeChainEvent(headEventCh)
	sub2 := b.arb.BlockChain().SubscribeBlockProcessingEvent(blockProcCh)
	defer func() {
		sub.Unsubscribe()
		sub2.Unsubscribe()
		for {
			select {
			case <-headEventCh:
			case <-blockProcCh:
			default:
				return
			}
		}
	}()

	var head *types.Header
	setHead := func(newHead *types.Header) {
		if newHead == nil {
			return
		}
		if head == nil || newHead.Hash() != head.Hash() {
			head = newHead
			chainView := b.newChainView(head)
			historyCutoff, _ := b.arb.BlockChain().HistoryPruningCutoff()
			var finalBlock uint64
			if fb := b.arb.BlockChain().CurrentFinalBlock(); fb != nil {
				finalBlock = fb.Number.Uint64()
			}
			b.filterMaps.SetTarget(chainView, historyCutoff, finalBlock)
		}
	}
	setHead(b.arb.BlockChain().CurrentBlock())

	for {
		select {
		case ev := <-headEventCh:
			setHead(ev.Header)
		case blockProc := <-blockProcCh:
			b.filterMaps.SetBlockProcessing(blockProc)
		case <-time.After(time.Second * 10):
			setHead(b.arb.BlockChain().CurrentBlock())
		case _, more := <-b.chanClose:
			if !more {
				return
			}
		}
	}
}

func (b *Backend) Stop() error {
	b.scope.Close()
	b.filterMaps.Stop()
	b.shutdownTracker.Stop()
	b.chainDb.Close()
	close(b.chanClose)
	return nil
}

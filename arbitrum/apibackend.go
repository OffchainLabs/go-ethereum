package arbitrum

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/filtermaps"
	"github.com/ethereum/go-ethereum/core/history"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/triedb"
)

var (
	liveStatesReferencedCounter        = metrics.NewRegisteredCounter("arb/apibackend/states/live/referenced", nil)
	liveStatesDereferencedCounter      = metrics.NewRegisteredCounter("arb/apibackend/states/live/dereferenced", nil)
	recreatedStatesReferencedCounter   = metrics.NewRegisteredCounter("arb/apibackend/states/recreated/referenced", nil)
	recreatedStatesDereferencedCounter = metrics.NewRegisteredCounter("arb/apibackend/states/recreated/dereferenced", nil)
)

type APIBackend struct {
	b *Backend

	fallbackClient        types.FallbackClient
	archiveClientsManager *archiveFallbackClientsManager
	sync                  SyncProgressBackend
}

type errorFilteredFallbackClient struct {
	impl types.FallbackClient
	url  string
}

func (c *errorFilteredFallbackClient) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	err := c.impl.CallContext(ctx, result, method, args...)
	if err != nil && strings.Contains(err.Error(), c.url) {
		// avoids leaking the URL in the error message.
		// URL can contain sensitive information such as API keys.
		log.Warn("fallback client error", "error", err)
		return errors.New("Failed to call fallback API")
	}
	return err
}

type timeoutFallbackClient struct {
	impl    types.FallbackClient
	timeout time.Duration
}

func (c *timeoutFallbackClient) CallContext(ctxIn context.Context, result interface{}, method string, args ...interface{}) error {
	ctx, cancel := context.WithTimeout(ctxIn, c.timeout)
	defer cancel()
	return c.impl.CallContext(ctx, result, method, args...)
}

func CreateFallbackClient(fallbackClientUrl string, fallbackClientTimeout time.Duration, isArchiveNode bool) (types.FallbackClient, error) {
	if fallbackClientUrl == "" {
		return nil, nil
	}
	if !isArchiveNode && strings.HasPrefix(fallbackClientUrl, "error:") {
		fields := strings.Split(fallbackClientUrl, ":")[1:]
		errNumber, convErr := strconv.ParseInt(fields[0], 0, 0)
		if convErr == nil {
			fields = fields[1:]
		} else {
			errNumber = -32000
		}
		types.SetFallbackError(strings.Join(fields, ":"), int(errNumber))
		return nil, nil
	}

	var fallbackClient types.FallbackClient
	var err error
	fallbackClient, err = rpc.Dial(fallbackClientUrl)
	if err != nil {
		return nil, fmt.Errorf("failed creating fallback connection: %w", err)
	}
	url, err := url.Parse(fallbackClientUrl)
	if err != nil {
		return nil, fmt.Errorf("failed parsing fallback URL: %w", err)
	}
	fallbackClient = &errorFilteredFallbackClient{
		impl: fallbackClient,
		url:  url.String(),
	}

	if fallbackClientTimeout != 0 {
		fallbackClient = &timeoutFallbackClient{
			impl:    fallbackClient,
			timeout: fallbackClientTimeout,
		}
	}
	return fallbackClient, nil
}

type SyncProgressBackend interface {
	SyncProgressMap(ctx context.Context) map[string]interface{}
	BlockMetadataByNumber(ctx context.Context, blockNum uint64) (common.BlockMetadata, error)
}

func createRegisterAPIBackend(backend *Backend, filterConfig filters.Config, fallbackClientUrl string, fallbackClientTimeout time.Duration, archiveRedirects []BlockRedirectConfig) (*filters.FilterSystem, error) {
	fallbackClient, err := CreateFallbackClient(fallbackClientUrl, fallbackClientTimeout, false)
	if err != nil {
		return nil, err
	}
	var archiveClientsManager *archiveFallbackClientsManager
	if len(archiveRedirects) != 0 {
		archiveClientsManager, err = newArchiveFallbackClientsManager(archiveRedirects)
		if err != nil {
			return nil, err
		}
	}
	backend.apiBackend = &APIBackend{
		b:                     backend,
		fallbackClient:        fallbackClient,
		archiveClientsManager: archiveClientsManager,
	}
	filterSystem := filters.NewFilterSystem(backend.apiBackend, filterConfig)
	backend.stack.RegisterAPIs(backend.apiBackend.GetAPIs(filterSystem))
	return filterSystem, nil
}

func (a *APIBackend) SetSyncBackend(sync SyncProgressBackend) error {
	if a.sync != nil {
		return errors.New("sync progress monitor already set")
	}
	a.sync = sync
	return nil
}

func (a *APIBackend) GetAPIs(filterSystem *filters.FilterSystem) []rpc.API {
	apis := ethapi.GetAPIs(a)

	apis = append(apis, rpc.API{
		Namespace: "eth",
		Version:   "1.0",
		Service:   filters.NewFilterAPI(filterSystem),
		Public:    true,
	})

	apis = append(apis, rpc.API{
		Namespace: "eth",
		Version:   "1.0",
		Service:   NewArbTransactionAPI(a),
		Public:    true,
	})

	apis = append(apis, rpc.API{
		Namespace: "net",
		Version:   "1.0",
		Service:   NewPublicNetAPI(a.ChainConfig().ChainID.Uint64()),
		Public:    true,
	})

	apis = append(apis, rpc.API{
		Namespace: "txpool",
		Version:   "1.0",
		Service:   NewPublicTxPoolAPI(),
		Public:    true,
	})

	apis = append(apis, tracers.APIs(a)...)

	return apis
}

func (a *APIBackend) BlockChain() *core.BlockChain {
	return a.b.BlockChain()
}

func (a *APIBackend) GetArbitrumNode() interface{} {
	return a.b.arb.ArbNode()
}

func (a *APIBackend) GetBody(ctx context.Context, hash common.Hash, number rpc.BlockNumber) (*types.Body, error) {
	body := a.BlockChain().GetBody(hash)
	if body == nil {
		if uint64(number) < a.HistoryPruningCutoff() {
			return nil, &history.PrunedHistoryError{}
		}
		return nil, errors.New("block body not found")
	}
	return body, nil
}

// General Ethereum API
func (a *APIBackend) SyncProgressMap(ctx context.Context) map[string]interface{} {
	if a.sync == nil {
		res := make(map[string]interface{})
		res["error"] = "sync object not set in apibackend"
		return res
	}
	return a.sync.SyncProgressMap(ctx)
}

func (a *APIBackend) SyncProgress(ctx context.Context) ethereum.SyncProgress {
	progress := a.SyncProgressMap(context.Background())

	if len(progress) == 0 {
		return ethereum.SyncProgress{}
	}
	return ethereum.SyncProgress{
		CurrentBlock: 0,
		HighestBlock: 1,
	}
}

func (a *APIBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return big.NewInt(0), nil // there's no tips in L2
}

func (a *APIBackend) FeeHistory(
	ctx context.Context,
	blocks uint64,
	newestBlock rpc.BlockNumber,
	rewardPercentiles []float64,
) (*big.Int, [][]*big.Int, []*big.Int, []float64, []*big.Int, []float64, error) {
	// TODO: Add info about baseFeePerBlobGas, blobGasUsedRatio, just like in EthAPIBackend FeeHistory
	if core.GetArbOSSpeedLimitPerSecond == nil {
		return nil, nil, nil, nil, nil, nil, errors.New("ArbOS not installed")
	}

	nitroGenesis := rpc.BlockNumber(a.ChainConfig().ArbitrumChainParams.GenesisBlockNum)
	newestBlock, latestBlock := a.BlockChain().ClipToPostNitroGenesis(newestBlock)

	maxFeeHistory := a.b.config.FeeHistoryMaxBlockCount
	if blocks > maxFeeHistory {
		log.Warn("Sanitizing fee history length", "requested", blocks, "truncated", maxFeeHistory)
		blocks = maxFeeHistory
	}
	if blocks < 1 {
		// returning with no data and no error means there are no retrievable blocks
		return common.Big0, nil, nil, nil, nil, nil, nil
	}

	// don't attempt to include blocks before genesis
	if rpc.BlockNumber(blocks) > (newestBlock - nitroGenesis) {
		blocks = uint64(newestBlock - nitroGenesis + 1)
	}
	oldestBlock := uint64(newestBlock) + 1 - blocks

	// inform that tipping has no effect on inclusion
	rewards := make([][]*big.Int, blocks)
	zeros := make([]*big.Int, len(rewardPercentiles))
	for i := range zeros {
		zeros[i] = common.Big0
	}
	for i := range rewards {
		rewards[i] = zeros
	}
	if len(rewardPercentiles) == 0 {
		rewards = nil
	}

	// use the most recent average compute rate for all blocks
	// note: while we could query this value for each block, it'd be prohibitively expensive
	state, _, err := a.StateAndHeaderByNumber(ctx, newestBlock)
	if err != nil {
		return common.Big0, nil, nil, nil, nil, nil, err
	}
	speedLimit, err := core.GetArbOSSpeedLimitPerSecond(state)
	if err != nil {
		return common.Big0, nil, nil, nil, nil, nil, err
	}

	gasUsed := make([]float64, blocks)
	basefees := make([]*big.Int, blocks+1) // the RPC semantics are to predict the future value

	// collect the basefees
	baseFeeLookup := newestBlock + 1
	if newestBlock == latestBlock {
		baseFeeLookup = newestBlock
	}
	var prevTimestamp uint64
	var timeSinceLastTimeChange uint64
	var currentTimestampGasUsed uint64
	if rpc.BlockNumber(oldestBlock) > nitroGenesis {
		header, err := a.HeaderByNumber(ctx, rpc.BlockNumber(oldestBlock-1))
		if err != nil {
			return common.Big0, nil, nil, nil, nil, nil, err
		}
		prevTimestamp = header.Time
	}
	for block := oldestBlock; block <= uint64(baseFeeLookup); block++ {
		header, err := a.HeaderByNumber(ctx, rpc.BlockNumber(block))
		if err != nil {
			return common.Big0, nil, nil, nil, nil, nil, err
		}
		basefees[block-oldestBlock] = header.BaseFee

		if block > uint64(newestBlock) {
			break
		}

		if header.Time > prevTimestamp {
			timeSinceLastTimeChange = header.Time - prevTimestamp
			currentTimestampGasUsed = 0
		}

		receipts := a.BlockChain().GetReceiptsByHash(header.Hash())
		for _, receipt := range receipts {
			if receipt.GasUsed > receipt.GasUsedForL1 {
				currentTimestampGasUsed += receipt.GasUsed - receipt.GasUsedForL1
			}
		}

		prevTimestamp = header.Time

		// In vanilla geth, this RPC returns the gasUsed ratio so a client can know how the basefee will change
		// To emulate this, we translate the compute rate into something similar, centered at an analogous 0.5
		var fullnessAnalogue float64
		if timeSinceLastTimeChange > 0 {
			fullnessAnalogue = float64(currentTimestampGasUsed) / float64(speedLimit) / float64(timeSinceLastTimeChange) / 2.0
			if fullnessAnalogue > 1.0 {
				fullnessAnalogue = 1.0
			}
		} else {
			// We haven't looked far enough back to know the last timestamp change,
			// so treat this block as full.
			fullnessAnalogue = 1.0
		}
		gasUsed[block-oldestBlock] = fullnessAnalogue
	}
	if newestBlock == latestBlock {
		basefees[blocks] = basefees[blocks-1] // guess the basefee won't change
	}

	return big.NewInt(int64(oldestBlock)), rewards, basefees, gasUsed, nil, nil, nil
}

func (a *APIBackend) BlobBaseFee(ctx context.Context) *big.Int {
	if excess := a.CurrentHeader().ExcessBlobGas; excess != nil {
		return eip4844.CalcBlobFee(a.ChainConfig(), a.CurrentHeader())
	}
	return nil
}

func (a *APIBackend) ChainDb() ethdb.Database {
	return a.b.chainDb
}

func (a *APIBackend) AccountManager() *accounts.Manager {
	return a.b.stack.AccountManager()
}

func (a *APIBackend) ExtRPCEnabled() bool {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) RPCGasCap() uint64 {
	return a.b.config.RPCGasCap
}

func (a *APIBackend) RPCTxFeeCap() float64 {
	return a.b.config.RPCTxFeeCap
}

func (a *APIBackend) CurrentView() *filtermaps.ChainView {
	head := a.BlockChain().CurrentBlock()
	if head == nil {
		return nil
	}
	return filtermaps.NewChainView(a.BlockChain(), head.Number.Uint64(), head.Hash())
}

func (a *APIBackend) RPCEVMTimeout() time.Duration {
	return a.b.config.RPCEVMTimeout
}

func (a *APIBackend) UnprotectedAllowed() bool {
	return a.b.config.TxAllowUnprotected
}

// Blockchain API
func (a *APIBackend) SetHead(number uint64) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	return a.headerByNumberImpl(ctx, number)
}

func (a *APIBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return a.BlockChain().GetHeaderByHash(hash), nil
}

func (a *APIBackend) blockNumberToUint(ctx context.Context, number rpc.BlockNumber) (uint64, error) {
	if number == rpc.LatestBlockNumber || number == rpc.PendingBlockNumber {
		return a.BlockChain().CurrentBlock().Number.Uint64(), nil
	}
	if number == rpc.SafeBlockNumber {
		if a.sync == nil {
			return 0, errors.New("block number not supported: object not set")
		}

		currentSafeBlock := a.BlockChain().CurrentSafeBlock()
		if currentSafeBlock == nil {
			return 0, errors.New("safe block not found")
		}
		return currentSafeBlock.Number.Uint64(), nil
	}
	if number == rpc.FinalizedBlockNumber {
		if a.sync == nil {
			return 0, errors.New("block number not supported: object not set")
		}

		currentFinalizedBlock := a.BlockChain().CurrentFinalBlock()
		if currentFinalizedBlock == nil {
			return 0, errors.New("finalized block not found")
		}
		return currentFinalizedBlock.Number.Uint64(), nil
	}
	if number == rpc.EarliestBlockNumber {
		return a.HistoryPruningCutoff(), nil
	}
	if number < 0 {
		return 0, errors.New("block number not supported")
	}
	return uint64(number.Int64()), nil
}

func (a *APIBackend) headerByNumberImpl(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	if number == rpc.LatestBlockNumber || number == rpc.PendingBlockNumber {
		return a.BlockChain().CurrentBlock(), nil
	}
	numUint, err := a.blockNumberToUint(ctx, number)
	if err != nil {
		return nil, err
	}
	return a.BlockChain().GetHeaderByNumber(numUint), nil
}

func (a *APIBackend) headerByNumberOrHashImpl(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	number, isnum := blockNrOrHash.Number()
	if isnum {
		return a.headerByNumberImpl(ctx, number)
	}
	hash, ishash := blockNrOrHash.Hash()
	if ishash {
		return a.BlockChain().GetHeaderByHash(hash), nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (a *APIBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	return a.headerByNumberOrHashImpl(ctx, blockNrOrHash)
}

func (a *APIBackend) CurrentHeader() *types.Header {
	return a.BlockChain().CurrentHeader()
}

func (a *APIBackend) CurrentBlock() *types.Header {
	return a.BlockChain().CurrentBlock()
}

func (a *APIBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	if number == rpc.LatestBlockNumber || number == rpc.PendingBlockNumber {
		currentHeader := a.BlockChain().CurrentBlock()
		currentBlock := a.BlockChain().GetBlock(currentHeader.Hash(), currentHeader.Number.Uint64())
		if currentBlock == nil {
			return nil, errors.New("can't find block for current header")
		}
		return currentBlock, nil
	}
	numUint, err := a.blockNumberToUint(ctx, number)
	if err != nil {
		return nil, err
	}
	block := a.BlockChain().GetBlockByNumber(numUint)
	if block == nil && numUint < a.HistoryPruningCutoff() {
		return nil, &history.PrunedHistoryError{}
	}
	return block, nil
}

func (a *APIBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	number := a.BlockChain().GetBlockNumber(hash)
	if number == nil {
		return nil, nil
	}
	block := a.BlockChain().GetBlock(hash, *number)
	if block == nil && *number < a.HistoryPruningCutoff() {
		return nil, &history.PrunedHistoryError{}
	}
	return block, nil
}

func (a *APIBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	number, isnum := blockNrOrHash.Number()
	if isnum {
		return a.BlockByNumber(ctx, number)
	}
	hash, ishash := blockNrOrHash.Hash()
	if ishash {
		return a.BlockByHash(ctx, hash)
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (a *APIBackend) BlockMetadataByNumber(ctx context.Context, blockNum uint64) (common.BlockMetadata, error) {
	return a.sync.BlockMetadataByNumber(ctx, blockNum)
}

func (a *APIBackend) NewMatcherBackend() filtermaps.MatcherBackend {
	return a.b.filterMaps.NewMatcherBackend()
}

func StateAndHeaderFromHeader(ctx context.Context, chainDb ethdb.Database, bc *core.BlockChain, maxRecreateStateDepth int64, header *types.Header, err error, archiveClientsManager *archiveFallbackClientsManager) (*state.StateDB, *types.Header, error) {
	if err != nil {
		return nil, header, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	if !bc.Config().IsArbitrumNitro(header.Number) {
		return nil, header, types.ErrUseFallback
	}
	if archiveClientsManager != nil && header.Number.Uint64() <= archiveClientsManager.lastAvailableBlock() {
		return nil, header, &types.ErrUseArchiveFallback{BlockNum: header.Number.Uint64()}
	}
	stateFor := func(db state.Database, snapshots *snapshot.Tree) func(header *types.Header) (*state.StateDB, StateReleaseFunc, error) {
		return func(header *types.Header) (*state.StateDB, StateReleaseFunc, error) {
			if header.Root != (common.Hash{}) {
				// Try referencing the root, if it isn't in dirties cache then Reference will have no effect
				db.TrieDB().Reference(header.Root, common.Hash{})
			}
			statedb, err := state.New(header.Root, db)
			if err != nil {
				return nil, nil, err
			}
			if header.Root != (common.Hash{}) {
				headerRoot := header.Root
				return statedb, func() { db.TrieDB().Dereference(headerRoot) }, nil
			}
			return statedb, NoopStateRelease, nil
		}
	}
	liveState, liveStateRelease, err := stateFor(bc.StateCache(), bc.Snapshots())(header)
	if err == nil {
		liveStatesReferencedCounter.Inc(1)
		liveState.SetArbFinalizer(func(*state.ArbitrumExtraData) {
			liveStateRelease()
			liveStatesDereferencedCounter.Inc(1)
		})
		return liveState, header, nil
	}
	// else err != nil => we don't need to call liveStateRelease

	// Create an ephemeral trie.Database for isolating the live one
	// note: triedb cleans cache is disabled in trie.HashDefaults
	// note: only states committed to diskdb can be found as we're creating new triedb
	// note: snapshots are not used here
	ephemeral := state.NewDatabase(triedb.NewDatabase(chainDb, triedb.HashDefaults), nil)
	lastState, lastHeader, lastStateRelease, err := FindLastAvailableState(ctx, bc, stateFor(ephemeral, nil), header, nil, maxRecreateStateDepth)
	if err != nil {
		return nil, nil, err
	}
	// make sure that we haven't found the state in diskdb
	if lastHeader == header {
		liveStatesReferencedCounter.Inc(1)
		lastState.SetArbFinalizer(func(*state.ArbitrumExtraData) {
			lastStateRelease()
			liveStatesDereferencedCounter.Inc(1)
		})
		return lastState, header, nil
	}
	defer lastStateRelease()
	targetBlock := bc.GetBlockByNumber(header.Number.Uint64())
	if targetBlock == nil {
		return nil, nil, errors.New("target block not found")
	}
	lastBlock := bc.GetBlockByNumber(lastHeader.Number.Uint64())
	if lastBlock == nil {
		return nil, nil, errors.New("last block not found")
	}
	reexec := uint64(0)
	checkLive := false
	preferDisk := false // preferDisk is ignored in this case
	statedb, release, err := eth.NewArbEthereum(bc, chainDb).StateAtBlock(ctx, targetBlock, reexec, lastState, lastBlock, checkLive, preferDisk)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to recreate state: %w", err)
	}
	// we are setting finalizer instead of returning a StateReleaseFunc to avoid changing ethapi.Backend interface to minimize diff to upstream
	recreatedStatesReferencedCounter.Inc(1)
	statedb.SetArbFinalizer(func(*state.ArbitrumExtraData) {
		release()
		recreatedStatesDereferencedCounter.Inc(1)
	})
	return statedb, header, err
}

func (a *APIBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	header, err := a.HeaderByNumber(ctx, number)
	return StateAndHeaderFromHeader(ctx, a.ChainDb(), a.b.arb.BlockChain(), a.b.config.MaxRecreateStateDepth, header, err, a.archiveClientsManager)
}

func (a *APIBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	header, err := a.HeaderByNumberOrHash(ctx, blockNrOrHash)
	hash, ishash := blockNrOrHash.Hash()
	bc := a.BlockChain()
	// check if we are not trying to get recent state that is not yet triedb referenced or committed in Blockchain.writeBlockWithState
	if ishash && header != nil && header.Number.Cmp(bc.CurrentBlock().Number) > 0 && bc.GetCanonicalHash(header.Number.Uint64()) != hash {
		return nil, nil, errors.New("requested block ahead of current block and the hash is not currently canonical")
	}
	return StateAndHeaderFromHeader(ctx, a.ChainDb(), a.b.arb.BlockChain(), a.b.config.MaxRecreateStateDepth, header, err, a.archiveClientsManager)
}

func (a *APIBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, checkLive bool, preferDisk bool) (statedb *state.StateDB, release tracers.StateReleaseFunc, err error) {
	if !a.BlockChain().Config().IsArbitrumNitro(block.Number()) {
		return nil, nil, types.ErrUseFallback
	}
	// DEV: This assumes that `StateAtBlock` only accesses the blockchain and chainDb fields
	return eth.NewArbEthereum(a.b.arb.BlockChain(), a.ChainDb()).StateAtBlock(ctx, block, reexec, base, nil, checkLive, preferDisk)
}

func (a *APIBackend) StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (*types.Transaction, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	if !a.BlockChain().Config().IsArbitrumNitro(block.Number()) {
		return nil, vm.BlockContext{}, nil, nil, types.ErrUseFallback
	}
	// DEV: This assumes that `StateAtTransaction` only accesses the blockchain and chainDb fields
	return eth.NewArbEthereum(a.b.arb.BlockChain(), a.ChainDb()).StateAtTransaction(ctx, block, txIndex, reexec)
}

func (a *APIBackend) HistoryPruningCutoff() uint64 {
	bn, _ := a.BlockChain().HistoryPruningCutoff()
	return bn
}

func (a *APIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return a.BlockChain().GetReceiptsByHash(hash), nil
}

func (a *APIBackend) GetEVM(ctx context.Context, state *state.StateDB, header *types.Header, vmConfig *vm.Config, blockCtx *vm.BlockContext) *vm.EVM {
	if vmConfig == nil {
		vmConfig = a.BlockChain().GetVMConfig()
	}
	var context vm.BlockContext
	if blockCtx != nil {
		context = *blockCtx
	} else {
		context = core.NewEVMBlockContext(header, a.BlockChain(), nil)
	}
	return vm.NewEVM(context, state, a.BlockChain().Config(), *vmConfig)
}

func (a *APIBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return a.BlockChain().SubscribeChainEvent(ch)
}

func (a *APIBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return a.BlockChain().SubscribeChainHeadEvent(ch)
}

// Transaction pool API
func (a *APIBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return a.b.EnqueueL2Message(ctx, signedTx, nil)
}

func (a *APIBackend) SendConditionalTx(ctx context.Context, signedTx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	return a.b.EnqueueL2Message(ctx, signedTx, options)
}

func (a *APIBackend) GetTransaction(txHash common.Hash) (bool, *types.Transaction, common.Hash, uint64, uint64) {
	tx, blockHash, blockNumber, index := rawdb.ReadTransaction(a.b.chainDb, txHash)
	return tx != nil, tx, blockHash, blockNumber, index
}

func (a *APIBackend) TxIndexDone() bool {
	return a.BlockChain().TxIndexDone()
}

func (a *APIBackend) GetPoolTransactions() (types.Transactions, error) {
	// Arbitrum doesn't have a pool
	return types.Transactions{}, nil
}

func (a *APIBackend) GetPoolTransaction(txHash common.Hash) *types.Transaction {
	// Arbitrum doesn't have a pool
	return nil
}

func (a *APIBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	stateDB, err := a.BlockChain().State()
	if err != nil {
		return 0, err
	}
	return stateDB.GetNonce(addr), nil
}

func (a *APIBackend) Stats() (pending int, queued int) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) TxPoolContent() (map[common.Address][]*types.Transaction, map[common.Address][]*types.Transaction) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) TxPoolContentFrom(addr common.Address) ([]*types.Transaction, []*types.Transaction) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return a.b.SubscribeNewTxsEvent(ch)
}

// Filter API

func (a *APIBackend) GetLogs(ctx context.Context, hash common.Hash, number uint64) ([][]*types.Log, error) {
	return rawdb.ReadLogs(a.ChainDb(), hash, number), nil
}

func (a *APIBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return a.BlockChain().SubscribeLogsEvent(ch)
}

func (a *APIBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	//Arbitrum doesn't really need pending logs. Logs are published as soon as we know them..
	return a.SubscribeLogsEvent(ch)
}

func (a *APIBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return a.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (a *APIBackend) ChainConfig() *params.ChainConfig {
	return a.BlockChain().Config()
}

func (a *APIBackend) Engine() consensus.Engine {
	return a.b.Engine()
}

func (a *APIBackend) Pending() (*types.Block, types.Receipts, *state.StateDB) {
	return nil, nil, nil
}

func (a *APIBackend) FallbackClient() types.FallbackClient {
	return a.fallbackClient
}

func (a *APIBackend) ArchiveFallbackClient(blockNum uint64) types.FallbackClient {
	return a.archiveClientsManager.fallbackClient(blockNum)
}

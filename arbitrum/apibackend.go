package arbitrum

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/arbitrum_types"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/bloombits"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

type APIBackend struct {
	b *Backend

	fallbackClient types.FallbackClient
	sync           SyncProgressBackend
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

func CreateFallbackClient(fallbackClientUrl string, fallbackClientTimeout time.Duration) (types.FallbackClient, error) {
	if fallbackClientUrl == "" {
		return nil, nil
	}
	if strings.HasPrefix(fallbackClientUrl, "error:") {
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
	if fallbackClientTimeout != 0 {
		fallbackClient = &timeoutFallbackClient{
			impl:    fallbackClient,
			timeout: fallbackClientTimeout,
		}
	}
	return fallbackClient, nil
}

type SyncProgressBackend interface {
	SyncProgressMap() map[string]interface{}
	SafeBlockNumber(ctx context.Context) (uint64, error)
	FinalizedBlockNumber(ctx context.Context) (uint64, error)
}

func createRegisterAPIBackend(backend *Backend, filterConfig filters.Config, fallbackClientUrl string, fallbackClientTimeout time.Duration) (*filters.FilterSystem, error) {
	fallbackClient, err := CreateFallbackClient(fallbackClientUrl, fallbackClientTimeout)
	if err != nil {
		return nil, err
	}
	backend.apiBackend = &APIBackend{
		b:              backend,
		fallbackClient: fallbackClient,
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
		Service:   filters.NewFilterAPI(filterSystem, false),
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
	return a.b.arb.BlockChain()
}

func (a *APIBackend) GetArbitrumNode() interface{} {
	return a.b.arb.ArbNode()
}

func (a *APIBackend) GetBody(ctx context.Context, hash common.Hash, number rpc.BlockNumber) (*types.Body, error) {
	if body := a.BlockChain().GetBody(hash); body != nil {
		return body, nil
	}
	return nil, errors.New("block body not found")
}

// General Ethereum API
func (a *APIBackend) SyncProgressMap() map[string]interface{} {
	if a.sync == nil {
		res := make(map[string]interface{})
		res["error"] = "sync object not set in apibackend"
		return res
	}
	return a.sync.SyncProgressMap()
}

func (a *APIBackend) SyncProgress() ethereum.SyncProgress {
	progress := a.SyncProgressMap()

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
) (*big.Int, [][]*big.Int, []*big.Int, []float64, error) {
	if core.GetArbOSSpeedLimitPerSecond == nil {
		return nil, nil, nil, nil, errors.New("ArbOS not installed")
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
		return common.Big0, nil, nil, nil, nil
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
		return common.Big0, nil, nil, nil, err
	}
	speedLimit, err := core.GetArbOSSpeedLimitPerSecond(state)
	if err != nil {
		return common.Big0, nil, nil, nil, err
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
			return common.Big0, nil, nil, nil, err
		}
		prevTimestamp = header.Time
	}
	for block := oldestBlock; block <= uint64(baseFeeLookup); block++ {
		header, err := a.HeaderByNumber(ctx, rpc.BlockNumber(block))
		if err != nil {
			return common.Big0, nil, nil, nil, err
		}
		basefees[block-oldestBlock] = header.BaseFee

		if block > uint64(newestBlock) {
			break
		}

		if header.Time > prevTimestamp {
			timeSinceLastTimeChange = header.Time - prevTimestamp
			currentTimestampGasUsed = 0
		}

		receipts := a.BlockChain().GetReceiptsByHash(header.ReceiptHash)
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

	return big.NewInt(int64(oldestBlock)), rewards, basefees, gasUsed, nil
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
		return a.sync.SafeBlockNumber(ctx)
	}
	if number == rpc.FinalizedBlockNumber {
		if a.sync == nil {
			return 0, errors.New("block number not supported: object not set")
		}
		return a.sync.FinalizedBlockNumber(ctx)
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
	return a.BlockChain().GetBlockByNumber(numUint), nil
}

func (a *APIBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return a.BlockChain().GetBlockByHash(hash), nil
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

func (a *APIBackend) stateAndHeaderFromHeader(ctx context.Context, header *types.Header, err error) (*state.StateDB, *types.Header, error) {
	if err != nil {
		return nil, header, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	if !a.BlockChain().Config().IsArbitrumNitro(header.Number) {
		return nil, header, types.ErrUseFallback
	}
	bc := a.BlockChain()
	stateFor := func(header *types.Header) (*state.StateDB, error) {
		return bc.StateAt(header.Root)
	}
	state, lastHeader, err := FindLastAvailableState(ctx, bc, stateFor, header, nil, a.b.config.MaxRecreateStateDepth)
	if err != nil {
		return nil, nil, err
	}
	if lastHeader == header {
		return state, header, nil
	}
	state, err = AdvanceStateUpToBlock(ctx, bc, state, header, lastHeader, nil)
	if err != nil {
		return nil, nil, err
	}
	return state, header, err
}

func (a *APIBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	header, err := a.HeaderByNumber(ctx, number)
	return a.stateAndHeaderFromHeader(ctx, header, err)
}

func (a *APIBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	header, err := a.HeaderByNumberOrHash(ctx, blockNrOrHash)
	return a.stateAndHeaderFromHeader(ctx, header, err)
}

func (a *APIBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, checkLive bool, preferDisk bool) (statedb *state.StateDB, release tracers.StateReleaseFunc, err error) {
	if !a.BlockChain().Config().IsArbitrumNitro(block.Number()) {
		return nil, nil, types.ErrUseFallback
	}
	// DEV: This assumes that `StateAtBlock` only accesses the blockchain and chainDb fields
	return eth.NewArbEthereum(a.b.arb.BlockChain(), a.ChainDb()).StateAtBlock(ctx, block, reexec, base, checkLive, preferDisk)
}

func (a *APIBackend) StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	if !a.BlockChain().Config().IsArbitrumNitro(block.Number()) {
		return nil, vm.BlockContext{}, nil, nil, types.ErrUseFallback
	}
	// DEV: This assumes that `StateAtTransaction` only accesses the blockchain and chainDb fields
	return eth.NewArbEthereum(a.b.arb.BlockChain(), a.ChainDb()).StateAtTransaction(ctx, block, txIndex, reexec)
}

func (a *APIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return a.BlockChain().GetReceiptsByHash(hash), nil
}

func (a *APIBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	if header := a.BlockChain().GetHeaderByHash(hash); header != nil {
		return a.BlockChain().GetTd(hash, header.Number.Uint64())
	}
	return nil
}

func (a *APIBackend) GetEVM(ctx context.Context, msg *core.Message, state *state.StateDB, header *types.Header, vmConfig *vm.Config, blockCtx *vm.BlockContext) (*vm.EVM, func() error) {
	vmError := func() error { return nil }
	if vmConfig == nil {
		vmConfig = a.BlockChain().GetVMConfig()
	}
	txContext := core.NewEVMTxContext(msg)
	var context vm.BlockContext
	if blockCtx != nil {
		context = *blockCtx
	} else {
		context = core.NewEVMBlockContext(header, a.BlockChain(), nil)
	}
	return vm.NewEVM(context, txContext, state, a.BlockChain().Config(), *vmConfig), vmError
}

func (a *APIBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return a.BlockChain().SubscribeChainEvent(ch)
}

func (a *APIBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return a.BlockChain().SubscribeChainHeadEvent(ch)
}

func (a *APIBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return a.BlockChain().SubscribeChainSideEvent(ch)
}

// Transaction pool API
func (a *APIBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return a.b.EnqueueL2Message(ctx, signedTx, nil)
}

func (a *APIBackend) SendConditionalTx(ctx context.Context, signedTx *types.Transaction, options *arbitrum_types.ConditionalOptions) error {
	return a.b.EnqueueL2Message(ctx, signedTx, options)
}

func (a *APIBackend) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	tx, blockHash, blockNumber, index := rawdb.ReadTransaction(a.b.chainDb, txHash)
	return tx, blockHash, blockNumber, index, nil
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
func (a *APIBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := a.b.bloomIndexer.Sections()
	return a.b.config.BloomBitsBlocks, sections
}

func (a *APIBackend) GetLogs(ctx context.Context, hash common.Hash, number uint64) ([][]*types.Log, error) {
	return rawdb.ReadLogs(a.ChainDb(), hash, number), nil
}

func (a *APIBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, a.b.bloomRequests)
	}
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
	return a.BlockChain().Engine()
}

func (b *APIBackend) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return nil, nil
}

func (b *APIBackend) FallbackClient() types.FallbackClient {
	return b.fallbackClient
}

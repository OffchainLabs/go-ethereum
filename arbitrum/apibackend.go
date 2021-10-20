package arbitrum

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/bloombits"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

type APIBackend struct {
	b *Backend
}

func createRegisterAPIBackend(backend *Backend) {
	backend.apiBackend = &APIBackend{
		b: backend,
	}
	backend.stack.RegisterAPIs(backend.apiBackend.GetAPIs())
}

func (a *APIBackend) GetAPIs() []rpc.API {
	return ethapi.GetAPIs(a)
}

// General Ethereum API
func (a *APIBackend) SyncProgress() ethereum.SyncProgress {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) FeeHistory(ctx context.Context, blockCount int, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (*big.Int, [][]*big.Int, []*big.Int, []float64, error) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) ChainDb() ethdb.Database {
	return a.b.ethDatabase
}

func (a *APIBackend) AccountManager() *accounts.Manager {
	return a.b.stack.AccountManager()
}

func (a *APIBackend) ExtRPCEnabled() bool {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) RPCGasCap() uint64 {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) RPCTxFeeCap() float64 {
	return a.b.ethConfig.RPCTxFeeCap
}

func (a *APIBackend) UnprotectedAllowed() bool {
	return true // TODO: is that true?
}

// Blockchain API
func (a *APIBackend) SetHead(number uint64) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	if number == rpc.LatestBlockNumber {
		return a.b.blockChain.CurrentBlock().Header(), nil
	}
	return a.b.blockChain.GetHeaderByNumber(uint64(number.Int64())), nil
}

func (a *APIBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return a.b.blockChain.GetHeaderByHash(hash), nil
}

func (a *APIBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	number, isnum := blockNrOrHash.Number()
	if isnum {
		return a.HeaderByNumber(ctx, number)
	}
	hash, ishash := blockNrOrHash.Hash()
	if ishash {
		return a.HeaderByHash(ctx, hash)
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (a *APIBackend) CurrentHeader() *types.Header {
	return a.b.blockChain.CurrentHeader()
}

func (a *APIBackend) CurrentBlock() *types.Block {
	return a.b.blockChain.CurrentBlock()
}

func (a *APIBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	return a.b.blockChain.GetBlockByNumber(uint64(number.Int64())), nil
}

func (a *APIBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return a.b.blockChain.GetBlockByHash(hash), nil
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

func (a *APIBackend) stateAndHeaderFromHeader(header *types.Header, err error) (*state.StateDB, *types.Header, error) {
	if err != nil {
		return nil, header, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	state, err := a.b.blockChain.StateAt(header.Root)
	return state, header, err
}

func (a *APIBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	return a.stateAndHeaderFromHeader(a.HeaderByNumber(ctx, number))
}

func (a *APIBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	return a.stateAndHeaderFromHeader(a.HeaderByNumberOrHash(ctx, blockNrOrHash))
}

func (a *APIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return a.b.blockChain.GetReceiptsByHash(hash), nil
}

func (a *APIBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmConfig *vm.Config) (*vm.EVM, func() error, error) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return a.b.blockChain.SubscribeChainEvent(ch)
}

func (a *APIBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return a.b.blockChain.SubscribeChainHeadEvent(ch)
}

func (a *APIBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return a.b.blockChain.SubscribeChainSideEvent(ch)
}

// Transaction pool API
func (a *APIBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return a.b.EnqueueL2Message(signedTx)
}

func (a *APIBackend) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	tx, blockHash, blockNumber, index := rawdb.ReadTransaction(a.b.ethDatabase, txHash)
	return tx, blockHash, blockNumber, index, nil
}

func (a *APIBackend) GetPoolTransactions() (types.Transactions, error) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) GetPoolTransaction(txHash common.Hash) *types.Transaction {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) Stats() (pending int, queued int) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) TxPoolContentFrom(addr common.Address) (types.Transactions, types.Transactions) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) SubscribeNewTxsEvent(_ chan<- core.NewTxsEvent) event.Subscription {
	panic("not implemented") // TODO: Implement
}

// Filter API
func (a *APIBackend) BloomStatus() (uint64, uint64) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) GetLogs(ctx context.Context, blockHash common.Hash) ([][]*types.Log, error) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	panic("not implemented") // TODO: Implement
}

func (a *APIBackend) ChainConfig() *params.ChainConfig {
	return a.b.blockChain.Config()
}

func (a *APIBackend) Engine() consensus.Engine {
	return a.b.blockChain.Engine()
}

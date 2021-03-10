package deepmind

import (
	"math/big"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/atomic"
)

// NoOpContext can be used when no recording should happen for a given code path
var NoOpContext *Context

var syncContext *Context = NewContext(&DelegateToWriterPrinter{writer: os.Stdout})

// MaybeSyncContext is used when syncing blocks with the network for mindreader consumption, there
// is always a single active sync context use for the whole syncing process, should not be used
// for other purposes.
//
// It responsibility of the user of sync context to ensure it's being used in a concurrent safe
// way and to handle its lifecycle behavior (like resetting it at the end of a block).
func MaybeSyncContext() *Context {
	if !Enabled {
		return NoOpContext
	}

	if !SyncInstrumentationEnabled {
		return NoOpContext
	}

	return syncContext
}

// SyncContext returns the sync context without any checking if deep mind is enabled or not. Use
// it only for specific cases and ensure you only use it when it's strictly correct to do so as this
// will print stdout lines.
func SyncContext() *Context {
	return syncContext
}

// Context is a block level data container used throughout deep mind instrumentation to
// keep active state about current instrumentation. This contains method to deal with
// block, transaction and call metadata required for proper functionning of Deep Mind
// code.
type Context struct {
	printer Printer

	blockLogIndex   uint64
	activeCallIndex string
	nextCallIndex   uint64
	callIndexStack  *ExtendedStack

	seenBlock     *atomic.Bool
	inBlock       *atomic.Bool
	inTransaction *atomic.Bool
}

func NewContext(printer Printer) *Context {
	ctx := &Context{
		printer: printer,

		activeCallIndex: "0",
		callIndexStack:  &ExtendedStack{},

		seenBlock:     atomic.NewBool(false),
		inBlock:       atomic.NewBool(false),
		inTransaction: atomic.NewBool(false),
	}

	ctx.callIndexStack.Push(ctx.activeCallIndex)

	return ctx
}

func NewSpeculativeExecutionContext() *Context {
	return NewContext(NewToBufferPrinter())
}

func (ctx *Context) Enabled() bool {
	return ctx != nil
}

func (ctx *Context) DeepMindLog() []byte {
	if ctx == nil {
		return nil
	}

	if v, ok := ctx.printer.(*ToBufferPrinter); ok {
		return v.buffer.Bytes()
	}

	return nil
}

// Block methods

func (ctx *Context) StartBlock(block *types.Block) {
	if !ctx.inBlock.CAS(false, true) {
		panic("entering a block while already in a block scope")
	}

	ctx.seenBlock.Store(true)
	ctx.printer.Print("BEGIN_BLOCK", Uint64(block.NumberU64()))
}

func (ctx *Context) FinalizeBlock(block *types.Block) {
	// We must not check if the finalize block is actually in the a block since
	// when deep mind block progress only is enabled, it would hit a panic
	ctx.printer.Print("FINALIZE_BLOCK", Uint64(block.NumberU64()))
}

// ExitBlock is used when an abnormal condition is encountered while processing
// transactions and we must end the block processing right away, resetting the start
// along the way.
func (ctx *Context) ExitBlock() {
	if !ctx.inBlock.CAS(true, false) {
		panic("exiting a block while not already within a block scope")
	}
	ctx.blockLogIndex = 0
}

func (ctx *Context) EndBlock(block *types.Block) {
	ctx.ExitBlock()

	ctx.printer.Print("END_BLOCK",
		Uint64(block.NumberU64()),
		Uint64(uint64(block.Size())),
		JSON(map[string]interface{}{
			"header": block.Header(),
			"uncles": block.Body().Uncles,
		}),
	)
}

// Transaction methods

func (ctx *Context) StartTransaction(tx *types.Transaction) {
	if ctx == nil {
		return
	}

	hash := tx.Hash()
	v, r, s := tx.RawSignatureValues()

	ctx.StartTransactionRaw(
		hash,
		tx.To(),
		tx.Value(),
		v.Bytes(),
		r.Bytes(),
		s.Bytes(),
		tx.Gas(),
		tx.GasPrice(),
		tx.Nonce(),
		tx.Data(),
	)
}

func (ctx *Context) StartTransactionRaw(
	hash common.Hash,
	to *common.Address,
	value *big.Int,
	v, r, s []byte,
	gasLimit uint64,
	gasPrice *big.Int,
	nonce uint64,
	data []byte,
) {
	if ctx == nil {
		return
	}

	ctx.openTransaction()

	// We start assuming the "null" value (i.e. a dot character), and update if `to` is set
	toAsString := "."
	if to != nil {
		toAsString = Addr(*to)
	}

	ctx.printer.Print("BEGIN_APPLY_TRX",
		Hash(hash),
		toAsString,
		Hex(value.Bytes()),
		Hex(v),
		Hex(r),
		Hex(s),
		Uint64(gasLimit),
		Hex(gasPrice.Bytes()),
		Uint64(nonce),
		Hex(data),
	)
}

func (ctx *Context) openTransaction() {
	if !ctx.inTransaction.CAS(false, true) {
		panic("entering a transaction while already in a transaction scope")
	}
}

func (ctx *Context) RecordTrxFrom(from common.Address) {
	if ctx == nil {
		return
	}

	if !ctx.inTransaction.Load() {
		debug.PrintStack()
		panic("the RecordTrxFrom should have been call within a transaction, something is deeply wrong")
	}

	ctx.printer.Print("TRX_FROM", Addr(from))
}

func (ctx *Context) RecordFailedTransaction(err error) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("FAILED_APPLY_TRX", err.Error())
	if !ctx.inTransaction.CAS(true, false) {
		panic("exiting a transaction while not already within a transaction scope")
	}
}

func (ctx *Context) EndTransaction(receipt *types.Receipt) {
	if ctx == nil {
		return
	}

	if !ctx.inTransaction.CAS(true, false) {
		panic("exiting a transaction while not already within a transaction scope")
	}

	logItems := make([]logItem, len(receipt.Logs))
	for i, log := range receipt.Logs {
		logItems[i] = logItem{
			"address": log.Address,
			"topics":  log.Topics,
			"data":    hexutil.Bytes(log.Data),
		}
	}

	ctx.printer.Print(
		"END_APPLY_TRX",
		Uint64(receipt.GasUsed),
		Hex(receipt.PostState),
		Uint64(receipt.CumulativeGasUsed),
		Hex(receipt.Bloom[:]),
		JSON(logItems),
	)

	ctx.nextCallIndex = 0
	ctx.activeCallIndex = "0"
	ctx.callIndexStack = &ExtendedStack{}
	ctx.callIndexStack.Push(ctx.activeCallIndex)
}

// Call methods

func (ctx *Context) StartCall(callType string) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_RUN_CALL", callType, ctx.openCall())
}

func (ctx *Context) openCall() string {
	ctx.nextCallIndex++
	ctx.activeCallIndex = strconv.FormatUint(ctx.nextCallIndex, 10)

	ctx.callIndexStack.Push(ctx.activeCallIndex)

	return ctx.activeCallIndex
}

func (ctx *Context) callIndex() string {
	if ctx.seenBlock.Load() && !ctx.inBlock.Load() {
		debug.PrintStack()
		panic("should have been call in a block, something is deeply wrong")
	}

	return ctx.activeCallIndex
}

func (ctx *Context) RecordCallParams(callType string, caller common.Address, callee common.Address, value *big.Int, gasLimit uint64, input []byte) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_PARAM", callType, ctx.callIndex(), Addr(caller), Addr(callee), Hex(value.Bytes()), Uint64(gasLimit), Hex(input))
}

func (ctx *Context) RecordCallWithoutCode() {
	if ctx == nil {
		return
	}

	ctx.printer.Print("ACCOUNT_WITHOUT_CODE", ctx.callIndex())
}

func (ctx *Context) RecordCallFailed(gasLeft uint64, reason string) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_CALL_FAILED", ctx.callIndex(), Uint64(gasLeft), reason)
}

func (ctx *Context) RecordCallReverted() {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_REVERTED", ctx.callIndex())
}

func (ctx *Context) closeCall() string {
	previousIndex := ctx.callIndexStack.MustPop()
	ctx.activeCallIndex = ctx.callIndexStack.MustPeek()

	return previousIndex
}

func (ctx *Context) EndCall(gasLeft uint64, returnValue []byte) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_END_CALL", ctx.closeCall(), Uint64(gasLeft), Hex(returnValue))
}

// In-call methods

func (ctx *Context) RecordKeccak(hashOfdata common.Hash, data []byte) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("EVM_KECCAK", ctx.callIndex(), Hash(hashOfdata), Hex(data))
}

func (ctx *Context) RecordGasRefund(gasOld, gasRefund uint64) {
	if ctx == nil {
		return
	}

	if gasRefund != 0 {
		ctx.printer.Print("GAS_CHANGE", ctx.callIndex(), Uint64(gasOld), Uint64(gasOld+gasRefund), string(RefundAfterExecutionGasChangeReason))
	}
}

func (ctx *Context) RecordGasConsume(gasOld, gasConsumed uint64, reason GasChangeReason) {
	if ctx == nil {
		return
	}

	if gasConsumed != 0 && reason != IgnoredGasChangeReason {
		ctx.printer.Print("GAS_CHANGE", ctx.callIndex(), Uint64(gasOld), Uint64(gasOld-gasConsumed), string(reason))
	}
}

func (ctx *Context) RecordStorageChange(addr common.Address, key, oldData, newData common.Hash) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("STORAGE_CHANGE", ctx.callIndex(), Addr(addr), Hash(key), Hash(oldData), Hash(newData))
}

func (ctx *Context) RecordBalanceChange(addr common.Address, oldBalance, newBalance *big.Int, reason BalanceChangeReason) {
	if ctx == nil {
		return
	}

	if reason != IgnoredBalanceChangeReason {
		// THOUGHTS: There is a choice between storage vs CPU here as we store the old balance and the new balance.
		//           Usually, balances are quite big. Storing instead the old balance and the delta would probably
		//           reduce a lot the storage space at the expense of CPU time to compute the delta and recomputed
		//           the new balance in place where it's required. This would need to be computed (the space
		//           savings) to see if it make sense to apply it or not.
		ctx.printer.Print("BALANCE_CHANGE", ctx.callIndex(), Addr(addr), BigInt(oldBalance), BigInt(newBalance), string(reason))
	}
}

func (ctx *Context) RecordLog(log *types.Log) {
	if ctx == nil {
		return
	}

	strtopics := make([]string, len(log.Topics))
	for idx, topic := range log.Topics {
		strtopics[idx] = Hash(topic)
	}

	ctx.printer.Print("ADD_LOG", ctx.callIndex(), ctx.logIndexInBlock(), Addr(log.Address), strings.Join(strtopics, ","), Hex(log.Data))
}

func (ctx *Context) logIndexInBlock() string {
	current := strconv.FormatUint(ctx.blockLogIndex, 10)
	ctx.blockLogIndex++
	return current
}

func (ctx *Context) RecordSuicide(addr common.Address, suicided bool, balanceBeforeSuicide *big.Int) {
	if ctx == nil {
		return
	}

	// This infers a balance change, a reduction from this account. In the `opSuicide` op code, the corresponding AddBalance is emitted.
	ctx.printer.Print("SUICIDE_CHANGE", ctx.callIndex(), Addr(addr), Bool(suicided), BigInt(balanceBeforeSuicide))

	if balanceBeforeSuicide.Sign() != 0 {
		// We need to explicit add a balance change removing the suicided contract balance since
		// the remaining balance of the contract has already been resetted to 0 by the time we
		// do the print call.
		ctx.RecordBalanceChange(addr, balanceBeforeSuicide, common.Big0, BalanceChangeReason("suicide_withdraw"))
	}
}

func (ctx *Context) RecordNewAccount(addr common.Address) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("CREATED_ACCOUNT", ctx.callIndex(), Addr(addr))
}

func (ctx *Context) RecordCodeChange(addr common.Address, inputHash, prevCode []byte, codeHash common.Hash, code []byte) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("CODE_CHANGE", ctx.callIndex(), Addr(addr), Hex(inputHash), Hex(prevCode), Hash(codeHash), Hex(code))
}

func (ctx *Context) RecordNonceChange(addr common.Address, oldNonce, newNonce uint64) {
	if ctx == nil {
		return
	}

	ctx.printer.Print("NONCE_CHANGE", ctx.callIndex(), Addr(addr), Uint64(oldNonce), Uint64(newNonce))
}

func (ctx *Context) RecordBeforeCallGasEvent(gasValue uint64) {
	if ctx == nil {
		return
	}

	// The `ctx.nextCallIndex` has not been incremented yet, so we add +1 for the linked call index
	ctx.printer.Print("GAS_EVENT", ctx.callIndex(), Uint64(ctx.nextCallIndex+1), string(BeforeCallGasEventID), Uint64(gasValue))
}

func (ctx *Context) RecordAfterCallGasEvent(gasValue uint64) {
	if ctx == nil {
		return
	}

	// The `ctx.nextCallIndex` is already pointing to previous call index, so we simply use it for the linked call index
	ctx.printer.Print("GAS_EVENT", ctx.callIndex(), Uint64(ctx.nextCallIndex), string(AfterCallGasEventID), Uint64(gasValue))
}

// Mempool methods

func (ctx *Context) RecordTrxPool(eventType string, tx *types.Transaction, err error) {
	if ctx == nil {
		return
	}

	var signer types.Signer = types.FrontierSigner{}
	if tx.Protected() {
		signer = types.NewEIP155Signer(tx.ChainId())
	}

	fromAsString := "."
	from, err := types.Sender(signer, tx)
	if err == nil {
		fromAsString = Addr(from)
	}

	toAsString := "."
	if tx.To() != nil {
		toAsString = Addr(*tx.To())
	}

	v, r, s := tx.RawSignatureValues()

	//todo: handle error message

	ctx.printer.Print(
		eventType,
		Hash(tx.Hash()),
		fromAsString,
		toAsString,
		Hex(tx.Value().Bytes()),
		Hex(v.Bytes()),
		Hex(r.Bytes()),
		Hex(s.Bytes()),
		Uint64(tx.Gas()),
		Hex(tx.GasPrice().Bytes()),
		Uint64(tx.Nonce()),
		Hex(tx.Data()),
	)
}

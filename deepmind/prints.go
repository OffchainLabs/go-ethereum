package deepmind

import (
	"math/big"
	"runtime/debug"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/golang-collections/collections/stack"
	"go.uber.org/atomic"
)

var EmptyValue = new(big.Int)

var activeIndex = "0"
var nextIndex = uint64(0)
var indexStack = &ExtendedStack{}

var logIndex = uint64(0)

var seenBlock = atomic.NewBool(false)
var inBlock = atomic.NewBool(false)
var inTransaction = atomic.NewBool(false)

func init() {
	indexStack.Push(activeIndex)
}

func EnterBlock() {
	if !inBlock.CAS(false, true) {
		panic("entering a block while already in a block scope")
	}

	seenBlock.Store(true)
}

func BeginTransaction(printer Printer, tx *types.Transaction) {
	if printer.Disabled() {
		return
	}

	hash := tx.Hash()
	v, r, s := tx.RawSignatureValues()

	EnterTransaction()
	BeginApplyTransaction(printer,
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

// BeginApplyTransaction unlike `BeginTransaction` does not enter a transaction so we are able to process
// multiple transaction in parallel.
func BeginApplyTransaction(
	printer Printer,
	hash common.Hash,
	to *common.Address,
	value *big.Int,
	v, r, s []byte,
	gasLimit uint64,
	gasPrice *big.Int,
	nonce uint64,
	data []byte,
) {
	if printer.Disabled() {
		return
	}

	// We start assuming the "null" value (i.e. a dot character), and update if `to` is set
	toAsString := "."
	if to != nil {
		toAsString = Addr(*to)
	}

	printer.Print("BEGIN_APPLY_TRX",
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

func FailedTransaction(printer Printer, err error) {
	if printer.Disabled() {
		return
	}

	printer.Print("FAILED_APPLY_TRX", err.Error())
	if !inTransaction.CAS(true, false) {
		panic("exiting a transaction while not already within a transaction scope")
	}
}

type logItem = map[string]interface{}

func EndTransaction(printer Printer, receipt *types.Receipt) {
	if printer.Disabled() {
		return
	}

	if !inTransaction.CAS(true, false) {
		panic("exiting a transaction while not already within a transaction scope")
	}

	EndApplyTransaction(printer, receipt)
}

// EndApplyTransaction unlike `EndTransaction` does not enter a transaction so we are able to process
// multiple transaction in parallel.
func EndApplyTransaction(printer Printer, receipt *types.Receipt) {
	if printer.Disabled() {
		return
	}

	logItems := make([]logItem, len(receipt.Logs))
	for i, log := range receipt.Logs {
		logItems[i] = logItem{
			"address": log.Address,
			"topics":  log.Topics,
			"data":    hexutil.Bytes(log.Data),
		}
	}

	printer.Print(
		"END_APPLY_TRX",
		Uint64(receipt.GasUsed),
		Hex(receipt.PostState),
		Uint64(receipt.CumulativeGasUsed),
		Hex(receipt.Bloom[:]),
		JSON(logItems),
	)
}

func IsInTransaction() bool {
	return inTransaction.Load()
}

func EnterTransaction() {
	// FIXME: Should we make some validation here?
	nextIndex = 0

	if !inTransaction.CAS(false, true) {
		panic("entering a transaction while already in a transaction scope")
	}
}

func EndBlock() {
	if !inBlock.CAS(true, false) {
		panic("exiting a block while not already within a block scope")
	}
	logIndex = 0
}

func CallEnter() string {
	nextIndex++
	activeIndex = strconv.FormatUint(nextIndex, 10)

	indexStack.Push(activeIndex)

	return activeIndex
}

func CallIndex() string {
	if seenBlock.Load() && !inBlock.Load() {
		debug.PrintStack()
		panic("should have been call in a block, something is deeply wrong")
	}

	return activeIndex
}

func CallReturn() string {
	previousIndex := indexStack.MustPop()
	activeIndex = indexStack.MustPeek()

	return previousIndex
}

func LogIndex() string {
	current := strconv.FormatUint(logIndex, 10)
	logIndex++
	return current
}

type ExtendedStack struct {
	stack.Stack
}

func (s *ExtendedStack) MustPop() string {
	popped := s.Pop()
	if popped == nil {
		panic("at least one element must exist in the index stack at this point")
	}

	return popped.(string)
}

func (s *ExtendedStack) MustPeek() string {
	peeked := s.Peek()
	if peeked == nil {
		panic("at least one element must exist in the index stack at this point")
	}

	return peeked.(string)
}

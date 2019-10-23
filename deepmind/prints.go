package deepmind

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/golang-collections/collections/stack"
	"go.uber.org/atomic"
)

var EmptyValue = new(big.Int)

var activeIndex = "0"
var nextIndex = uint64(0)
var indexStack = &ExtendedStack{}

var seenBlock = atomic.NewBool(false)
var inBlock = atomic.NewBool(false)
var inTransaction = atomic.NewBool(false)

func init() {
	indexStack.Push(activeIndex)
}

func IsInTransaction() bool {
	return inTransaction.Load()
}

func EnterBlock() {
	if !inBlock.CAS(false, true) {
		panic("entering a block while already in a block scope")
	}

	seenBlock.Store(true)
}

func EnterTransaction() {
	// FIXME: Should we make some validation here?
	nextIndex = 0

	if !inTransaction.CAS(false, true) {
		panic("entering a transaction while already in a transaction scope")
	}
}

func EndTransaction() {
	if !inTransaction.CAS(true, false) {
		panic("exiting a transaction while not already within a transaction scope")
	}
}

func EndBlock() {
	if !inBlock.CAS(true, false) {
		panic("exiting a block while not already within a block scope")
	}
}

func Print(input ...string) {
	// All Print should be wrapped to avoid unecessary memory allocation
	// to happen. This is just a safe guard, even wondering if we should
	// not remove it altogether.
	if !Enabled && !BlockProgressEnabled {
		return
	}

	line := "DMLOG " + strings.Join(input, " ") + "\n"
	var written int
	var err error
	var i int
	loops := 10
	for i = 0; i < loops; i++ {
		toWrite := line[written:]
		written, err = fmt.Print(toWrite)
		if len(toWrite) != written {
			continue
		}
		if err == nil {
			return
		}
		if i == loops - 1 {
			fmt.Println("\nDMLOG FAILED WRITING:", err)
		}
	}
	fmt.Printf("\nDMLOG FAILED RETRIES %d\n", loops)
}

func PrintEnterCall(callType string) {
	Print("EVM_RUN_CALL", callType, CallEnter())
}

func PrintCallParams(callType string, caller common.Address, callee common.Address, value *big.Int, gasLimit uint64, input []byte) {
	Print("EVM_PARAM", callType, CallIndex(), Addr(caller), Addr(callee), Hex(value.Bytes()), Uint64(gasLimit), Hex(input))
}

func PrintTrxPool(callType string, tx *types.Transaction, err error) {
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

	Print(
		callType,
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

func PrintCallFailed(gasLeft uint64, reason string) {
	Print("EVM_CALL_FAILED", CallIndex(), Uint64(gasLeft), reason)
}

func PrintCallReverted() {
	Print("EVM_REVERTED", CallIndex())
}

func PrintEndCall(gasLeft uint64, returnValue []byte) {
	Print("EVM_END_CALL", CallReturn(), Uint64(gasLeft), Hex(returnValue))
}

func Addr(in common.Address) string {
	return hex.EncodeToString(in[:])
}

func Bool(in bool) string {
	if in {
		return "true"
	}

	return "false"
}

func Hash(in common.Hash) string {
	return hex.EncodeToString(in[:])
}

func Hex(in []byte) string {
	if len(in) == 0 {
		return "."
	}

	return hex.EncodeToString(in)
}

func BigInt(in *big.Int) string {
	return Hex(in.Bytes())
}

func Uint(in uint) string {
	return strconv.FormatUint(uint64(in), 10)
}

func Uint64(in uint64) string {
	return strconv.FormatUint(in, 10)
}

func JSON(in interface{}) string {
	out, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}

	return string(out)
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

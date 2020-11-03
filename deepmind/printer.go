package deepmind

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var globalPrinter = &DelegateToWriterPrinter{writer: os.Stdout}
var DiscardingPrinter = &NoOpPrinter{}

func GlobalPrinter() Printer {
	if !SyncInstrumentationEnabled {
		return DiscardingPrinter
	}

	return globalPrinter
}

type Printer interface {
	// Disabled determines if this printer is actually doing a no-op operation
	// and as such, further computation can be skipped altogether.
	Disabled() bool

	// **Important!**
	//
	// All `Print` should be wrapped around an if condition at call site
	// to avoid unecessary memory allocation to happen.
	//
	// There is **NO** safe guard here, it's printed right away
	Print(input ...string)
}

type NoOpPrinter struct {
}

func (p *NoOpPrinter) Disabled() bool {
	return true
}

func (p *NoOpPrinter) Print(input ...string) {
}

type DelegateToWriterPrinter struct {
	writer io.Writer
}

func (p *DelegateToWriterPrinter) Disabled() bool {
	return false
}

func (p *DelegateToWriterPrinter) Print(input ...string) {
	line := "DMLOG " + strings.Join(input, " ") + "\n"
	var written int
	var err error
	loops := 10
	for i := 0; i < loops; i++ {
		written, err = fmt.Fprint(p.writer, line)

		if len(line) == written {
			return
		}

		line = line[written:]

		if i == loops-1 {
			break
		}
	}

	errstr := fmt.Sprintf("\nDMLOG FAILED WRITING %dx: %s\n", loops, err)
	ioutil.WriteFile("/tmp/deep_mind_writer_failed_print.log", []byte(errstr), 0644)
	fmt.Fprint(p.writer, errstr)
}

type ToBufferPrinter struct {
	buffer *bytes.Buffer
}

func NewToBufferPrinter() *ToBufferPrinter {
	return &ToBufferPrinter{
		buffer: bytes.NewBuffer(nil),
	}
}

func (p *ToBufferPrinter) Disabled() bool {
	return false
}

func (p *ToBufferPrinter) Print(input ...string) {
	p.buffer.WriteString("DMLOG " + strings.Join(input, " ") + "\n")
}

func (p *ToBufferPrinter) Buffer() *bytes.Buffer {
	return p.buffer
}

// Helper Shortcuts

func Print(printer Printer, input ...string) {
	if printer.Disabled() {
		return
	}

	printer.Print(input...)
}

func PrintEnterCall(printer Printer, callType string) {
	if printer.Disabled() {
		return
	}

	printer.Print("EVM_RUN_CALL", callType, CallEnter())
}

func PrintCallParams(printer Printer, callType string, caller common.Address, callee common.Address, value *big.Int, gasLimit uint64, input []byte) {
	if printer.Disabled() {
		return
	}

	printer.Print("EVM_PARAM", callType, CallIndex(), Addr(caller), Addr(callee), Hex(value.Bytes()), Uint64(gasLimit), Hex(input))
}

func PrintTrxPool(printer Printer, eventType string, tx *types.Transaction, err error) {
	if printer.Disabled() {
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

	printer.Print(
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

func PrintTrxFrom(printer Printer, from common.Address) {
	if printer.Disabled() {
		return
	}

	if !IsInTransaction() {
		debug.PrintStack()
		panic("the PrintTrxFrom should have been call within a transaction, something is deeply wrong")
	}

	printer.Print("TRX_FROM", Addr(from))
}

func PrintCallFailed(printer Printer, gasLeft uint64, reason string) {
	if printer.Disabled() {
		return
	}

	printer.Print("EVM_CALL_FAILED", CallIndex(), Uint64(gasLeft), reason)
}

func PrintCallReverted(printer Printer) {
	if printer.Disabled() {
		return
	}

	printer.Print("EVM_REVERTED", CallIndex())
}

func PrintEndCall(printer Printer, gasLeft uint64, returnValue []byte) {
	if printer.Disabled() {
		return
	}

	printer.Print("EVM_END_CALL", CallReturn(), Uint64(gasLeft), Hex(returnValue))
}

func PrintEVMKeccak(printer Printer, hashOfdata common.Hash, data []byte) {
	if printer.Disabled() {
		return
	}

	printer.Print("EVM_KECCAK", CallIndex(), Hash(hashOfdata), Hex(data))
}

func PrintGasRefund(printer Printer, gasOld, gasRefund uint64) {
	if printer.Disabled() {
		return
	}

	if gasRefund != 0 {
		printer.Print("GAS_CHANGE", CallIndex(), Uint64(gasOld), Uint64(gasOld+gasRefund), string(RefundAfterExecutionGasChangeReason))
	}
}

func PrintGasConsume(printer Printer, gasOld, gasConsumed uint64, reason GasChangeReason) {
	if printer.Disabled() {
		return
	}

	if gasConsumed != 0 && reason != IgnoredGasChangeReason {
		printer.Print("GAS_CHANGE", CallIndex(), Uint64(gasOld), Uint64(gasOld-gasConsumed), string(reason))
	}
}

func PrintStorageChange(printer Printer, addr common.Address, key, oldData, newData common.Hash) {
	if printer.Disabled() {
		return
	}

	printer.Print("STORAGE_CHANGE", CallIndex(), Addr(addr), Hash(key), Hash(oldData), Hash(newData))
}

func PrintBalanceChange(printer Printer, addr common.Address, oldBalance, newBalance *big.Int, reason BalanceChangeReason) {
	if printer.Disabled() {
		return
	}

	if reason != IgnoredBalanceChangeReason {
		// THOUGHTS: There is a choice between storage vs CPU here as we store the old balance and the new balance.
		//           Usually, balances are quite big. Storing instead the old balance and the delta would probably
		//           reduce a lot the storage space at the expense of CPU time to compute the delta and recomputed
		//           the new balance in place where it's required. This would need to be computed (the space
		//           savings) to see if it make sense to apply it or not.
		printer.Print("BALANCE_CHANGE", CallIndex(), Addr(addr), BigInt(oldBalance), BigInt(newBalance), string(reason))
	}
}

func PrintAddLog(printer Printer, log *types.Log) {
	if printer.Disabled() {
		return
	}

	strtopics := make([]string, len(log.Topics))
	for idx, topic := range log.Topics {
		strtopics[idx] = Hash(topic)
	}

	printer.Print("ADD_LOG", CallIndex(), LogIndex(), Addr(log.Address), strings.Join(strtopics, ","), Hex(log.Data))
}

func PrintSuicide(printer Printer, addr common.Address, suicided bool, balanceBeforeSuicide *big.Int) {
	if printer.Disabled() {
		return
	}

	// This infers a balance change, a reduction from this account. In the `opSuicide` op code, the corresponding AddBalance is emitted.
	printer.Print("SUICIDE_CHANGE", CallIndex(), Addr(addr), Bool(suicided), BigInt(balanceBeforeSuicide))

	if balanceBeforeSuicide.Sign() != 0 {
		// We need to explicit add a balance change removing the suicided contract balance since
		// the remaining balance of the contract has already been resetted to 0 by the time we
		// do the print call.
		PrintBalanceChange(printer, addr, balanceBeforeSuicide, common.Big0, BalanceChangeReason("suicide_withdraw"))
	}
}

func PrintCreatedAccount(printer Printer, addr common.Address) {
	if printer.Disabled() {
		return
	}

	// TODO: the CallIndex should be attached to the NEXT EVM call.
	// add as `pendingCreateAccount`, prochain EVM_CALL start whatever ,il les pluck et les
	// clear.
	printer.Print("CREATED_ACCOUNT", CallIndex(), Addr(addr))
	// TODO: in our data, we simply flag `account_created: true`,
	// and index in `search` accordingly.
	// created:true address:0x123123123213213
	// { creatorCall
	// } }
}

func PrintCodeChange(printer Printer, addr common.Address, inputHash, prevCode []byte, codeHash common.Hash, code []byte) {
	if printer.Disabled() {
		return
	}

	printer.Print("CODE_CHANGE", CallIndex(), Addr(addr), Hex(inputHash), Hex(prevCode), Hash(codeHash), Hex(code))
}

func PrintNonceChange(printer Printer, addr common.Address, oldNonce, newNonce uint64) {
	if printer.Disabled() {
		return
	}

	printer.Print("NONCE_CHANGE", CallIndex(), Addr(addr), Uint64(oldNonce), Uint64(newNonce))
}

func PrintBeforeCallGasEvent(printer Printer, gasValue uint64) {
	if printer.Disabled() {
		return
	}

	// The `nextIndex` has not been incremented yet, so we add +1 for the linked call index
	printer.Print("GAS_EVENT", CallIndex(), Uint64(nextIndex+1), string(BeforeCallGasEventID), Uint64(gasValue))
}

func PrintAfterCallGasEvent(printer Printer, gasValue uint64) {
	if printer.Disabled() {
		return
	}

	// The `nextIndex` is already pointing to previous call index, so we simply use it for the linked call index
	printer.Print("GAS_EVENT", CallIndex(), Uint64(nextIndex), string(AfterCallGasEventID), Uint64(gasValue))
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

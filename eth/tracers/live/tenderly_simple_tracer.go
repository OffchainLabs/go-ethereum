package live

import (
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtracing "github.com/ethereum/go-ethereum/core/tracing"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"math/big"
)

// TenderlySimpleTracer is an interface that is used to trace the execution of a block.
type TenderlySimpleTracer interface {
	OnBlockStart(event gethtracing.BlockEvent)
	OnBlockEnd(err error)
	OnClose()
	OnSystemCallStart()
	OnSystemCallStartV2(ctx *gethtracing.VMContext)
	OnSystemCallEnd()
	OnTxStart(ctx *gethtracing.VMContext, tx *gethtypes.Transaction, from gethcommon.Address)
	OnTxEnd(receipt *gethtypes.Receipt, err error)
	OnEnter(_ int, typ byte, from gethcommon.Address, to gethcommon.Address, input []byte, gas uint64, value *big.Int)
	OnExit(_ int, output []byte, used uint64, err error, reverted bool)
	OnOpcode(pc uint64, typ byte, gas, cost uint64, scope gethtracing.OpContext, rData []byte, depth int, err error)
	OnBalanceChange(addr gethcommon.Address, prev *big.Int, n *big.Int, reason gethtracing.BalanceChangeReason)
	OnNonceChange(addr gethcommon.Address, prev, new uint64)
	OnCodeChange(addr gethcommon.Address, prevCodeHash gethcommon.Hash, prevCode []byte, codeHash gethcommon.Hash, code []byte)
	OnStorageChange(addr gethcommon.Address, slot gethcommon.Hash, prev, new gethcommon.Hash)
	OnBalanceRead(addr gethcommon.Address, bal *big.Int)
	OnNonceRead(addr gethcommon.Address, nonce uint64)
	OnCodeRead(addr gethcommon.Address, code []byte)
	OnCodeSizeRead(addr gethcommon.Address, size int)
	OnCodeHashRead(addr gethcommon.Address, hash gethcommon.Hash)
	OnStorageRead(addr gethcommon.Address, slot gethcommon.Hash, value gethcommon.Hash)
}

// Ensure that simpleTracer implements the TenderlySimpleTracer interface.
var _ TenderlySimpleTracer = (*simpleTracer)(nil)

// simpleTracer is a simple implementation of the TenderlySimpleTracer interface.
// It just represents the tenderly tracing flow.
// We hook to the gethtracing.Hooks to get the tracing events.
// We build the block and transaction data from the tracing events.
// OnBlockEnd, we send the block data to the tenderly pipeline for further processing.
type simpleTracer struct {
	block *gethtypes.Block
	tx    *gethtypes.Transaction
}

// NewTenderlySimpleTracer creates a new instance of the simpleTracer.
func NewTenderlySimpleTracer() TenderlySimpleTracer {
	return &simpleTracer{}
}

func (t *simpleTracer) OnBlockStart(event gethtracing.BlockEvent) {
	//log.Info("tenderly::OnBlockStart", "event", event)
	t.block = event.Block
}

func (t *simpleTracer) OnBlockEnd(err error) {
	//log.Info("tenderly::OnBlockEnd", "err", err)
}

func (t *simpleTracer) OnClose() {
	//log.Info("tenderly::OnClose")
}

func (t *simpleTracer) OnSystemCallStart() {
	//log.Info("tenderly::OnSystemCallStart")
}

func (t *simpleTracer) OnSystemCallStartV2(ctx *gethtracing.VMContext) {
	//log.Info("tenderly::OnSystemCallStartV2", "ctx", ctx)
}

func (t *simpleTracer) OnSystemCallEnd() {
	//log.Info("tenderly::OnSystemCallEnd")
}

func (t *simpleTracer) OnTxStart(ctx *gethtracing.VMContext, tx *gethtypes.Transaction, from gethcommon.Address) {
	//log.Info("tenderly::OnTxStart", "ctx", ctx, "tx", tx, "from", from)
	t.tx = tx
}

func (t *simpleTracer) OnTxEnd(receipt *gethtypes.Receipt, err error) {
	//log.Info("tenderly::OnTxEnd", "receipt", receipt, "err", err)
}

func (t *simpleTracer) OnEnter(_ int, typ byte, from gethcommon.Address, to gethcommon.Address, input []byte, gas uint64, value *big.Int) {
	//log.Info("tenderly::OnEnter", "typ", typ, "from", from, "to", to, "input", input, "gas", gas, "value", value)
}

func (t *simpleTracer) OnExit(_ int, output []byte, used uint64, err error, reverted bool) {
	//log.Info("tenderly::OnExit", "output", output, "used", used, "err", err, "reverted", reverted)
}

func (t *simpleTracer) OnOpcode(pc uint64, typ byte, gas, cost uint64, scope gethtracing.OpContext, rData []byte, depth int, err error) {
	//log.Info("tenderly::OnOpcode", "pc", pc, "typ", typ, "gas", gas, "cost", cost, "scope", scope, "rData", rData, "depth", depth, "err", err)
}

func (t *simpleTracer) OnBalanceChange(addr gethcommon.Address, prev *big.Int, n *big.Int, reason gethtracing.BalanceChangeReason) {
	//log.Info("tenderly::OnBalanceChange", "addr", addr, "prev", prev, "n", n, "reason", reason)
}

func (t *simpleTracer) OnNonceChange(addr gethcommon.Address, prev, new uint64) {
	//log.Info("tenderly::OnNonceChange", "addr", addr, "prev", prev, "new", new)
}

func (t *simpleTracer) OnCodeChange(addr gethcommon.Address, prevCodeHash gethcommon.Hash, prevCode []byte, codeHash gethcommon.Hash, code []byte) {
	//log.Info("tenderly::OnCodeChange", "addr", addr, "prevCodeHash", prevCodeHash, "prevCode", prevCode, "codeHash", codeHash, "code", code)
}

func (t *simpleTracer) OnStorageChange(addr gethcommon.Address, slot gethcommon.Hash, prev, new gethcommon.Hash) {
	//log.Info("tenderly::OnStorageChange", "addr", addr, "slot", slot, "prev", prev, "new", new)
}

func (t *simpleTracer) OnBalanceRead(addr gethcommon.Address, bal *big.Int) {
	//log.Info("tenderly::OnBalanceRead", "addr", addr, "bal", bal)
}

func (t *simpleTracer) OnNonceRead(addr gethcommon.Address, nonce uint64) {
	//log.Info("tenderly::OnNonceRead", "addr", addr, "nonce", nonce)
}

func (t *simpleTracer) OnCodeRead(addr gethcommon.Address, code []byte) {
	//log.Info("tenderly::OnCodeRead", "addr", addr, "code", code)
}

func (t *simpleTracer) OnCodeSizeRead(addr gethcommon.Address, size int) {
	//log.Info("tenderly::OnCodeSizeRead", "addr", addr, "size", size)
}

func (t *simpleTracer) OnCodeHashRead(addr gethcommon.Address, hash gethcommon.Hash) {
	//log.Info("tenderly::OnCodeHashRead", "addr", addr, "hash", hash)
}

func (t *simpleTracer) OnStorageRead(addr gethcommon.Address, slot gethcommon.Hash, value gethcommon.Hash) {
	//log.Info("tenderly::OnStorageRead", "addr", addr, "slot", slot, "value", value)
}

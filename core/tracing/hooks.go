// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package tracing defines hooks for 'live tracing' of block processing and transaction
// execution. Here we define the low-level [Hooks] object that carries hooks which are
// invoked by the go-ethereum core at various points in the state transition.
//
// To create a tracer that can be invoked with Geth, you need to register it using
// [github.com/ethereum/go-ethereum/eth/tracers.LiveDirectory.Register].
//
// See https://geth.ethereum.org/docs/developers/evm-tracing/live-tracing for a tutorial.
package tracing

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// OpContext provides the context at which the opcode is being
// executed in, including the memory, stack and various contract-level information.
type OpContext interface {
	MemoryData() []byte
	StackData() []uint256.Int
	Caller() common.Address
	Address() common.Address
	CallValue() *uint256.Int
	CallInput() []byte
	ContractCode() []byte
}

// StateDB gives tracers access to the whole state.
type StateDB interface {
	GetBalance(common.Address) *uint256.Int
	GetNonce(common.Address) uint64
	GetCode(common.Address) []byte
	GetCodeHash(common.Address) common.Hash
	GetState(common.Address, common.Hash) common.Hash
	GetTransientState(common.Address, common.Hash) common.Hash
	Exist(common.Address) bool
	GetRefund() uint64
	GetAccessList() (addresses map[common.Address]int, slots []map[common.Hash]struct{})
}

// VMContext provides the context for the EVM execution.
type VMContext struct {
	Coinbase    common.Address
	BlockNumber *big.Int
	Time        uint64
	Random      *common.Hash
	BaseFee     *big.Int
	StateDB     StateDB

	// Arbitrum information
	ArbOSVersion uint64
}

// BlockEvent is emitted upon tracing an incoming block.
// It contains the block as well as consensus related information.
type BlockEvent struct {
	Block     *types.Block
	Finalized *types.Header
	Safe      *types.Header
}

type (
	/*
		- VM events -
	*/

	// TxStartHook is called before the execution of a transaction starts.
	// Call simulations don't come with a valid signature. `from` field
	// to be used for address of the caller.
	TxStartHook = func(vm *VMContext, tx *types.Transaction, from common.Address)

	// TxEndHook is called after the execution of a transaction ends.
	TxEndHook = func(receipt *types.Receipt, err error)

	// EnterHook is invoked when the processing of a message starts.
	//
	// Take note that EnterHook, when in the context of a live tracer, can be invoked
	// outside of the `OnTxStart` and `OnTxEnd` hooks when dealing with system calls,
	// see [OnSystemCallStartHook] and [OnSystemCallEndHook] for more information.
	EnterHook = func(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int)

	// ExitHook is invoked when the processing of a message ends.
	// `revert` is true when there was an error during the execution.
	// Exceptionally, before the homestead hardfork a contract creation that
	// ran out of gas when attempting to persist the code to database did not
	// count as a call failure and did not cause a revert of the call. This will
	// be indicated by `reverted == false` and `err == ErrCodeStoreOutOfGas`.
	//
	// Take note that ExitHook, when in the context of a live tracer, can be invoked
	// outside of the `OnTxStart` and `OnTxEnd` hooks when dealing with system calls,
	// see [OnSystemCallStartHook] and [OnSystemCallEndHook] for more information.
	ExitHook = func(depth int, output []byte, gasUsed uint64, err error, reverted bool)

	// OpcodeHook is invoked just prior to the execution of an opcode.
	OpcodeHook = func(pc uint64, op byte, gas, cost uint64, scope OpContext, rData []byte, depth int, err error)

	// FaultHook is invoked when an error occurs during the execution of an opcode.
	FaultHook = func(pc uint64, op byte, gas, cost uint64, scope OpContext, depth int, err error)

	// GasChangeHook is invoked when the gas changes.
	GasChangeHook = func(old, new uint64, reason GasChangeReason)

	/*
		- Chain events -
	*/

	// BlockchainInitHook is called when the blockchain is initialized.
	BlockchainInitHook = func(chainConfig *params.ChainConfig)

	// CloseHook is called when the blockchain closes.
	CloseHook = func()

	// BlockStartHook is called before executing `block`.
	// `td` is the total difficulty prior to `block`.
	BlockStartHook = func(event BlockEvent)

	// BlockEndHook is called after executing a block.
	BlockEndHook = func(err error)

	// BlockEndMetricsHook is called after executing a block and calculating the metrics timers
	BlockEndMetricsHook = func(blockNumber uint64, blockInsertDuration time.Duration)

	// SkippedBlockHook indicates a block was skipped during processing
	// due to it being known previously. This can happen e.g. when recovering
	// from a crash.
	SkippedBlockHook = func(event BlockEvent)

	// GenesisBlockHook is called when the genesis block is being processed.
	GenesisBlockHook = func(genesis *types.Block, alloc types.GenesisAlloc)

	// OnSystemCallStartHook is called when a system call is about to be executed. Today,
	// this hook is invoked when the EIP-4788 system call is about to be executed to set the
	// beacon block root.
	//
	// After this hook, the EVM call tracing will happened as usual so you will receive a `OnEnter/OnExit`
	// as well as state hooks between this hook and the `OnSystemCallEndHook`.
	//
	// Note that system call happens outside normal transaction execution, so the `OnTxStart/OnTxEnd` hooks
	// will not be invoked.
	OnSystemCallStartHook = func()

	// OnSystemCallStartHookV2 is called when a system call is about to be executed. Refer
	// to `OnSystemCallStartHook` for more information.
	OnSystemCallStartHookV2 = func(vm *VMContext)

	// OnSystemCallEndHook is called when a system call has finished executing. Today,
	// this hook is invoked when the EIP-4788 system call is about to be executed to set the
	// beacon block root.
	OnSystemCallEndHook = func()

	/*
		- State events -
	*/

	// BalanceChangeHook is called when the balance of an account changes.
	BalanceChangeHook = func(addr common.Address, prev, new *big.Int, reason BalanceChangeReason)

	// NonceChangeHook is called when the nonce of an account changes.
	NonceChangeHook = func(addr common.Address, prev, new uint64)

	// NonceChangeHookV2 is called when the nonce of an account changes.
	NonceChangeHookV2 = func(addr common.Address, prev, new uint64, reason NonceChangeReason)

	// CodeChangeHook is called when the code of an account changes.
	CodeChangeHook = func(addr common.Address, prevCodeHash common.Hash, prevCode []byte, codeHash common.Hash, code []byte)

	// StorageChangeHook is called when the storage of an account changes.
	StorageChangeHook = func(addr common.Address, slot common.Hash, prev, new common.Hash)

	// LogHook is called when a log is emitted.
	LogHook = func(log *types.Log)

	// BlockHashReadHook is called when EVM reads the blockhash of a block.
	BlockHashReadHook = func(blockNumber uint64, hash common.Hash)

	CaptureArbitrumTransferHook   = func(from, to *common.Address, value *big.Int, before bool, reason BalanceChangeReason)
	CaptureArbitrumStorageGetHook = func(key common.Hash, depth int, before bool)
	CaptureArbitrumStorageSetHook = func(key, value common.Hash, depth int, before bool)

	CaptureStylusHostioHook = func(name string, args, outs []byte, startInk, endInk uint64)
)

type Hooks struct {
	// VM events
	OnTxStart   TxStartHook
	OnTxEnd     TxEndHook
	OnEnter     EnterHook
	OnExit      ExitHook
	OnOpcode    OpcodeHook
	OnFault     FaultHook
	OnGasChange GasChangeHook
	// Chain events
	OnBlockchainInit    BlockchainInitHook
	OnClose             CloseHook
	OnBlockStart        BlockStartHook
	OnBlockEnd          BlockEndHook
	OnBlockEndMetrics   BlockEndMetricsHook
	OnSkippedBlock      SkippedBlockHook
	OnGenesisBlock      GenesisBlockHook
	OnSystemCallStart   OnSystemCallStartHook
	OnSystemCallStartV2 OnSystemCallStartHookV2
	OnSystemCallEnd     OnSystemCallEndHook
	// State events
	OnBalanceChange BalanceChangeHook
	OnNonceChange   NonceChangeHook
	OnNonceChangeV2 NonceChangeHookV2
	OnCodeChange    CodeChangeHook
	OnStorageChange StorageChangeHook
	OnLog           LogHook
	// Block hash read
	OnBlockHashRead BlockHashReadHook

	// Arbitrum: capture a transfer, mint, or burn that happens outside of EVM execution
	CaptureArbitrumTransfer   CaptureArbitrumTransferHook
	CaptureArbitrumStorageGet CaptureArbitrumStorageGetHook
	CaptureArbitrumStorageSet CaptureArbitrumStorageSetHook
	// Stylus: capture hostio invocation
	CaptureStylusHostio CaptureStylusHostioHook
}

// BalanceChangeReason is used to indicate the reason for a balance change, useful
// for tracing and reporting.
type BalanceChangeReason byte

//go:generate go run golang.org/x/tools/cmd/stringer -type=BalanceChangeReason -trimprefix=BalanceChange -output gen_balance_change_reason_stringer.go

const (
	BalanceChangeUnspecified BalanceChangeReason = 0

	// Issuance
	// BalanceIncreaseRewardMineUncle is a reward for mining an uncle block.
	BalanceIncreaseRewardMineUncle BalanceChangeReason = 1
	// BalanceIncreaseRewardMineBlock is a reward for mining a block.
	BalanceIncreaseRewardMineBlock BalanceChangeReason = 2
	// BalanceIncreaseWithdrawal is ether withdrawn from the beacon chain.
	BalanceIncreaseWithdrawal BalanceChangeReason = 3
	// BalanceIncreaseGenesisBalance is ether allocated at the genesis block.
	BalanceIncreaseGenesisBalance BalanceChangeReason = 4

	// Transaction fees
	// BalanceIncreaseRewardTransactionFee is the transaction tip increasing block builder's balance.
	BalanceIncreaseRewardTransactionFee BalanceChangeReason = 5
	// BalanceDecreaseGasBuy is spent to purchase gas for execution a transaction.
	// Part of this gas will be burnt as per EIP-1559 rules.
	BalanceDecreaseGasBuy BalanceChangeReason = 6
	// BalanceIncreaseGasReturn is ether returned for unused gas at the end of execution.
	BalanceIncreaseGasReturn BalanceChangeReason = 7

	// DAO fork
	// BalanceIncreaseDaoContract is ether sent to the DAO refund contract.
	BalanceIncreaseDaoContract BalanceChangeReason = 8
	// BalanceDecreaseDaoAccount is ether taken from a DAO account to be moved to the refund contract.
	BalanceDecreaseDaoAccount BalanceChangeReason = 9

	// BalanceChangeTransfer is ether transferred via a call.
	// it is a decrease for the sender and an increase for the recipient.
	BalanceChangeTransfer BalanceChangeReason = 10
	// BalanceChangeTouchAccount is a transfer of zero value. It is only there to
	// touch-create an account.
	BalanceChangeTouchAccount BalanceChangeReason = 11

	// BalanceIncreaseSelfdestruct is added to the recipient as indicated by a selfdestructing account.
	BalanceIncreaseSelfdestruct BalanceChangeReason = 12
	// BalanceDecreaseSelfdestruct is deducted from a contract due to self-destruct.
	BalanceDecreaseSelfdestruct BalanceChangeReason = 13
	// BalanceDecreaseSelfdestructBurn is ether that is sent to an already self-destructed
	// account within the same tx (captured at end of tx).
	// Note it doesn't account for a self-destruct which appoints itself as recipient.
	BalanceDecreaseSelfdestructBurn BalanceChangeReason = 14

	// BalanceChangeRevert is emitted when the balance is reverted back to a previous value due to call failure.
	// It is only emitted when the tracer has opted in to use the journaling wrapper (WrapWithJournal).
	BalanceChangeRevert BalanceChangeReason = 15
)

// Arbitrum specific
const (
	BalanceChangeDuringEVMExecution BalanceChangeReason = 128 + iota
	BalanceIncreaseDeposit
	BalanceDecreaseWithdrawToL1
	BalanceIncreaseL1PosterFee
	BalanceIncreaseInfraFee
	BalanceIncreaseNetworkFee
	BalanceChangeTransferInfraRefund
	BalanceChangeTransferNetworkRefund
	BalanceIncreasePrepaid
	BalanceDecreaseUndoRefund
	BalanceChangeEscrowTransfer
	BalanceChangeTransferBatchposterReward
	BalanceChangeTransferBatchposterRefund
	BalanceChangeTransferRetryableExcessRefund
	// Stylus
	BalanceChangeTransferActivationFee
	BalanceChangeTransferActivationReimburse
	// Native token minting and burning
	BalanceIncreaseMintNativeToken
	BalanceDecreaseBurnNativeToken
)

// Str gives the arbitrum specific string for the corresponding BalanceChangeReason
func (b BalanceChangeReason) Str() string {
	switch b {
	case BalanceIncreaseRewardTransactionFee:
		return "tip"
	case BalanceDecreaseGasBuy:
		return "feePayment"
	case BalanceIncreaseGasReturn:
		return "gasRefund"
	case BalanceChangeTransfer:
		return "transfer via a call"
	case BalanceDecreaseSelfdestruct:
		return "selfDestruct"
	case BalanceChangeDuringEVMExecution:
		return "during evm execution"
	case BalanceIncreaseDeposit:
		return "deposit"
	case BalanceDecreaseWithdrawToL1:
		return "withdraw"
	case BalanceIncreaseL1PosterFee, BalanceIncreaseInfraFee, BalanceIncreaseNetworkFee:
		return "feeCollection"
	case BalanceIncreasePrepaid:
		return "prepaid"
	case BalanceDecreaseUndoRefund:
		return "undoRefund"
	case BalanceChangeEscrowTransfer:
		return "escrow"
	case BalanceChangeTransferInfraRefund, BalanceChangeTransferNetworkRefund, BalanceChangeTransferRetryableExcessRefund:
		return "refund"
	// Batchposter
	case BalanceChangeTransferBatchposterReward:
		return "batchPosterReward"
	case BalanceChangeTransferBatchposterRefund:
		return "batchPosterRefund"
	// Stylus
	case BalanceChangeTransferActivationFee:
		return "activate"
	case BalanceChangeTransferActivationReimburse:
		return "reimburse"
	default:
		return "unspecified"
	}
}

// GasChangeReason is used to indicate the reason for a gas change, useful
// for tracing and reporting.
//
// There is essentially two types of gas changes, those that can be emitted once per transaction
// and those that can be emitted on a call basis, so possibly multiple times per transaction.
//
// They can be recognized easily by their name, those that start with `GasChangeTx` are emitted
// once per transaction, while those that start with `GasChangeCall` are emitted on a call basis.
type GasChangeReason byte

//go:generate go run golang.org/x/tools/cmd/stringer -type=GasChangeReason -trimprefix=GasChange -output gen_gas_change_reason_stringer.go

const (
	GasChangeUnspecified GasChangeReason = 0

	// GasChangeTxInitialBalance is the initial balance for the call which will be equal to the gasLimit of the call. There is only
	// one such gas change per transaction.
	GasChangeTxInitialBalance GasChangeReason = 1
	// GasChangeTxIntrinsicGas is the amount of gas that will be charged for the intrinsic cost of the transaction, there is
	// always exactly one of those per transaction.
	GasChangeTxIntrinsicGas GasChangeReason = 2
	// GasChangeTxRefunds is the sum of all refunds which happened during the tx execution (e.g. storage slot being cleared)
	// this generates an increase in gas. There is at most one of such gas change per transaction.
	GasChangeTxRefunds GasChangeReason = 3
	// GasChangeTxLeftOverReturned is the amount of gas left over at the end of transaction's execution that will be returned
	// to the chain. This change will always be a negative change as we "drain" left over gas towards 0. If there was no gas
	// left at the end of execution, no such even will be emitted. The returned gas's value in Wei is returned to caller.
	// There is at most one of such gas change per transaction.
	GasChangeTxLeftOverReturned GasChangeReason = 4

	// GasChangeCallInitialBalance is the initial balance for the call which will be equal to the gasLimit of the call. There is only
	// one such gas change per call.
	GasChangeCallInitialBalance GasChangeReason = 5
	// GasChangeCallLeftOverReturned is the amount of gas left over that will be returned to the caller, this change will always
	// be a negative change as we "drain" left over gas towards 0. If there was no gas left at the end of execution, no such even
	// will be emitted.
	GasChangeCallLeftOverReturned GasChangeReason = 6
	// GasChangeCallLeftOverRefunded is the amount of gas that will be refunded to the call after the child call execution it
	// executed completed. This value is always positive as we are giving gas back to the you, the left over gas of the child.
	// If there was no gas left to be refunded, no such even will be emitted.
	GasChangeCallLeftOverRefunded GasChangeReason = 7
	// GasChangeCallContractCreation is the amount of gas that will be burned for a CREATE.
	GasChangeCallContractCreation GasChangeReason = 8
	// GasChangeCallContractCreation2 is the amount of gas that will be burned for a CREATE2.
	GasChangeCallContractCreation2 GasChangeReason = 9
	// GasChangeCallCodeStorage is the amount of gas that will be charged for code storage.
	GasChangeCallCodeStorage GasChangeReason = 10
	// GasChangeCallOpCode is the amount of gas that will be charged for an opcode executed by the EVM, exact opcode that was
	// performed can be check by `OnOpcode` handling.
	GasChangeCallOpCode GasChangeReason = 11
	// GasChangeCallPrecompiledContract is the amount of gas that will be charged for a precompiled contract execution.
	GasChangeCallPrecompiledContract GasChangeReason = 12
	// GasChangeCallStorageColdAccess is the amount of gas that will be charged for a cold storage access as controlled by EIP2929 rules.
	GasChangeCallStorageColdAccess GasChangeReason = 13
	// GasChangeCallFailedExecution is the burning of the remaining gas when the execution failed without a revert.
	GasChangeCallFailedExecution GasChangeReason = 14
	// GasChangeWitnessContractInit flags the event of adding to the witness during the contract creation initialization step.
	GasChangeWitnessContractInit GasChangeReason = 15
	// GasChangeWitnessContractCreation flags the event of adding to the witness during the contract creation finalization step.
	GasChangeWitnessContractCreation GasChangeReason = 16
	// GasChangeWitnessCodeChunk flags the event of adding one or more contract code chunks to the witness.
	GasChangeWitnessCodeChunk GasChangeReason = 17
	// GasChangeWitnessContractCollisionCheck flags the event of adding to the witness when checking for contract address collision.
	GasChangeWitnessContractCollisionCheck GasChangeReason = 18
	// GasChangeTxDataFloor is the amount of extra gas the transaction has to pay to reach the minimum gas requirement for the
	// transaction data. This change will always be a negative change.
	GasChangeTxDataFloor GasChangeReason = 19

	// GasChangeIgnored is a special value that can be used to indicate that the gas change should be ignored as
	// it will be "manually" tracked by a direct emit of the gas change event.
	GasChangeIgnored GasChangeReason = 0xFF
)

// NonceChangeReason is used to indicate the reason for a nonce change.
type NonceChangeReason byte

//go:generate go run golang.org/x/tools/cmd/stringer -type=NonceChangeReason -trimprefix NonceChange -output gen_nonce_change_reason_stringer.go

const (
	NonceChangeUnspecified NonceChangeReason = 0

	// NonceChangeGenesis is the nonce allocated to accounts at genesis.
	NonceChangeGenesis NonceChangeReason = 1

	// NonceChangeEoACall is the nonce change due to an EoA call.
	NonceChangeEoACall NonceChangeReason = 2

	// NonceChangeContractCreator is the nonce change of an account creating a contract.
	NonceChangeContractCreator NonceChangeReason = 3

	// NonceChangeNewContract is the nonce change of a newly created contract.
	NonceChangeNewContract NonceChangeReason = 4

	// NonceChangeTransaction is the nonce change due to a EIP-7702 authorization.
	NonceChangeAuthorization NonceChangeReason = 5

	// NonceChangeRevert is emitted when the nonce is reverted back to a previous value due to call failure.
	// It is only emitted when the tracer has opted in to use the journaling wrapper (WrapWithJournal).
	NonceChangeRevert NonceChangeReason = 6
)

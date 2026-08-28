// Copyright 2014 The go-ethereum Authors
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

package core

import (
	"bytes"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// ExecutionResult includes all output after executing given evm
// message no matter the execution itself is successful or not.
type ExecutionResult struct {
	UsedGas    uint64 // Total used gas, refunded gas is deducted
	MaxUsedGas uint64 // Maximum gas consumed during execution, excluding gas refunds.
	Err        error  // Any error encountered during the execution(listed in core/vm/errors.go)
	ReturnData []byte // Returned data from evm(function result or data supplied with revert opcode)

	// Arbitrum: a tx may yield others that need to run afterward (see retryables)
	ScheduledTxes types.Transactions
	// Arbitrum: the contract deployed from the top-level transaction, or nil if not a contract creation tx
	TopLevelDeployed *common.Address
	// Arbitrum: total used multi-dimensional gas
	UsedMultiGas multigas.MultiGas
}

// Unwrap returns the internal evm error which allows us for further
// analysis outside.
func (result *ExecutionResult) Unwrap() error {
	return result.Err
}

// Failed returns the indicator whether the execution is successful or not
func (result *ExecutionResult) Failed() bool { return result.Err != nil }

// Return is a helper function to help caller distinguish between revert reason
// and function return. Return returns the data after execution if no error occurs.
func (result *ExecutionResult) Return() []byte {
	if result.Err != nil {
		return nil
	}
	return common.CopyBytes(result.ReturnData)
}

// Revert returns the concrete revert reason if the execution is aborted by `REVERT`
// opcode. Note the reason can be nil if no data supplied with revert opcode.
func (result *ExecutionResult) Revert() []byte {
	if result.Err != vm.ErrExecutionReverted {
		return nil
	}
	return common.CopyBytes(result.ReturnData)
}

// ErrFilteredOnChain is returned when a transaction was found in the onchain filter
// and executed as a no-op (nonce incremented, all gas consumed). It wraps
// ErrExecutionReverted so receipt status checks work correctly.
type ErrFilteredOnChain struct {
	TxHash common.Hash
}

func (e *ErrFilteredOnChain) Error() string {
	return state.ChainPolicyRejectionMessage
}

func (e *ErrFilteredOnChain) Unwrap() error {
	return vm.ErrExecutionReverted
}

// IntrinsicGas computes the 'intrinsic gas' for a message with the given data.
func IntrinsicGas(data []byte, accessList types.AccessList, authList []types.SetCodeAuthorization, isContractCreation bool, rules params.Rules, costPerStateByte uint64) (vm.GasCosts, error) {
	multiGas, err := IntrinsicMultiGas(data, accessList, authList, isContractCreation, rules, costPerStateByte)
	if err != nil {
		return vm.GasCosts{}, err
	}
	return intrinsicGasCosts(multiGas, rules.IsAmsterdam), nil
}

// Arbitrum: intrinsicGasCosts splits an intrinsic multigas into the EIP-8037
// two-dimensional vector. Under Amsterdam every state-gas component of the
// intrinsic cost is booked to ResourceKindStorageGrowth and nothing else is,
// so that dimension is exactly the state gas.
func intrinsicGasCosts(multiGas multigas.MultiGas, isAmsterdam bool) vm.GasCosts {
	if isAmsterdam {
		stateGas := multiGas.Get(multigas.ResourceKindStorageGrowth)
		return vm.GasCosts{RegularGas: multiGas.SingleGas() - stateGas, StateGas: stateGas}
	}
	return vm.GasCosts{RegularGas: multiGas.SingleGas()}
}

// IntrinsicMultiGas returns the intrinsic gas as a multi-gas.
func IntrinsicMultiGas(data []byte, accessList types.AccessList, authList []types.SetCodeAuthorization, isContractCreation bool, rules params.Rules, costPerStateByte uint64) (multigas.MultiGas, error) {
	// Set the starting gas for the raw transaction
	var gas multigas.MultiGas
	if isContractCreation && rules.IsHomestead {
		if rules.IsAmsterdam {
			gas.SaturatingIncrementInto(multigas.ResourceKindComputation, params.TxGas+params.CreateGasAmsterdam)
			gas.SaturatingIncrementInto(multigas.ResourceKindStorageGrowth, params.AccountCreationSize*costPerStateByte)
		} else {
			gas.SaturatingIncrementInto(multigas.ResourceKindComputation, params.TxGasContractCreation)
		}
	} else {
		gas.SaturatingIncrementInto(multigas.ResourceKindComputation, params.TxGas)
	}
	// Add gas for authorizations
	if authList != nil {
		if rules.IsAmsterdam {
			gas.SaturatingIncrementInto(multigas.ResourceKindComputation, uint64(len(authList))*params.TxAuthTupleRegularGas)
			gas.SaturatingIncrementInto(multigas.ResourceKindStorageGrowth, uint64(len(authList))*(params.AuthorizationCreationSize+params.AccountCreationSize)*costPerStateByte)
		} else {
			gas.SaturatingIncrementInto(multigas.ResourceKindStorageGrowth, uint64(len(authList))*params.CallNewAccountGas)
		}
	}
	dataLen := uint64(len(data))
	// Bump the required gas by the amount of transactional data
	if dataLen > 0 {
		// Zero and non-zero bytes are priced differently
		z := uint64(bytes.Count(data, []byte{0}))
		nz := dataLen - z

		// Make sure we don't exceed uint64 for all data combinations
		nonZeroGas := params.TxDataNonZeroGasFrontier
		if rules.IsIstanbul {
			nonZeroGas = params.TxDataNonZeroGasEIP2028
		}
		if (math.MaxUint64-gas.SingleGas())/nonZeroGas < nz {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		gas.SaturatingIncrementInto(multigas.ResourceKindL2Calldata, nz*nonZeroGas)

		if (math.MaxUint64-gas.SingleGas())/params.TxDataZeroGas < z {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		gas.SaturatingIncrementInto(multigas.ResourceKindL2Calldata, z*params.TxDataZeroGas)

		if isContractCreation && rules.IsShanghai {
			lenWords := toWordSize(dataLen)
			if (math.MaxUint64-gas.SingleGas())/params.InitCodeWordGas < lenWords {
				return multigas.ZeroGas(), ErrGasUintOverflow
			}
			gas.SaturatingIncrementInto(multigas.ResourceKindComputation, lenWords*params.InitCodeWordGas)
		}
	}
	if accessList != nil {
		// Access lists (EIP-2930) pre-warm addresses and storage keys so that subsequent reads
		// are cheaper. This is a pre-paid read access fee — no persistent state is modified.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		addresses := uint64(len(accessList))
		storageKeys := uint64(accessList.StorageKeys())
		if (math.MaxUint64-gas.SingleGas())/params.TxAccessListAddressGas < addresses {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		gas.SaturatingIncrementInto(multigas.ResourceKindStorageAccessRead, addresses*params.TxAccessListAddressGas)
		if (math.MaxUint64-gas.SingleGas())/params.TxAccessListStorageKeyGas < storageKeys {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		gas.SaturatingIncrementInto(multigas.ResourceKindStorageAccessRead, storageKeys*params.TxAccessListStorageKeyGas)

		// EIP-7981: access list data is charged in addition to the base charge.
		if rules.IsAmsterdam {
			const (
				addressCost    = common.AddressLength * params.TxCostFloorPerToken7976 * params.TxTokenPerNonZeroByte
				storageKeyCost = common.HashLength * params.TxCostFloorPerToken7976 * params.TxTokenPerNonZeroByte
			)
			if (math.MaxUint64-gas.SingleGas())/addressCost < addresses {
				return multigas.ZeroGas(), ErrGasUintOverflow
			}
			gas.SaturatingIncrementInto(multigas.ResourceKindStorageAccessRead, addresses*addressCost)
			if (math.MaxUint64-gas.SingleGas())/storageKeyCost < storageKeys {
				return multigas.ZeroGas(), ErrGasUintOverflow
			}
			gas.SaturatingIncrementInto(multigas.ResourceKindStorageAccessRead, storageKeys*storageKeyCost)
		}
	}
	return gas, nil
}

// FloorDataGas computes the minimum gas required for a transaction based on its data tokens (EIP-7623).
func FloorDataGas(rules params.Rules, data []byte, accessList types.AccessList) (uint64, error) {
	var (
		tokens    uint64
		tokenCost uint64
	)
	if rules.IsAmsterdam {
		// EIP-7976 changes how calldata is priced.
		// From 10/40 to 64/64 for zero/non-zero bytes.
		tokenCost = params.TxCostFloorPerToken7976
		dataLen := uint64(len(data))
		if math.MaxUint64/params.TxTokenPerNonZeroByte < dataLen {
			return 0, ErrGasUintOverflow
		}
		tokens = dataLen * params.TxTokenPerNonZeroByte

		// EIP-7981 adds additional tokens for every entry in the accesslist
		const addressTokenCost = uint64(common.AddressLength) * params.TxTokenPerNonZeroByte
		addresses := uint64(len(accessList))
		if (math.MaxUint64-tokens)/addressTokenCost < addresses {
			return 0, ErrGasUintOverflow
		}
		tokens += addresses * addressTokenCost

		const storageKeyTokenCost = uint64(common.HashLength) * params.TxTokenPerNonZeroByte
		storageKeys := uint64(accessList.StorageKeys())
		if (math.MaxUint64-tokens)/storageKeyTokenCost < storageKeys {
			return 0, ErrGasUintOverflow
		}
		tokens += storageKeys * storageKeyTokenCost
	} else {
		var (
			z  = uint64(bytes.Count(data, []byte{0}))
			nz = uint64(len(data)) - z
		)
		// Pre-Amsterdam
		if math.MaxUint64/params.TxTokenPerNonZeroByte < nz {
			return 0, ErrGasUintOverflow
		}
		tokens = nz * params.TxTokenPerNonZeroByte
		if math.MaxUint64-tokens < z {
			return 0, ErrGasUintOverflow
		}
		tokens += z
		tokenCost = params.TxCostFloorPerToken
	}

	// Check for overflow
	if (math.MaxUint64-params.TxGas)/tokenCost < tokens {
		return 0, ErrGasUintOverflow
	}
	// Minimum gas required for a transaction based on its data tokens (EIP-7623).
	return params.TxGas + tokens*tokenCost, nil
}

// toWordSize returns the ceiled word size required for init code payment calculation.
func toWordSize(size uint64) uint64 {
	if size > math.MaxUint64-31 {
		return math.MaxUint64/32 + 1
	}

	return (size + 31) / 32
}

// A Message contains the data derived from a single transaction that is relevant to state
// processing.
type Message struct {
	// Arbitrum-specific
	TxRunContext *MessageRunContext
	Tx           *types.Transaction

	To                    *common.Address
	From                  common.Address
	Nonce                 uint64
	Value                 *uint256.Int
	GasLimit              uint64
	GasPrice              *uint256.Int
	GasFeeCap             *uint256.Int
	GasTipCap             *uint256.Int
	Data                  []byte
	AccessList            types.AccessList
	BlobGasFeeCap         *uint256.Int
	BlobHashes            []common.Hash
	SetCodeAuthorizations []types.SetCodeAuthorization

	// When SkipNonceChecks is true, the message nonce is not checked against the
	// account nonce in state.
	//
	// This field will be set to true for operations like RPC eth_call
	// or the state prefetching.
	SkipNonceChecks bool

	// When set, the message is not treated as a transaction, and certain
	// transaction-specific checks are skipped:
	//
	// - From is not verified to be an EOA
	// - GasLimit is not checked against the protocol defined tx gaslimit
	SkipTransactionChecks bool
	// L1 charging is disabled when SkipL1Charging is true.
	// This field might be set to true for operations like RPC eth_call.
	SkipL1Charging bool
}

type messageRunMode uint8

const (
	messageCommitMode messageRunMode = iota
	messageGasEstimationMode
	messageEthcallMode
	messageReplayMode
	messageRecordingMode
	messageSequencingMode
	messageDelayedSequencingMode
)

type MessageRunContext struct {
	runMode messageRunMode

	wasmCacheTag uint32
	wasmTargets  []rawdb.WasmTarget

	tipRecording bool
}

func NewMessageSequencingContext(wasmTargets []rawdb.WasmTarget) *MessageRunContext {
	messageSequencingCtx := NewMessageCommitContext(wasmTargets)
	messageSequencingCtx.runMode = messageSequencingMode
	return messageSequencingCtx
}

// NewMessageDelayedSequencingContext is used when the sequencer builds a block
// from a finalized L1 delayed-inbox message. Delayed messages cannot be
// filtered post-commitment (censorship resistance), so consumers that gate
// FilterTx on "sequencer is actively building a block" should use IsSequencing
// (which returns false here) rather than IsCommitMode.
func NewMessageDelayedSequencingContext(wasmTargets []rawdb.WasmTarget) *MessageRunContext {
	ctx := NewMessageCommitContext(wasmTargets)
	ctx.runMode = messageDelayedSequencingMode
	return ctx
}

func NewMessageCommitContext(wasmTargets []rawdb.WasmTarget) *MessageRunContext {
	if len(wasmTargets) == 0 {
		wasmTargets = []rawdb.WasmTarget{rawdb.LocalTarget()}
	}
	return &MessageRunContext{
		runMode:      messageCommitMode,
		wasmCacheTag: 1,
		wasmTargets:  wasmTargets,
	}
}

func NewMessageReplayContext() *MessageRunContext {
	return &MessageRunContext{
		runMode:     messageReplayMode,
		wasmTargets: []rawdb.WasmTarget{rawdb.LocalTarget()},
	}
}

func NewMessageRecordingContext(wasmTargets []rawdb.WasmTarget) *MessageRunContext {
	if len(wasmTargets) == 0 {
		wasmTargets = []rawdb.WasmTarget{rawdb.LocalTarget()}
	}
	return &MessageRunContext{
		runMode:     messageRecordingMode,
		wasmTargets: wasmTargets,
	}
}

func NewTipRecordingContext(ctx *MessageRunContext) *MessageRunContext {
	tipRecordingCtx := *ctx
	tipRecordingCtx.tipRecording = true
	return &tipRecordingCtx
}

func NewMessagePrefetchContext() *MessageRunContext {
	return NewMessageReplayContext()
}

func NewMessageEthcallContext() *MessageRunContext {
	return &MessageRunContext{
		runMode:     messageEthcallMode,
		wasmTargets: []rawdb.WasmTarget{rawdb.LocalTarget()},
	}
}

func NewMessageGasEstimationContext() *MessageRunContext {
	return &MessageRunContext{
		runMode:     messageGasEstimationMode,
		wasmTargets: []rawdb.WasmTarget{rawdb.LocalTarget()},
	}
}

// IsSequencing returns true only when the sequencer is actively building a
// block from a directly-received (not-yet-batch-posted) L2 transaction. This
// is the predicate that gates pre-inclusion filtering (e.g. db.FilterTx).
// Delayed-inbox sequencing does NOT satisfy this — use IsDelayedSequencing
// for that case.
func (c *MessageRunContext) IsSequencing() bool {
	return c.runMode == messageSequencingMode
}

// IsDelayedSequencing returns true only when the sequencer is actively
// building a block from a delayed-inbox message. These must not be filtered
// (censorship resistance), so consumers of FilterTx should treat this as
// "exempt from filtering" even though the block is still being committed.
func (c *MessageRunContext) IsDelayedSequencing() bool {
	return c.runMode == messageDelayedSequencingMode
}

func (c *MessageRunContext) IsCommitMode() bool {
	return c.runMode == messageCommitMode ||
		c.runMode == messageSequencingMode ||
		c.runMode == messageDelayedSequencingMode
}

// these message modes are executed onchain so cannot make any gas shortcuts
func (c *MessageRunContext) IsExecutedOnChain() bool {
	return c.IsCommitMode() || c.runMode == messageReplayMode || c.runMode == messageRecordingMode
}

func (c *MessageRunContext) IsGasEstimation() bool {
	return c.runMode == messageGasEstimationMode
}

func (c *MessageRunContext) IsNonMutating() bool {
	return c.runMode == messageGasEstimationMode || c.runMode == messageEthcallMode
}

func (c *MessageRunContext) IsEthcall() bool {
	return c.runMode == messageEthcallMode
}

func (c *MessageRunContext) IsRecording() bool {
	return c.runMode == messageRecordingMode || c.tipRecording
}

func (c *MessageRunContext) WasmCacheTag() uint32 {
	return c.wasmCacheTag
}

func (c *MessageRunContext) WasmTargets() []rawdb.WasmTarget {
	return c.wasmTargets
}

func (c *MessageRunContext) RunModeMetricName() string {
	switch c.runMode {
	case messageSequencingMode:
		return "sequencing_runmode"
	case messageDelayedSequencingMode:
		return "delayed_sequencing_runmode"
	case messageCommitMode:
		return "commit_runmode"
	case messageGasEstimationMode:
		return "gas_estimation_runmode"
	case messageEthcallMode:
		return "eth_call_runmode"
	case messageReplayMode:
		return "replay_runmode"
	case messageRecordingMode:
		return "recording_runmode"
	default:
		return "unknown_runmode"
	}
}

// TransactionToMessage converts a transaction into a Message.
func TransactionToMessage(tx *types.Transaction, s types.Signer, baseFee *big.Int, runCtx *MessageRunContext) (*Message, error) {
	from, err := types.Sender(s, tx)
	if err != nil {
		return nil, err
	}
	gasPrice, overflow := uint256.FromBig(tx.GasPrice())
	if overflow {
		return nil, fmt.Errorf("%w: address %v, maxFeePerGas bit length: %d", ErrFeeCapVeryHigh,
			from.Hex(), tx.GasPrice().BitLen())
	}
	txGasFeeCap := tx.GasFeeCap()
	gasFeeCap, overflow := uint256.FromBig(txGasFeeCap)
	if overflow {
		return nil, fmt.Errorf("%w: address %v, maxFeePerGas bit length: %d", ErrFeeCapVeryHigh,
			from.Hex(), tx.GasFeeCap().BitLen())
	}
	txGasTipCap := tx.GasTipCap()
	gasTipCap, overflow := uint256.FromBig(txGasTipCap)
	if overflow {
		return nil, fmt.Errorf("%w: address %v, maxPriorityFeePerGas bit length: %d", ErrTipVeryHigh,
			from.Hex(), tx.GasTipCap().BitLen())
	}
	value, overflow := uint256.FromBig(tx.Value())
	if overflow {
		return nil, fmt.Errorf("value exceeds 256 bits: address %v", from.Hex())
	}
	blobGasFeeCap, overflow := uint256.FromBig(tx.BlobGasFeeCap())
	if overflow {
		return nil, fmt.Errorf("blobGasFeeCap exceeds 256 bits: address %v", from.Hex())
	}

	msg := &Message{
		TxRunContext: runCtx,

		Tx: tx,

		From:                  from,
		Nonce:                 tx.Nonce(),
		GasLimit:              tx.Gas(),
		GasPrice:              gasPrice,
		GasFeeCap:             gasFeeCap,
		GasTipCap:             gasTipCap,
		To:                    tx.To(),
		Value:                 value,
		Data:                  tx.Data(),
		AccessList:            tx.AccessList(),
		SetCodeAuthorizations: tx.SetCodeAuthorizations(),
		SkipNonceChecks:       tx.SkipNonceChecks(),       // TODO Arbitrum upstream this was init'd to false
		SkipTransactionChecks: tx.SkipTransactionChecks(), // TODO Arbitrum upstream this was init'd to false
		BlobHashes:            tx.BlobHashes(),
		BlobGasFeeCap:         blobGasFeeCap,
	}
	// If baseFee provided, set gasPrice to effectiveGasPrice.
	if baseFee != nil {
		effectiveGasPrice := new(big.Int).Add(baseFee, txGasTipCap)
		if effectiveGasPrice.Cmp(txGasFeeCap) > 0 {
			effectiveGasPrice = txGasFeeCap
		}
		// EffectiveGasPrice is already capped by txGasFeeCap, therefore
		// the overflow check is not required.
		msg.GasPrice = uint256.MustFromBig(effectiveGasPrice)
	}
	return msg, nil
}

// ApplyMessage computes the new state by applying the given message
// against the old state within the environment.
//
// ApplyMessage returns the bytes returned by any EVM execution (if it took place),
// the gas used (which includes gas refunds) and an error if it failed. An error always
// indicates a core error meaning that the message would always fail for that particular
// state and would never be accepted within a block.
func ApplyMessage(evm *vm.EVM, msg *Message, gp *GasPool) (*ExecutionResult, error) {
	// Do not panic if the gas pool is nil. This is allowed when executing
	// a single message via RPC invocation.
	if gp == nil {
		gp = NewGasPool(msg.GasLimit)
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	return newStateTransition(evm, msg, gp).execute()
}

// stateTransition represents a state transition.
//
// == The State Transitioning Model
//
// A state transition is a change made when a transaction is applied to the current world
// state. The state transitioning model does all the necessary work to work out a valid new
// state root.
//
//  1. Nonce handling
//  2. Pre pay gas
//  3. Create a new state object if the recipient is nil
//  4. Value transfer
//
// == If contract creation ==
//
//	4a. Attempt to run transaction data
//	4b. If valid, use result as code for the new state object
//
// == end ==
//
//  5. Run Script section
//  6. Derive new state root
type stateTransition struct {
	gp           *GasPool
	msg          *Message
	gasRemaining vm.GasBudget
	state        vm.StateDB
	evm          *vm.EVM
}

// newStateTransition initialises and returns a new state transition object.
func newStateTransition(evm *vm.EVM, msg *Message, gp *GasPool) *stateTransition {
	if ReadyEVMForL2 != nil {
		ReadyEVMForL2(evm, msg)
	}

	return &stateTransition{
		gp:    gp,
		evm:   evm,
		msg:   msg,
		state: evm.StateDB,
	}
}

// to returns the recipient of the message.
func (st *stateTransition) to() common.Address {
	if st.msg == nil || st.msg.To == nil /* contract creation */ {
		return common.Address{}
	}
	return *st.msg.To
}

// buyGas pre-pays gas from the sender's balance and initializes the
// transaction's gas budget. It is invoked at the tail of preCheck.
//
// The balance requirement is the worst-case ETH the tx may need to lock
// up: `msg.GasLimit × max(msg.GasPrice, msg.GasFeeCap) + msg.Value`,
// plus `blobGas × msg.BlobGasFeeCap` under Cancun. Insufficient balance
// returns ErrInsufficientFunds. After the check, the sender is actually
// debited `msg.GasLimit × msg.GasPrice` (plus `blobGas × blobBaseFee`
// under Cancun), the cap-vs-tip differential is settled at tx end.
//
// The gas budget is seeded into both `initialBudget` (frozen snapshot
// for tx-end accounting) and `gasRemaining` (live running balance):
//
//   - Pre-Amsterdam: one-dimensional regular budget equal to
//     `msg.GasLimit`; the state-gas reservoir is zero.
//   - Amsterdam+ (EIP-8037): two-dimensional budget. Regular gas is
//     capped at `MaxTxGas` (EIP-7825, 16_777_216); any excess from
//     `msg.GasLimit` above that cap becomes the state-gas reservoir.
func (st *stateTransition) buyGas() error {
	mgval := new(uint256.Int).SetUint64(st.msg.GasLimit)
	_, overflow := mgval.MulOverflow(mgval, st.msg.GasPrice)
	if overflow {
		return fmt.Errorf("%w: address %v required balance exceeds 256 bits", ErrInsufficientFunds, st.msg.From.Hex())
	}
	balanceCheck := new(uint256.Int).Set(mgval)
	if st.msg.GasFeeCap != nil {
		balanceCheck.SetUint64(st.msg.GasLimit)
		if _, overflow := balanceCheck.MulOverflow(balanceCheck, st.msg.GasFeeCap); overflow {
			return fmt.Errorf("%w: address %v required balance exceeds 256 bits", ErrInsufficientFunds, st.msg.From.Hex())
		}
	}
	if st.msg.Value != nil {
		if _, overflow := balanceCheck.AddOverflow(balanceCheck, st.msg.Value); overflow {
			return fmt.Errorf("%w: address %v required balance exceeds 256 bits", ErrInsufficientFunds, st.msg.From.Hex())
		}
	}

	if st.evm.ChainConfig().IsCancun(st.evm.Context.BlockNumber, st.evm.Context.Time, st.evm.Context.ArbOSVersion) {
		if blobGas := st.blobGasUsed(); blobGas > 0 {
			// Check that the user has enough funds to cover blobGasUsed * tx.BlobGasFeeCap
			blobBalanceCheck := new(uint256.Int).SetUint64(blobGas)
			if _, overflow := blobBalanceCheck.MulOverflow(blobBalanceCheck, st.msg.BlobGasFeeCap); overflow {
				return fmt.Errorf("%w: address %v required balance exceeds 256 bits", ErrInsufficientFunds, st.msg.From.Hex())
			}
			if _, overflow := balanceCheck.AddOverflow(balanceCheck, blobBalanceCheck); overflow {
				return fmt.Errorf("%w: address %v required balance exceeds 256 bits", ErrInsufficientFunds, st.msg.From.Hex())
			}
			// Pay for blobGasUsed * actual blob fee
			blobBaseFee, overflow := uint256.FromBig(st.evm.Context.BlobBaseFee)
			if overflow {
				return fmt.Errorf("invalid blobBaseFee: %v", st.evm.Context.BlobBaseFee)
			}
			blobFee := new(uint256.Int).SetUint64(blobGas)

			// In practice, overflow checking is unnecessary, as blobBaseFee cannot exceed
			// BlobGasFeeCap. However, in eth_call it is still possible for users to specify
			// an excessively large blob base fee and bypass the blob base fee validation.
			_, overflow = blobFee.MulOverflow(blobFee, blobBaseFee)
			if overflow {
				return fmt.Errorf("%w: address %v required balance exceeds 256 bits", ErrInsufficientFunds, st.msg.From.Hex())
			}
			_, overflow = mgval.AddOverflow(mgval, blobFee)
			if overflow {
				return fmt.Errorf("%w: address %v required balance exceeds 256 bits", ErrInsufficientFunds, st.msg.From.Hex())
			}
		}
	}
	if have, want := st.state.GetBalance(st.msg.From), balanceCheck; have.Cmp(want) < 0 {
		return fmt.Errorf("%w: address %v have %v want %v", ErrInsufficientFunds, st.msg.From.Hex(), have, want)
	}
	isAmsterdam := st.evm.ChainConfig().IsAmsterdam(st.evm.Context.BlockNumber, st.evm.Context.Time, st.evm.Context.ArbOSVersion)

	// Reserve the gas budget in the block gas pool
	var err error
	if isAmsterdam {
		err = st.gp.CheckGasAmsterdam(min(st.msg.GasLimit, params.MaxTxGasRenamedForNitroMerges), st.msg.GasLimit)
	} else {
		err = st.gp.CheckGasLegacy(st.msg.GasLimit)
	}
	if err != nil {
		return err
	}

	// After Amsterdam we limit the regular gas to 16M, the data gas to the transaction limit
	limit := st.msg.GasLimit
	if isAmsterdam {
		limit = min(st.msg.GasLimit, params.MaxTxGasRenamedForNitroMerges)
	}
	st.gasRemaining = vm.NewGasBudget(limit, st.msg.GasLimit-limit)

	if st.evm.Config.Tracer.HasGasHook() {
		st.evm.Config.Tracer.EmitGasChange(tracing.Gas{}, st.gasRemaining.AsTracing(), tracing.GasChangeTxInitialBalance)
	}
	// Deduct the gas cost from the sender's balance
	st.state.SubBalance(st.msg.From, mgval, tracing.BalanceDecreaseGasBuy)

	// Arbitrum: record fee payment
	if tracer := st.evm.Config.Tracer; tracer != nil && tracer.CaptureArbitrumTransfer != nil {
		tracer.CaptureArbitrumTransfer(&st.msg.From, nil, mgval.ToBig(), true, tracing.BalanceDecreaseGasBuy)
	}

	return nil
}

// preCheck performs all pre-execution validation that does not require
// the EVM to run, then ends by calling buyGas to lock in the gas budget.
// It returns a consensus error if any of the following fail:
//
//   - Sender nonce matches state and is not at 2^64-1 (EIP-2681).
//   - EIP-7825 per-tx gas-limit cap on Osaka chains pre-Amsterdam
//     (the cap also bounds the regular dimension after Amsterdam, but
//     it is enforced there via the two-dimensional budget in buyGas).
//   - EIP-3607 sender-is-EOA, allowing accounts whose only code is an
//     EIP-7702 delegation designator.
//   - EIP-1559 fee-cap, tip-cap and base-fee constraints (London+).
//   - Blob-tx structural checks: non-nil `To`, non-empty hash list,
//     valid KZG versioned hashes, count below `BlobTxMaxBlobs` (Osaka+).
//   - Blob fee-cap not below the current blob base fee (Cancun+).
//   - EIP-7702 set-code-tx shape: non-nil `To` and non-empty
//     authorization list.
//
// The SkipNonceChecks / SkipTransactionChecks / NoBaseFee flags bypass
// subsets of these checks for simulation paths (eth_call, eth_estimateGas).
func (st *stateTransition) preCheck() error {
	// Only check transactions that are not fake
	msg := st.msg
	if !msg.SkipNonceChecks {
		// Make sure this transaction's nonce is correct.
		stNonce := st.state.GetNonce(msg.From)
		if msgNonce := msg.Nonce; stNonce < msgNonce {
			return fmt.Errorf("%w: address %v, tx: %d state: %d", ErrNonceTooHigh,
				msg.From.Hex(), msgNonce, stNonce)
		} else if stNonce > msgNonce {
			return fmt.Errorf("%w: address %v, tx: %d state: %d", ErrNonceTooLow,
				msg.From.Hex(), msgNonce, stNonce)
		} else if stNonce+1 < stNonce {
			return fmt.Errorf("%w: address %v, nonce: %d", ErrNonceMax,
				msg.From.Hex(), stNonce)
		}
	}
	var (
		isOsaka     = st.evm.ChainConfig().IsOsaka(st.evm.Context.BlockNumber, st.evm.Context.Time, st.evm.Context.ArbOSVersion)
		isAmsterdam = st.evm.ChainConfig().IsAmsterdam(st.evm.Context.BlockNumber, st.evm.Context.Time, st.evm.Context.ArbOSVersion)
	)
	if !msg.SkipTransactionChecks {
		// Verify tx gas limit does not exceed EIP-7825 cap.
		if !st.evm.ChainConfig().IsArbitrum() && !isAmsterdam && isOsaka && msg.GasLimit > params.MaxTxGasRenamedForNitroMerges {
			return fmt.Errorf("%w (cap: %d, tx: %d)", ErrGasLimitTooHigh, params.MaxTxGasRenamedForNitroMerges, msg.GasLimit)
		}
		// Make sure the sender is an EOA
		code := st.state.GetCode(msg.From)
		_, delegated := types.ParseDelegation(code)
		if len(code) > 0 && !delegated {
			return fmt.Errorf("%w: address %v, len(code): %d", ErrSenderNoEOA, msg.From.Hex(), len(code))
		}
	}

	// Make sure that transaction gasFeeCap is greater than the baseFee (post london)
	if st.evm.ChainConfig().IsLondon(st.evm.Context.BlockNumber) {
		// Skip the checks if gas fields are zero and baseFee was explicitly disabled (eth_call)
		skipCheck := st.evm.Config.NoBaseFee && msg.GasFeeCap.BitLen() == 0 && msg.GasTipCap.BitLen() == 0
		if !skipCheck {
			if msg.GasFeeCap.Cmp(msg.GasTipCap) < 0 {
				return fmt.Errorf("%w: address %v, maxPriorityFeePerGas: %s, maxFeePerGas: %s", ErrTipAboveFeeCap,
					msg.From.Hex(), msg.GasTipCap, msg.GasFeeCap)
			}
			// This will panic if baseFee is nil, but basefee presence is verified
			// as part of header validation.
			if msg.GasFeeCap.CmpBig(st.evm.Context.BaseFee) < 0 {
				return fmt.Errorf("%w: address %v, maxFeePerGas: %s, baseFee: %s", ErrFeeCapTooLow,
					msg.From.Hex(), msg.GasFeeCap, st.evm.Context.BaseFee)
			}
		}
	}
	// Check the blob version validity
	if msg.BlobHashes != nil {
		// The to field of a blob tx type is mandatory, and a `BlobTx` transaction internally
		// has it as a non-nillable value, so any msg derived from blob transaction has it non-nil.
		// However, messages created through RPC (eth_call) don't have this restriction.
		if msg.To == nil {
			return ErrBlobTxCreate
		}
		if len(msg.BlobHashes) == 0 {
			return ErrMissingBlobHashes
		}
		if isOsaka && len(msg.BlobHashes) > params.BlobTxMaxBlobs {
			return ErrTooManyBlobs
		}
		for i, hash := range msg.BlobHashes {
			if !kzg4844.IsValidVersionedHash(hash[:]) {
				return fmt.Errorf("blob %d has invalid hash version", i)
			}
		}
	}
	// Check that the user is paying at least the current blob fee
	if st.evm.ChainConfig().IsCancun(st.evm.Context.BlockNumber, st.evm.Context.Time, st.evm.Context.ArbOSVersion) {
		if st.blobGasUsed() > 0 {
			// Skip the checks if gas fields are zero and blobBaseFee was explicitly disabled (eth_call)
			skipCheck := st.evm.Config.NoBaseFee && msg.BlobGasFeeCap.BitLen() == 0
			if !skipCheck {
				// This will panic if blobBaseFee is nil, but blobBaseFee presence
				// is verified as part of header validation.
				if msg.BlobGasFeeCap.CmpBig(st.evm.Context.BlobBaseFee) < 0 {
					return fmt.Errorf("%w: address %v blobGasFeeCap: %v, blobBaseFee: %v", ErrBlobFeeCapTooLow,
						msg.From.Hex(), msg.BlobGasFeeCap, st.evm.Context.BlobBaseFee)
				}
			}
		}
	}
	// Check that EIP-7702 authorization list signatures are well formed.
	if msg.SetCodeAuthorizations != nil {
		if msg.To == nil {
			return fmt.Errorf("%w (sender %v)", ErrSetCodeTxCreate, msg.From)
		}
		if len(msg.SetCodeAuthorizations) == 0 {
			return fmt.Errorf("%w (sender %v)", ErrEmptyAuthList, msg.From)
		}
	}
	return st.buyGas()
}

// execute transitions the state by applying the current message and
// returns the EVM execution result with the following fields:
//
//   - used gas: total gas used, including gas refunded
//   - peak used gas: maximum gas used before applying refunds
//   - returndata: data returned by the EVM
//   - execution error: EVM-level errors that abort execution, such as
//     ErrOutOfGas or ErrExecutionReverted
//
// If a consensus error is encountered, it is returned directly with a
// nil EVM execution result.
func (st *stateTransition) execute() (*ExecutionResult, error) {
	endTxNow, startHookUsedMultiGas, err, returnData := st.evm.ProcessingHook.StartTxHook()
	startHookUsedSingleGas := startHookUsedMultiGas.SingleGas()

	if endTxNow {
		// Arbitrum endTxNow paths (deposits, retryable submission, internal txs)
		// bypass the normal CheckGasLegacy/ChargeGasLegacy flow but still report
		// gas used. Record it as used without reserving it from the pool, so
		// gp.Used() (which feeds ProcessResult.GasUsed, validated against
		// header.GasUsed) and gp.CumulativeUsed() (which feeds
		// receipt.CumulativeGasUsed, and the GasUsed derived from it via
		// DeriveFields) both include this tx's contribution, while the gas
		// stays available to the rest of the block.
		if startHookUsedSingleGas > 0 {
			if reportErr := st.gp.ReportUnreservedGas(startHookUsedSingleGas); reportErr != nil {
				return nil, reportErr
			}
		}
		return &ExecutionResult{
			UsedGas:       startHookUsedSingleGas,
			MaxUsedGas:    startHookUsedSingleGas,
			Err:           err,
			ReturnData:    returnData,
			ScheduledTxes: st.evm.ProcessingHook.ScheduledTxes(),
			UsedMultiGas:  startHookUsedMultiGas,
		}, nil
	}

	// Arbitrum: drop tip when tip collection is not enabled
	if !st.evm.ProcessingHook.CollectTips() {
		if baseFee := uint256.MustFromBig(st.evm.Context.BaseFee); st.msg.GasPrice.Cmp(baseFee) > 0 {
			st.msg.GasPrice = baseFee
			st.msg.GasTipCap = new(uint256.Int)
		}
	}

	// Validate the message and pre-pay gas.
	if err := st.preCheck(); err != nil {
		return nil, err
	}

	// Charge intrinsic gas (with overflow detection inside IntrinsicGas).
	// Under Amsterdam the cost is two-dimensional and Charge debits both
	// regular and state in one step.
	var (
		msg              = st.msg
		rules            = st.evm.ChainConfig().Rules(st.evm.Context.BlockNumber, st.evm.Context.Random != nil, st.evm.Context.Time, st.evm.Context.ArbOSVersion)
		contractCreation = msg.To == nil
		floorDataGas     uint64
		usedMultiGas     = multigas.ZeroGas()
	)
	iMultiGasCost, err := IntrinsicMultiGas(msg.Data, msg.AccessList, msg.SetCodeAuthorizations, contractCreation, rules, st.evm.Context.CostPerStateByte)
	if err != nil {
		return nil, err
	}
	iGasCost := intrinsicGasCosts(iMultiGasCost, rules.IsAmsterdam)
	prior, sufficient := st.gasRemaining.Charge(iGasCost)
	if !sufficient {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrIntrinsicGas, st.gasRemaining.RegularGas, iGasCost.RegularGas)
	}
	if st.evm.Config.Tracer.HasGasHook() {
		st.evm.Config.Tracer.EmitGasChange(prior.AsTracing(), st.gasRemaining.AsTracing(), tracing.GasChangeTxIntrinsicGas)
	}
	usedMultiGas = usedMultiGas.SaturatingAdd(iMultiGasCost)

	// Validate the EIP-7623 calldata floor against the gas limit. The floor inflates
	// the total gas usage at tx end, so the gas limit must be sufficient to cover that.
	if rules.IsPrague && st.evm.ProcessingHook.IsCalldataPricingIncreaseEnabled() {
		floorDataGas, err = FloorDataGas(rules, msg.Data, msg.AccessList)
		if err != nil {
			return nil, err
		}
		// Make sure the transaction has sufficient gas allowance to
		// pay the floor cost.
		if msg.GasLimit < floorDataGas {
			return nil, fmt.Errorf("%w: have %d, want %d", ErrFloorDataGas, msg.GasLimit, floorDataGas)
		}
		// In Amsterdam, the transaction gas limit is allowed to exceed
		// params.MaxTxGas, but the calldata floor cost is capped by it.
		if rules.IsAmsterdam && max(iGasCost.RegularGas, floorDataGas) > params.MaxTxGasRenamedForNitroMerges {
			return nil, fmt.Errorf("%w: regular intrisic cost %v, floor: %v", ErrFloorDataGas, iGasCost.RegularGas, floorDataGas)
		}
	}

	tipAmount := big.NewInt(0)
	tipReceipient, multiGas, err := st.evm.ProcessingHook.GasChargingHook(&st.gasRemaining.RegularGas, iGasCost.RegularGas)
	if err != nil {
		return nil, err
	}

	usedMultiGas = usedMultiGas.SaturatingAdd(multiGas)

	if rules.IsEIP4762 {
		st.evm.AccessEvents.AddTxOrigin(msg.From)

		if targetAddr := msg.To; targetAddr != nil {
			st.evm.AccessEvents.AddTxDestination(*targetAddr, msg.Value.Sign() != 0, !st.state.Exist(*targetAddr))
		}
	}

	// Top-call affordability, the sender must still be able to cover the value
	// transfer of the top frame after gas pre-pay.
	value := msg.Value
	if value == nil {
		value = new(uint256.Int)
	}
	if !value.IsZero() && !st.evm.Context.CanTransfer(st.state, msg.From, value) {
		return nil, fmt.Errorf("%w: address %v", ErrInsufficientFundsForTransfer, msg.From.Hex())
	}

	// Execute the preparatory steps for state transition which includes:
	// - prepare accessList(post-berlin)
	// - reset transient storage(EIP-1153)
	// - enable block-level accessList construction (EIP-7928)
	st.state.Prepare(rules, msg.From, st.evm.Context.Coinbase, msg.To, vm.ActivePrecompiles(rules), msg.AccessList)

	// Execute the top-most frame
	var deployedContract *common.Address

	var (
		ret    []byte
		vmerr  error // vm errors do not effect consensus and are therefore not assigned to err
		result vm.GasBudget
	)

	// Check against hardcoded transaction hashes that have previously reverted, so instead
	// of executing the transaction we just update state nonce and remaining gas to avoid
	// state divergence.
	usedMultiGas, vmerr = st.evm.ProcessingHook.RevertedTxHook(&st.gasRemaining.RegularGas, usedMultiGas)

	// vmerr is only not nil when we find a previous reverted transaction
	if vmerr == nil {
		// Capture the forwarded regular-gas amount BEFORE ForwardAll consumes
		// it, so Absorb can back out state-gas spillover from UsedRegularGas
		// per EIP-8037.
		forwarded := st.gasRemaining.RegularGas
		if contractCreation {
			// Check whether the init code size has been exceeded.
			if err := vm.CheckMaxInitCodeSize(&rules, uint64(len(msg.Data))); err != nil {
				return nil, err
			}
			// Execute the transaction's creation.
			deployedContract = &common.Address{}
			ret, *deployedContract, result, multiGas, vmerr = st.evm.Create(msg.From, msg.Data, st.gasRemaining.ForwardAll(), value)
			st.gasRemaining.Absorb(result, forwarded)
			usedMultiGas = usedMultiGas.SaturatingAdd(multiGas)

			// If the contract creation failed, refund the account-creation state
			// gas pre-charged in IntrinsicGas.
			if rules.IsAmsterdam && vmerr != nil {
				st.gasRemaining.RefundState(params.AccountCreationSize * st.evm.Context.CostPerStateByte)
			}
		} else {
			// Increment the nonce for the next transaction.
			st.state.SetNonce(msg.From, st.state.GetNonce(msg.From)+1, tracing.NonceChangeEoACall)

			// Apply EIP-7702 authorizations.
			st.applyAuthorizations(rules, msg.SetCodeAuthorizations)

			// Perform convenience warming of sender's delegation target. Although the
			// sender is already warmed in Prepare(..), it's possible a delegation to
			// the account was deployed during this transaction. To handle correctly,
			// simply wait until the final state of delegations is determined before
			// performing the resolution and warming.
			if addr, ok := types.ParseDelegation(st.state.GetCode(*msg.To)); ok {
				st.state.AddAddressToAccessList(addr)
			}
			// Execute the transaction's call.
			ret, result, multiGas, vmerr = st.evm.Call(msg.From, st.to(), msg.Data, st.gasRemaining.ForwardAll(), value)
			st.gasRemaining.Absorb(result, forwarded)
			usedMultiGas = usedMultiGas.SaturatingAdd(multiGas)
		}
	}

	// Settle down the gas usage and refund the ETH back if any remaining
	gasUsed, peakUsed, gasLeft, usedMultiGas, err := st.settleGas(rules, floorDataGas, usedMultiGas)
	if err != nil {
		return nil, err
	}

	// Pay the effective transaction fee to the specific coinbase
	effectiveTip := msg.GasPrice
	if rules.IsLondon {
		baseFee, overflow := uint256.FromBig(st.evm.Context.BaseFee)
		if overflow {
			return nil, fmt.Errorf("invalid baseFee: %v", st.evm.Context.BaseFee)
		}
		effectiveTip = new(uint256.Int).Sub(msg.GasPrice, baseFee)
	}
	if st.evm.Config.NoBaseFee && msg.GasFeeCap.Sign() == 0 && msg.GasTipCap.Sign() == 0 {
		// Skip fee payment when NoBaseFee is set and the fee fields
		// are 0. This avoids a negative effectiveTip being applied to
		// the coinbase when simulating calls.
	} else {
		// Only charge the tip on compute gas, not poster gas.
		// The poster is compensated separately in EndTxHook.
		computeGasUsed, posterGas := uint64(0), st.evm.ProcessingHook.PosterGas()
		if gasUsed > posterGas {
			computeGasUsed = gasUsed - posterGas
		}
		fee := new(uint256.Int).SetUint64(computeGasUsed)
		fee.Mul(fee, effectiveTip)
		st.state.AddBalance(tipReceipient, fee, tracing.BalanceIncreaseRewardTransactionFee)
		tipAmount = fee.ToBig()

		// add the coinbase to the witness iff the fee is greater than 0
		if rules.IsEIP4762 && fee.Sign() != 0 {
			st.evm.AccessEvents.AddAccount(st.evm.Context.Coinbase, true, math.MaxUint64)
		}
	}
	// Arbitrum: record the tip
	if tracer := st.evm.Config.Tracer; tracer != nil && st.evm.ProcessingHook.CollectTips() && tracer.CaptureArbitrumTransfer != nil {
		tracer.CaptureArbitrumTransfer(nil, &tipReceipient, tipAmount, false, tracing.BalanceIncreaseRewardTransactionFee)
	}

	st.evm.ProcessingHook.EndTxHook(gasLeft, usedMultiGas, vmerr == nil)

	// Arbitrum: record self destructs
	if tracer := st.evm.Config.Tracer; tracer != nil && tracer.CaptureArbitrumTransfer != nil {
		suicides := st.evm.StateDB.GetSelfDestructs()
		for i, address := range suicides {
			balance := st.evm.StateDB.GetBalance(address)
			tracer.CaptureArbitrumTransfer(&suicides[i], nil, balance.ToBig(), false, tracing.BalanceDecreaseSelfdestruct)
		}
	}

	// EIP-7708: Emit the ETH-burn logs
	if rules.IsAmsterdam {
		for _, log := range st.evm.StateDB.LogsForBurnAccounts() {
			st.evm.StateDB.AddLog(log)
		}
	}
	return &ExecutionResult{
		UsedGas:          gasUsed,
		MaxUsedGas:       peakUsed,
		Err:              vmerr,
		ReturnData:       ret,
		ScheduledTxes:    st.evm.ProcessingHook.ScheduledTxes(),
		TopLevelDeployed: deployedContract,
		UsedMultiGas:     usedMultiGas,
	}, nil
}

// settleGas finalizes the per-tx gas accounting after EVM execution:
//
//   - Snapshots the EIP-8037 block-level 2D figures (tx_regular_gas,
//     tx_state_gas) before any refund or floor:
//
//     tx_gas_used_before_refund = tx.gas - gas_left - state_gas_reservoir
//     tx_state_gas              = state_gas_used
//     tx_regular_gas            = tx_gas_used_before_refund - tx_state_gas
//
//   - Computes the receipt scalar tx_gas_used by applying the EIP-3529
//     refund (capped at tx_gas_used_before_refund/5) and the EIP-7623
//     calldata floor:
//
//     tx_gas_used = max(tx_gas_used_before_refund - tx_gas_refund, calldata_floor)
//
//   - Charges the block gas pool (2D under Amsterdam, scalar pre-Amsterdam).
//
//   - Refunds the leftover gas to the sender as ETH.
//
// Returns the receipt-level tx_gas_used, the pre-refund peak (consumed
// by gas-estimation callers via ExecutionResult.MaxUsedGas), the leftover
// gas (consumed by the Arbitrum EndTxHook), and the multigas usage updated
// with the refund and calldata-floor bookkeeping. UsedStateGas should never
// become negative in the top-most frame, since state-gas refunds occur only
// when state creation is reverted within the same transaction and clearing
// pre-existing state is never refunded.
//
// Arbitrum: gas held by the ProcessingHook is returned to the leftover up
// front and never counts as used.
func (st *stateTransition) settleGas(rules params.Rules, floorDataGas uint64, usedMultiGas multigas.MultiGas) (uint64, uint64, uint64, multigas.MultiGas, error) {
	if st.gasRemaining.UsedStateGas < 0 {
		return 0, 0, 0, usedMultiGas, fmt.Errorf("negative topmost frame state gas usage, %d", st.gasRemaining.UsedStateGas)
	}
	txStateGas := uint64(st.gasRemaining.UsedStateGas)

	// EIP-8037:
	// tx_gas_used_before_refund = tx.gas - tx_output.gas_left - tx_output.state_gas_reservoir
	// tx_state_gas = intrinsic_state_gas + tx_output.execution_state_gas_used
	// tx_regular_gas = tx_gas_used_before_refund - tx_state_gas
	gasLeft := st.gasRemaining.RegularGas + st.gasRemaining.StateGas + st.evm.ProcessingHook.HeldGas()
	gasUsedBeforeRefund := st.msg.GasLimit - gasLeft

	if gasUsedBeforeRefund < txStateGas {
		return 0, 0, 0, usedMultiGas, fmt.Errorf("negative topmost frame regular gas usage, total: %d, state: %d", gasUsedBeforeRefund, txStateGas)
	}
	txRegularGas := gasUsedBeforeRefund - txStateGas

	// EIP-3529: tx_gas_refund = min(tx_gas_used_before_refund/5, refund_counter).
	refund := st.calcRefund(gasUsedBeforeRefund)
	// Arbitrum: set the multigas refunds
	usedMultiGas = usedMultiGas.WithRefund(refund)
	if st.evm.Config.Tracer.HasGasHook() {
		st.evm.Config.Tracer.EmitGasChange(tracing.Gas{Regular: gasLeft}, tracing.Gas{Regular: gasLeft + refund}, tracing.GasChangeTxRefunds)
	}
	gasLeft += refund
	gasUsed := gasUsedBeforeRefund - refund

	// EIP-7623: tx_gas_used = max(tx_gas_used_after_refund, calldata_floor).
	peakUsed := gasUsedBeforeRefund
	if rules.IsPrague && gasUsed < floorDataGas {
		diff := floorDataGas - gasUsed
		if st.evm.Config.Tracer.HasGasHook() {
			st.evm.Config.Tracer.EmitGasChange(tracing.Gas{Regular: gasLeft}, tracing.Gas{Regular: gasLeft - diff}, tracing.GasChangeTxDataFloor)
		}
		gasLeft -= diff
		gasUsed = floorDataGas
		peakUsed = max(peakUsed, floorDataGas)
		// Arbitrum: the floor bump is calldata gas.
		if floorDataGas > usedMultiGas.SingleGas() {
			usedMultiGas = usedMultiGas.SaturatingIncrement(multigas.ResourceKindL2Calldata, floorDataGas-usedMultiGas.SingleGas())
		}
	}

	if rules.IsAmsterdam {
		if err := st.gp.ChargeGasAmsterdam(txRegularGas, txStateGas, gasUsed); err != nil {
			return 0, 0, 0, usedMultiGas, err
		}
	} else {
		if err := st.gp.ChargeGasLegacy(gasLeft, gasUsed); err != nil {
			return 0, 0, 0, usedMultiGas, err
		}
	}

	// Refund leftover gas to the sender as ETH.
	if gasLeft > 0 {
		refund := new(uint256.Int).Mul(uint256.NewInt(gasLeft), st.msg.GasPrice)
		st.state.AddBalance(st.msg.From, refund, tracing.BalanceIncreaseGasReturn)

		if st.evm.Config.Tracer.HasGasHook() {
			st.evm.Config.Tracer.EmitGasChange(tracing.Gas{Regular: gasLeft}, tracing.Gas{}, tracing.GasChangeTxLeftOverReturned)
		}
	}

	// Arbitrum: record the gas refund, including zero-value refunds
	if tracer := st.evm.Config.Tracer; tracer != nil && tracer.CaptureArbitrumTransfer != nil {
		refund := new(uint256.Int).Mul(uint256.NewInt(gasLeft), st.msg.GasPrice)
		tracer.CaptureArbitrumTransfer(nil, &st.msg.From, refund.ToBig(), false, tracing.BalanceIncreaseGasReturn)
	}
	return gasUsed, peakUsed, gasLeft, usedMultiGas, nil
}

// validateAuthorization validates an EIP-7702 authorization against the state.
func (st *stateTransition) validateAuthorization(auth *types.SetCodeAuthorization) (authority common.Address, err error) {
	// Verify chain ID is null or equal to current chain ID.
	if !auth.ChainID.IsZero() && auth.ChainID.CmpBig(st.evm.ChainConfig().ChainID) != 0 {
		return authority, ErrAuthorizationWrongChainID
	}
	// Limit nonce to 2^64-1 per EIP-2681.
	if auth.Nonce+1 < auth.Nonce {
		return authority, ErrAuthorizationNonceOverflow
	}
	// Validate signature values and recover authority.
	authority, err = auth.Authority()
	if err != nil {
		return authority, fmt.Errorf("%w: %v", ErrAuthorizationInvalidSignature, err)
	}
	// Check the authority account
	//  1) doesn't have code or has existing delegation
	//  2) matches the auth's nonce
	//
	// Note it is added to the access list even if the authorization is invalid.
	st.state.AddAddressToAccessList(authority)
	code := st.state.GetCode(authority)
	if _, ok := types.ParseDelegation(code); len(code) != 0 && !ok {
		return authority, ErrAuthorizationDestinationHasCode
	}
	if have := st.state.GetNonce(authority); have != auth.Nonce {
		return authority, ErrAuthorizationNonceMismatch
	}
	return authority, nil
}

// applyAuthorization applies an EIP-7702 code delegation to the state and,
// under EIP-8037, reconciles the per-authorization intrinsic state-gas pre-
// charge so that, per authority:
//
//   - the account portion (AccountCreationSize × CPSB) is charged at most
//     once, and only when the account did not exist before the tx
//
//   - the delegation-indicator portion (AuthorizationCreationSize × CPSB) is
//     charged at most once, and only when the authority ends the tx delegated
//     having started it undelegated.
func (st *stateTransition) applyAuthorization(rules params.Rules, auth *types.SetCodeAuthorization, delegates map[common.Address]bool) error {
	authority, err := st.validateAuthorization(auth)
	if err != nil {
		if rules.IsAmsterdam {
			st.gasRemaining.RefundState((params.AccountCreationSize + params.AuthorizationCreationSize) * st.evm.Context.CostPerStateByte)
		}
		return err
	}
	prevDelegation, curDelegated := types.ParseDelegation(st.state.GetCode(authority))

	if !rules.IsAmsterdam {
		if st.state.Exist(authority) {
			st.state.AddRefund(params.CallNewAccountGas - params.TxAuthTupleGas)
		}
	} else {
		if st.state.Exist(authority) {
			st.gasRemaining.RefundState(params.AccountCreationSize * st.evm.Context.CostPerStateByte)
		}
		authBase := params.AuthorizationCreationSize * st.evm.Context.CostPerStateByte

		preDelegated, ok := delegates[authority]
		if !ok {
			preDelegated = curDelegated
			delegates[authority] = preDelegated
		}
		if auth.Address == (common.Address{}) {
			// Clearing writes no indicator, refill this auth's state charge.
			st.gasRemaining.RefundState(authBase)

			// The indicator was created by an earlier auth within the same
			// transaction, refill the state charge as it's no longer justified.
			if curDelegated && !preDelegated {
				st.gasRemaining.RefundState(authBase)
			}
		} else if curDelegated || preDelegated {
			// The 23-byte slot is already occupied, overwriting it writes no
			// new bytes, refill the state charge.
			st.gasRemaining.RefundState(authBase)
		}
	}

	// Update nonce and account code.
	st.state.SetNonce(authority, auth.Nonce+1, tracing.NonceChangeAuthorization)

	// Delegation to zero address means clear.
	if auth.Address == (common.Address{}) {
		if curDelegated {
			st.state.SetCode(authority, nil, tracing.CodeChangeAuthorizationClear)
		}
		return nil
	}
	// Install delegation to auth.Address if the delegation changed
	if !curDelegated || auth.Address != prevDelegation {
		st.state.SetCode(authority, types.AddressToDelegation(auth.Address), tracing.CodeChangeAuthorization)
	}
	return nil
}

// applyAuthorizations applies an EIP-7702 code delegation to the state.
func (st *stateTransition) applyAuthorizations(rules params.Rules, auths []types.SetCodeAuthorization) {
	preDelegated := make(map[common.Address]bool)
	for _, auth := range auths {
		st.applyAuthorization(rules, &auth, preDelegated)
	}
}

// calcRefund computes the EIP-3529 refund cap against tx_gas_used_before_refund.
// Arbitrum: gas the ProcessingHook marks nonrefundable is excluded from the
// cap base.
func (st *stateTransition) calcRefund(gasUsedBeforeRefund uint64) uint64 {
	nonrefundable := st.evm.ProcessingHook.NonrefundableGas()
	if nonrefundable >= gasUsedBeforeRefund {
		return 0
	}
	quotient := params.RefundQuotient
	if st.evm.ChainConfig().IsLondon(st.evm.Context.BlockNumber) {
		quotient = params.RefundQuotientEIP3529
	}
	refund := (gasUsedBeforeRefund - nonrefundable) / quotient
	if refund > st.state.GetRefund() {
		refund = st.state.GetRefund()
	}
	return refund
}

// blobGasUsed returns the amount of blob gas used by the message.
func (st *stateTransition) blobGasUsed() uint64 {
	return uint64(len(st.msg.BlobHashes) * params.BlobTxBlobGasPerBlob)
}

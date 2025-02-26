package brontes

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

// ---------------------------------------------------------------------
// Basic types and helpers
// ---------------------------------------------------------------------

// LogData represents log data with topics and data.
type LogData struct {
	Topics []common.Hash
	Data   []byte
}

// ---------------------------------------------------------------------
// Types for tracing and call frames
// ---------------------------------------------------------------------

// CallTrace represents a trace of a call.
type CallTrace struct {
	Depth                    int
	Success                  bool
	Caller                   common.Address
	Address                  common.Address // For CALL calls, this is the callee; for CREATE, it is the created address.
	MaybePrecompile          *bool
	SelfdestructRefundTarget *common.Address
	Kind                     CallKind
	Value                    *big.Int
	Data                     []byte
	Output                   []byte
	GasUsed                  uint64
	GasLimit                 uint64
	Reverted                 bool
	Error                    error
	Steps                    []CallTraceStep
}

func (ct *CallTrace) IsError() bool {
	return ct.Error != nil
}

func (ct *CallTrace) IsRevert() bool {
	return ct.Reverted
}

func (ct *CallTrace) AsErrorMsg(kind TraceStyle) *string {
	if !ct.IsError() {
		return nil
	}
	errMsg := ct.Error.Error()
	return &errMsg
}

// CallTraceNode represents a node in the call trace arena.
type CallTraceNode struct {
	Parent   *int
	Children []int
	Idx      int
	Trace    CallTrace
	Logs     []LogData
	Ordering []LogCallOrder
}

// ExecutionAddress returns the execution address based on the call kind.
func (ctn *CallTraceNode) ExecutionAddress() common.Address {
	if ctn.Trace.Kind.IsDelegate() {
		return ctn.Trace.Caller
	}
	return ctn.Trace.Address
}

// IsPrecompile returns true if the trace is a call to a precompile.
func (ctn *CallTraceNode) IsPrecompile() bool {
	if ctn.Trace.MaybePrecompile != nil {
		return *ctn.Trace.MaybePrecompile
	}
	return false
}

// Kind returns the kind of the call.
func (ctn *CallTraceNode) Kind() CallKind {
	return ctn.Trace.Kind
}

// IsSelfdestruct returns true if the call was a selfdestruct.
func (ctn *CallTraceNode) IsSelfdestruct() bool {
	return ctn.Trace.SelfdestructRefundTarget != nil
}

// ---------------------------------------------------------------------
// Call kinds and conversions
// ---------------------------------------------------------------------

// CallKind is an enumeration of call types.
type CallKind int

const (
	CallKindCall CallKind = iota
	CallKindStaticCall
	CallKindCallCode
	CallKindDelegateCall
	CallKindCreate
	CallKindCreate2
)

func FromCallTypeCode(typ byte) CallKind {
	callScheme := vm.OpCode(typ)
	switch callScheme {
	case vm.CALL:
		return CallKindCall
	case vm.STATICCALL:
		return CallKindStaticCall
	case vm.CALLCODE:
		return CallKindCallCode
	case vm.DELEGATECALL:
		return CallKindDelegateCall
	case vm.CREATE:
		return CallKindCreate
	case vm.CREATE2:
		return CallKindCreate2
	}
	panic("unknown call type")
}

func (ck CallKind) IsAnyCreate() bool {
	return ck == CallKindCreate || ck == CallKindCreate2
}

func (ck CallKind) IsAnyCall() bool {
	return ck == CallKindCall || ck == CallKindCallCode || ck == CallKindStaticCall || ck == CallKindDelegateCall
}

func (ck CallKind) IsDelegate() bool {
	return ck == CallKindDelegateCall || ck == CallKindCallCode
}

func (ck CallKind) IsStaticCall() bool {
	return ck == CallKindStaticCall
}

func (ck CallKind) String() string {
	switch ck {
	case CallKindCall:
		return "CALL"
	case CallKindStaticCall:
		return "STATICCALL"
	case CallKindCallCode:
		return "CALLCODE"
	case CallKindDelegateCall:
		return "DELEGATECALL"
	case CallKindCreate:
		return "CREATE"
	case CallKindCreate2:
		return "CREATE2"
	default:
		return "UNKNOWN"
	}
}

// ---------------------------------------------------------------------
// Additional supporting types
// ---------------------------------------------------------------------

// CallTraceStepStackItem represents an item on the call trace step stack.
type CallTraceStepStackItem struct {
	TraceNode   *CallTraceNode
	Step        *CallTraceStep
	CallChildID *int
}

// CallTraceStep represents a tracked execution step.
type CallTraceStep struct {
	Depth            int
	Pc               int
	Op               vm.OpCode
	Contract         common.Address
	Stack            *[]uint256.Int // nil if not captured
	PushStack        *[]uint256.Int
	Memory           RecordedMemory
	MemorySize       int
	GasRemaining     uint64
	GasRefundCounter uint64
	GasCost          uint64
	StorageChange    *StorageChange
}

// ---------------------------------------------------------------------
// Storage and memory types
// ---------------------------------------------------------------------

// StorageChangeReason indicates why a storage slot was modified.
type StorageChangeReason int

const (
	StorageChangeReasonSLOAD StorageChangeReason = iota
	StorageChangeReasonSSTORE
)

// StorageChange represents a change to contract storage.
type StorageChange struct {
	Key      *big.Int
	Value    *big.Int
	HadValue *big.Int
	Reason   StorageChangeReason
}

// RecordedMemory wraps captured execution memory.
type RecordedMemory struct {
	Data []byte
}

func NewRecordedMemory(mem []byte) RecordedMemory {
	return RecordedMemory{Data: mem}
}

func (rm *RecordedMemory) AsBytes() []byte {
	return rm.Data
}

func (rm *RecordedMemory) Resize(size int) {
	if len(rm.Data) < size {
		newData := make([]byte, size)
		copy(newData, rm.Data)
		rm.Data = newData
	} else {
		rm.Data = rm.Data[:size]
	}
}

func (rm *RecordedMemory) Len() int {
	return len(rm.Data)
}

func (rm *RecordedMemory) IsEmpty() bool {
	return len(rm.Data) == 0
}

func (rm *RecordedMemory) MemoryChunks() []string {
	return convertMemory(rm.AsBytes())
}

// TransactionTrace represents a parity transaction trace.
type TransactionTrace struct {
	Action       ActionInterface
	Error        *string
	Result       *TraceOutput
	TraceAddress []uint64
	Subtraces    int
}

type ActionKind int

const (
	ActionKindCall = iota
	ActionKindCreate
	ActionKindSelfDestruct
	ActionKindReward
)

type ActionInterface interface {
	GetFromAddr() common.Address
	ActionType() ActionKind
	GetToAddr() common.Address
	GetMsgValue() []byte
	GetCallData() []byte
}

// Action represents a call action (or create/selfdestruct).
type Action struct {
	Type         ActionKind
	Call         *CallAction
	Create       *CreateAction
	SelfDestruct *SelfdestructAction
	Reward       *RewardAction
}

type RewardType int

const (
	RewardTypeBlock RewardType = iota
	RewardTypeUncle
)

// CallAction represents a call action.
type CallAction struct {
	From     common.Address
	To       common.Address
	Value    *big.Int
	Gas      uint64
	Input    []byte
	CallType CallKind
}

func (ca *CallAction) GetFromAddr() common.Address {
	return ca.From
}

func (ca *CallAction) ActionType() ActionKind {
	return ActionKindCall
}

func (ca *CallAction) GetToAddr() common.Address {
	return ca.To
}

func (ca *CallAction) GetMsgValue() []byte {
	return ca.Value.Bytes()
}

func (ca *CallAction) GetCallData() []byte {
	return ca.Input
}

// CallOutput represents the output of a call.
type CallOutput struct {
	GasUsed uint64
	Output  []byte
}

// CreateAction represents a contract creation action.
type CreateAction struct {
	From  common.Address
	Value *big.Int
	Gas   uint64
	Init  []byte
}

func (ca *CreateAction) GetFromAddr() common.Address {
	return ca.From
}

func (ca *CreateAction) ActionType() ActionKind {
	return ActionKindCall
}

func (ca *CreateAction) GetToAddr() common.Address {
	return common.Address{}
}

func (ca *CreateAction) GetMsgValue() []byte {
	return ca.Value.Bytes()
}

func (ca *CreateAction) GetCallData() []byte {
	return ca.Init
}

type RewardAction struct {
	Author     common.Address
	RewardType RewardType
	Value      *big.Int
}

func (ra *RewardAction) GetFromAddr() common.Address {
	return ra.Author
}

func (ra *RewardAction) ActionType() ActionKind {
	return ActionKindReward
}

func (ra *RewardAction) GetToAddr() common.Address {
	return common.Address{}
}

func (ra *RewardAction) GetMsgValue() []byte {
	return ra.Value.Bytes()
}

func (ra *RewardAction) GetCallData() []byte {
	return []byte{}
}

// CreateOutput represents the output of a contract creation.
type CreateOutput struct {
	GasUsed uint64
	Code    []byte
	Address common.Address
}

// SelfdestructAction represents a selfdestruct action.
type SelfdestructAction struct {
	Address       common.Address
	RefundAddress common.Address
	Balance       *big.Int
}

func (sa *SelfdestructAction) GetFromAddr() common.Address {
	return sa.Address
}

func (sa *SelfdestructAction) ActionType() ActionKind {
	return ActionKindSelfDestruct
}

func (sa *SelfdestructAction) GetToAddr() common.Address {
	return sa.Address
}

func (sa *SelfdestructAction) GetMsgValue() []byte {
	return []byte{}
}

func (sa *SelfdestructAction) GetCallData() []byte {
	return []byte{}
}

type TraceOutputType int

const (
	TraceOutputTypeCall TraceOutputType = iota
	TraceOutputTypeCreate
)

// TraceOutput represents the output in a trace (either call or create).
type TraceOutput struct {
	Type   TraceOutputType
	Call   *CallOutput
	Create *CreateOutput
}

// LogCallOrderType distinguishes between a log index and a call (trace node) index.
type LogCallOrderType int

const (
	// LogCallOrderLog indicates that the ordering holds the index of a corresponding log.
	LogCallOrderLog LogCallOrderType = iota
	// LogCallOrderCall indicates that the ordering holds the index of a corresponding trace node.
	LogCallOrderCall
)

// LogCallOrder represents the ordering for calls and logs.
// It contains a type tag (LogCallOrderLog or LogCallOrderCall) and an associated index.
type LogCallOrder struct {
	Type  LogCallOrderType
	Index int
}

func NewLogCallOrderCall(i int) LogCallOrder {
	return LogCallOrder{Type: LogCallOrderCall, Index: i}
}

func NewLogCallOrderLog(i int) LogCallOrder {
	return LogCallOrder{Type: LogCallOrderLog, Index: i}
}

type TransactionInfo struct {
	Hash        *common.Hash
	Index       *uint64
	BlockHash   *common.Hash
	BlockNumber *uint64
	BaseFee     *big.Int
}

type ExecutionStatus int

const (
	ExecutionSuccess = iota
	ExecutionRevert
	ExecutionHalt
)

type SuccessReason int

const (
	SuccessReasonStop = iota
	SuccessReasonReturn
	SuccessReasonSelfDestructj
)

type ExeuctionResultSuccess struct {
	Reason      SuccessReason
	GasUsed     uint64
	GasRefunded uint64
	Logs        []LogData
	Output      TraceOutput
}

type ExeuctionResultRevert struct {
	GasUsed uint64
	Output  []byte
}

type HaltReason int

// TODO: There are more than 10 reasons for a halt, but let's not take care of it now since we are not interested to them at the moment.
const (
	HaltReasonFail = iota
)

type ExeuctionResultHalt struct {
	Reason  HaltReason
	GasUsed uint64
}

type ExecutionResult struct {
	Status  ExecutionStatus
	Success *ExeuctionResultSuccess
	Revert  *ExeuctionResultRevert
	Halt    *ExeuctionResultHalt
}

func (er *ExecutionResult) GasUsed() uint64 {
	switch er.Status {
	case ExecutionSuccess:
		return er.Success.GasUsed
	case ExecutionRevert:
		return er.Revert.GasUsed
	case ExecutionHalt:
		return er.Halt.GasUsed
	}
	panic("unknown execution result status")
}

func (er *ExecutionResult) IsSuccess() bool {
	return er.Status == ExecutionSuccess
}

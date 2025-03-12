package brontes

import (
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type TraceActions interface {
	GetCallFrameInfo() CallFrameInfo
	GetFromAddr() common.Address
	GetToAddr() common.Address
	GetMsgSender() common.Address
	GetCallData() []byte
	GetReturnCallData() []byte
	IsStaticCall() bool
	IsCreate() bool
	ActionType() ActionKind
	GetCreateOutput() common.Address
	IsDelegateCall() bool
}

type DecodedParams struct {
	FieldName string
	FieldType string
	Value     string
}

type DecodedCallData struct {
	FunctionName string
	CallData     []DecodedParams
	ReturnData   []DecodedParams
}

type CallFrameInfo struct {
	TraceIdx      uint64
	CallData      []byte
	ReturnData    []byte
	TargetAddress common.Address
	FromAddress   common.Address
	Logs          []types.Log
	DelegateLogs  []types.Log
	MsgSender     common.Address
	MsgValue      []byte
}

type CallInfo struct {
	TraceIdx      uint64
	TargetAddress common.Address
	FromAddress   common.Address
	MsgSender     common.Address
	MsgValue      *big.Int
}

type TransactionTraceWithLogs struct {
	Kind        CallKind
	Trace       TransactionTrace
	Logs        []types.Log
	MsgSender   common.Address
	TraceIdx    uint64
	DecodedData *DecodedCallData
}

func (t *TransactionTraceWithLogs) IsStaticCall() bool {
	return t.Kind.IsStaticCall()
}

func (t *TransactionTraceWithLogs) IsCreate() bool {
	return t.Kind.IsAnyCreate()
}

func (t *TransactionTraceWithLogs) IsDelegateCall() bool {
	return t.Kind.IsDelegate()
}

func (t *TransactionTraceWithLogs) GetCreateOutput() common.Address {
	if t.Trace.Result.Type == TraceOutputTypeCreate && t.Trace.Result.Create != nil {
		return common.Address(t.Trace.Result.Create.Address)
	}
	return common.Address{} // default address
}

func (t *TransactionTraceWithLogs) ActionType() ActionKind {
	return t.Trace.Action.ActionType()
}

func (t *TransactionTraceWithLogs) GetFromAddr() common.Address {
	return t.Trace.Action.GetFromAddr()
}

func (t *TransactionTraceWithLogs) GetMsgSender() common.Address {
	return t.MsgSender
}

func (t *TransactionTraceWithLogs) GetToAddr() common.Address {
	return t.Trace.Action.GetToAddr()
}

func (t *TransactionTraceWithLogs) GetCallData() []byte {
	return t.Trace.Action.GetCallData()
}

func (t *TransactionTraceWithLogs) GetReturnCallData() []byte {
	if t.Trace.Result == nil {
		return nil
	}

	if t.Trace.Result.Call != nil {
		return t.Trace.Result.Call.Output
	}

	return nil
}

func (t *TransactionTraceWithLogs) GetMsgValue() []byte {
	return t.Trace.Action.GetMsgValue()
}

func (t *TransactionTraceWithLogs) GetCallFrameInfo() CallFrameInfo {
	return CallFrameInfo{
		TraceIdx:      t.TraceIdx,
		CallData:      t.GetCallData(),
		ReturnData:    t.GetReturnCallData(),
		TargetAddress: t.GetToAddr(),
		FromAddress:   t.GetFromAddr(),
		Logs:          t.Logs,
		DelegateLogs:  make([]types.Log, 0),
		MsgSender:     t.MsgSender,
		MsgValue:      t.GetMsgValue(),
	}
}

type TxTrace struct {
	BlockNumber    uint64
	Trace          []TransactionTraceWithLogs
	TxHash         common.Hash
	GasUsed        *big.Int
	EffectivePrice *big.Int
	TxIndex        uint64
	IsSuccess      bool
}

// NewTxTrace creates a new TxTrace.
func NewTxTrace(
	blockNumber uint64,
	trace []TransactionTraceWithLogs,
	txHash [32]byte,
	txIndex uint64,
	gasUsed *big.Int,
	effectivePrice *big.Int,
	isSuccess bool,
) *TxTrace {
	return &TxTrace{
		BlockNumber:    blockNumber,
		Trace:          trace,
		TxHash:         common.BytesToHash(txHash[:]),
		TxIndex:        txIndex,
		GasUsed:        gasUsed,
		EffectivePrice: effectivePrice,
		IsSuccess:      isSuccess,
	}
}

func (t TxTrace) MarshalJSON() ([]byte, error) {
	// Base map to hold fields
	m := make(map[string]any)
	m["block_number"] = t.BlockNumber
	m["tx_hash"] = t.TxHash.String()
	m["gas_used"] = t.GasUsed.String()
	m["effective_price"] = t.EffectivePrice.String()
	m["tx_index"] = t.TxIndex
	m["is_success"] = t.IsSuccess

	// Process trace meta fields.
	var traceIdx []uint64
	var msgSender []string
	var errors []any // use any so nil values can be preserved
	var subtraces []uint64
	var traceAddress [][]uint64

	for _, traceItem := range t.Trace {
		traceIdx = append(traceIdx, traceItem.TraceIdx)
		msgSender = append(msgSender, traceItem.MsgSender.String())
		// Copy the error field (could be nil)
		errors = append(errors, traceItem.Trace.Error)
		subtraces = append(subtraces, uint64(traceItem.Trace.Subtraces))
		traceAddress = append(traceAddress, traceItem.Trace.TraceAddress)
	}

	m["trace_meta.trace_idx"] = traceIdx
	m["trace_meta.msg_sender"] = msgSender
	m["trace_meta.error"] = errors
	m["trace_meta.subtraces"] = subtraces
	m["trace_meta.trace_address"] = traceAddress

	// Process clickhouse decoded call data.
	decodedData := NewClickhouseDecodedCallData(&t)
	m["trace_decoded_data.trace_idx"] = decodedData.TraceIdx
	m["trace_decoded_data.function_name"] = decodedData.FunctionName
	m["trace_decoded_data.call_data"] = decodedData.CallData
	m["trace_decoded_data.return_data"] = decodedData.ReturnData

	// Process clickhouse logs.
	logs := NewClickhouseLogs(&t)
	m["trace_logs.trace_idx"] = logs.TraceIdx
	m["trace_logs.log_idx"] = logs.LogIdx
	m["trace_logs.address"] = logs.Address
	m["trace_logs.topics"] = logs.Topics
	m["trace_logs.data"] = logs.Data

	// Process clickhouse create action.
	createAction := NewClickhouseCreateAction(&t)
	m["trace_create_actions.trace_idx"] = createAction.TraceIdx
	m["trace_create_actions.from"] = createAction.From
	m["trace_create_actions.gas"] = createAction.Gas
	m["trace_create_actions.init"] = createAction.Init
	m["trace_create_actions.value"] = createAction.Value

	// Process clickhouse call action.
	callAction := NewClickhouseCallAction(&t)
	m["trace_call_actions.trace_idx"] = callAction.TraceIdx
	m["trace_call_actions.from"] = callAction.From
	m["trace_call_actions.call_type"] = callAction.CallType
	m["trace_call_actions.gas"] = callAction.Gas
	m["trace_call_actions.input"] = callAction.Input
	m["trace_call_actions.to"] = callAction.To
	m["trace_call_actions.value"] = callAction.Value

	// Process clickhouse self-destruct action.
	selfDestructAction := NewClickhouseSelfDestructAction(&t)
	m["trace_self_destruct_actions.trace_idx"] = selfDestructAction.TraceIdx
	m["trace_self_destruct_actions.address"] = selfDestructAction.Address
	m["trace_self_destruct_actions.balance"] = selfDestructAction.Balance
	m["trace_self_destruct_actions.refund_address"] = selfDestructAction.RefundAddress

	// Process clickhouse reward action.
	rewardAction := NewClickhouseRewardAction(&t)
	m["trace_reward_actions.trace_idx"] = rewardAction.TraceIdx
	m["trace_reward_actions.author"] = rewardAction.Author
	m["trace_reward_actions.value"] = rewardAction.Value
	m["trace_reward_actions.reward_type"] = rewardAction.RewardType

	// Process clickhouse call output.
	callOutput := NewClickhouseCallOutput(&t)
	m["trace_call_outputs.trace_idx"] = callOutput.TraceIdx
	m["trace_call_outputs.gas_used"] = callOutput.GasUsed
	m["trace_call_outputs.output"] = callOutput.Output

	// Process clickhouse create output.
	createOutput := NewClickhouseCreateOutput(&t)
	m["trace_create_outputs.trace_idx"] = createOutput.TraceIdx
	m["trace_create_outputs.address"] = createOutput.Address
	m["trace_create_outputs.code"] = createOutput.Code
	m["trace_create_outputs.gas_used"] = createOutput.GasUsed

	return json.Marshal(m)
}

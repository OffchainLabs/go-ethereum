package brontes

import (
	"fmt"
)

// ClickhouseDecodedCallData represents decoded function call data for ClickHouse
type ClickhouseDecodedCallData struct {
	TraceIdx     []uint64
	FunctionName []string
	CallData     [][]DecodedParams
	ReturnData   [][]DecodedParams
}

// NewClickhouseDecodedCallData creates a ClickhouseDecodedCallData from a TxTrace
func NewClickhouseDecodedCallData(value *TxTrace) *ClickhouseDecodedCallData {
	result := &ClickhouseDecodedCallData{}
	for _, trace := range value.Trace {
		if trace.DecodedData != nil {
			result.TraceIdx = append(result.TraceIdx, trace.TraceIdx)
			result.FunctionName = append(result.FunctionName, trace.DecodedData.FunctionName)
			result.CallData = append(result.CallData, trace.DecodedData.CallData)
			result.ReturnData = append(result.ReturnData, trace.DecodedData.ReturnData)
		}
	}
	return result
}

// ClickhouseLogs represents transaction logs for ClickHouse
type ClickhouseLogs struct {
	TraceIdx []uint64
	LogIdx   []uint64
	Address  []string
	Topics   [][]string
	Data     []string
}

// NewClickhouseLogs creates a ClickhouseLogs from a TxTrace
func NewClickhouseLogs(value *TxTrace) *ClickhouseLogs {
	result := &ClickhouseLogs{}
	for _, trace := range value.Trace {
		for logIdx, log := range trace.Logs {
			result.TraceIdx = append(result.TraceIdx, trace.TraceIdx)
			result.LogIdx = append(result.LogIdx, uint64(logIdx))
			result.Address = append(result.Address, log.Address.String())

			// Convert topics to strings
			topicStrings := make([]string, len(log.Topics))
			for i, topic := range log.Topics {
				topicStrings[i] = topic.String()
			}
			result.Topics = append(result.Topics, topicStrings)

			result.Data = append(result.Data, fmt.Sprintf("%x", log.Data))
		}
	}
	return result
}

// ClickhouseCreateAction represents contract creation actions for ClickHouse
type ClickhouseCreateAction struct {
	TraceIdx []uint64
	From     []string
	Gas      []uint64
	Init     []string
	Value    [][32]byte
}

// NewClickhouseCreateAction creates a ClickhouseCreateAction from a TxTrace
func NewClickhouseCreateAction(value *TxTrace) *ClickhouseCreateAction {
	result := &ClickhouseCreateAction{}
	for _, trace := range value.Trace {
		if trace.IsCreate() {
			if createAction, ok := trace.Trace.Action.(*CreateAction); ok {
				result.TraceIdx = append(result.TraceIdx, trace.TraceIdx)
				result.From = append(result.From, createAction.From.String())
				result.Gas = append(result.Gas, createAction.Gas)
				result.Init = append(result.Init, fmt.Sprintf("%x", createAction.Init))

				// Convert big.Int to [32]byte
				var valueBytes [32]byte
				createAction.Value.FillBytes(valueBytes[:])
				result.Value = append(result.Value, valueBytes)
			}
		}
	}
	return result
}

// ClickhouseCallAction represents contract call actions for ClickHouse
type ClickhouseCallAction struct {
	TraceIdx []uint64
	From     []string
	CallType []string
	Gas      []uint64
	Input    []string
	To       []string
	Value    [][32]byte
}

// NewClickhouseCallAction creates a ClickhouseCallAction from a TxTrace
func NewClickhouseCallAction(value *TxTrace) *ClickhouseCallAction {
	result := &ClickhouseCallAction{}
	for _, trace := range value.Trace {
		if callAction, ok := trace.Trace.Action.(*CallAction); ok {
			result.TraceIdx = append(result.TraceIdx, trace.TraceIdx)
			result.From = append(result.From, callAction.From.String())
			result.CallType = append(result.CallType, callAction.CallType.String())
			result.Gas = append(result.Gas, callAction.Gas)
			result.Input = append(result.Input, fmt.Sprintf("%x", callAction.Input))
			result.To = append(result.To, callAction.To.String())

			// Convert big.Int to [32]byte
			var valueBytes [32]byte
			callAction.Value.FillBytes(valueBytes[:])
			result.Value = append(result.Value, valueBytes)
		}
	}
	return result
}

// ClickhouseSelfDestructAction represents self-destruct actions for ClickHouse
type ClickhouseSelfDestructAction struct {
	TraceIdx      []uint64
	Address       []string
	Balance       [][32]byte
	RefundAddress []string
}

// NewClickhouseSelfDestructAction creates a ClickhouseSelfDestructAction from a TxTrace
func NewClickhouseSelfDestructAction(value *TxTrace) *ClickhouseSelfDestructAction {
	result := &ClickhouseSelfDestructAction{}
	for _, trace := range value.Trace {
		if selfDestruct, ok := trace.Trace.Action.(*SelfdestructAction); ok {
			result.TraceIdx = append(result.TraceIdx, trace.TraceIdx)
			result.Address = append(result.Address, selfDestruct.Address.String())
			result.RefundAddress = append(result.RefundAddress, selfDestruct.RefundAddress.String())

			// Convert big.Int to [32]byte
			var balanceBytes [32]byte
			selfDestruct.Balance.FillBytes(balanceBytes[:])
			result.Balance = append(result.Balance, balanceBytes)
		}
	}
	return result
}

// ClickhouseRewardAction represents reward actions for ClickHouse
type ClickhouseRewardAction struct {
	TraceIdx   []uint64
	Author     []string
	Value      [][32]byte
	RewardType []string
}

// NewClickhouseRewardAction creates a ClickhouseRewardAction from a TxTrace
func NewClickhouseRewardAction(value *TxTrace) *ClickhouseRewardAction {
	result := &ClickhouseRewardAction{}
	for _, trace := range value.Trace {
		if rewardAction, ok := trace.Trace.Action.(*RewardAction); ok {
			result.TraceIdx = append(result.TraceIdx, trace.TraceIdx)
			result.Author = append(result.Author, rewardAction.Author.String())

			// Convert RewardType to string
			var rewardTypeStr string
			if rewardAction.RewardType == RewardTypeBlock {
				rewardTypeStr = "Block"
			} else {
				rewardTypeStr = "Uncle"
			}
			result.RewardType = append(result.RewardType, rewardTypeStr)

			// Convert big.Int to [32]byte
			var valueBytes [32]byte
			rewardAction.Value.FillBytes(valueBytes[:])
			result.Value = append(result.Value, valueBytes)
		}
	}
	return result
}

// ClickhouseCallOutput represents call outputs for ClickHouse
type ClickhouseCallOutput struct {
	TraceIdx []uint64
	GasUsed  []uint64
	Output   []string
}

// NewClickhouseCallOutput creates a ClickhouseCallOutput from a TxTrace
func NewClickhouseCallOutput(value *TxTrace) *ClickhouseCallOutput {
	result := &ClickhouseCallOutput{}
	for _, trace := range value.Trace {
		if trace.Trace.Result != nil && trace.Trace.Result.Type == TraceOutputTypeCall && trace.Trace.Result.Call != nil {
			callOutput := trace.Trace.Result.Call
			result.TraceIdx = append(result.TraceIdx, trace.TraceIdx)
			result.GasUsed = append(result.GasUsed, callOutput.GasUsed)
			result.Output = append(result.Output, fmt.Sprintf("%x", callOutput.Output))
		}
	}
	return result
}

// ClickhouseCreateOutput represents contract creation outputs for ClickHouse
type ClickhouseCreateOutput struct {
	TraceIdx []uint64
	Address  []string
	Code     []string
	GasUsed  []uint64
}

// NewClickhouseCreateOutput creates a ClickhouseCreateOutput from a TxTrace
func NewClickhouseCreateOutput(value *TxTrace) *ClickhouseCreateOutput {
	result := &ClickhouseCreateOutput{}
	for _, trace := range value.Trace {
		if trace.Trace.Result != nil && trace.Trace.Result.Type == TraceOutputTypeCreate && trace.Trace.Result.Create != nil {
			createOutput := trace.Trace.Result.Create
			result.TraceIdx = append(result.TraceIdx, trace.TraceIdx)
			result.Address = append(result.Address, createOutput.Address.String())
			result.Code = append(result.Code, fmt.Sprintf("%x", createOutput.Code))
			result.GasUsed = append(result.GasUsed, createOutput.GasUsed)
		}
	}
	return result
}

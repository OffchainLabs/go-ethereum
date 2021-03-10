package deepmind

import (
	"math/big"

	"github.com/golang-collections/collections/stack"
)

var EmptyValue = new(big.Int)

type logItem = map[string]interface{}

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

// BalanceChangeReason denotes a reason why a given balance change occurred.
//
// **Important!** For easier extraction of all possible `BalanceChangeReason`, ensure you always
//                define valid value using the type wrapper so it matches the extraction
//                regex `BalanceChangeReason\("[a-z0-9_]+"\)`. All other values that should not
//                be matched can be defined here using `var X BalanceChangeReason = "something"`
//                since does not match the above regexp.
type BalanceChangeReason string

// IgnoredBalanceChangeReason **On purposely defined using a different syntax, check `BalanceChangeReason` type doc above**
var IgnoredBalanceChangeReason BalanceChangeReason = "ignored"

// GasChangeReason denotes a reason why a given gas cost was incurred for an operation.
//
// **Important!** For easier extraction of all possible `GasChangeReason`, ensure you always
//                define valid value using the type wrapper so it matches the extraction
//                regex `GasChangeReason\("[a-z0-9_]+"\)`. All other values that should not
//                be matched can be defined here using `var X GasChangeReason = "something"`
//                since does not match the above regexp.
type GasChangeReason string

// RefundAfterExecutionGasChangeReason to be used for all gas refund operation
var RefundAfterExecutionGasChangeReason = GasChangeReason("refund_after_execution")

// FailedExecutionGasChangeReason to be used for all call failure remaining gas burning operation
var FailedExecutionGasChangeReason = GasChangeReason("failed_execution")

// IgnoredGasChangeReason **On purposely defined using a different syntax, check `GasChangeReason` type doc above**
var IgnoredGasChangeReason GasChangeReason = "ignored"

// GasEventID denotes the id of the following gas event. Gas events are
// there to record gas at various location in the execution. For now,
// there is After/Before event pair for the root call and After/Before
// events for each child and sub-children call.
//
// **Important!** For easier extraction of all possible `GasEventID`, ensure you always
//                define valid value using the type wrapper so it matches the extraction
//                regex `GasEventID\("[a-z0-9_]+"\)`. All other values that should not
//                be matched can be defined here using `var X GasEventID = "something"`
//                since does not match the above regexp.
type GasEventID string

var BeforeCallGasEventID = GasEventID("before_call")
var AfterCallGasEventID = GasEventID("after_call")

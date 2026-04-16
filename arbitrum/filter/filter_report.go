package filter

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

type FilterReasonType string

const (
	ReasonFrom                          FilterReasonType = "from"
	ReasonTo                            FilterReasonType = "to"
	ReasonDealiasedFrom                 FilterReasonType = "dealiased_from"
	ReasonRetryableBeneficiary          FilterReasonType = "retryable_beneficiary"
	ReasonRetryableFeeRefund            FilterReasonType = "retryable_fee_refund"
	ReasonRetryableTo                   FilterReasonType = "retryable_to"
	ReasonDealiasedRetryableBeneficiary FilterReasonType = "dealiased_retryable_beneficiary"
	ReasonDealiasedRetryableFeeRefund   FilterReasonType = "dealiased_retryable_fee_refund"
	ReasonEventRule                     FilterReasonType = "event_rule"
	ReasonContractAddress               FilterReasonType = "contract_address"
	ReasonContractCaller                FilterReasonType = "contract_caller"
	ReasonSelfdestructBeneficiary       FilterReasonType = "selfdestruct_beneficiary"
)

// lint:require-exhaustive-initialization
type RawLog struct {
	Address common.Address `json:"address"`
	Topics  []common.Hash  `json:"topics"`
	Data    hexutil.Bytes  `json:"data"`
}

// lint:require-exhaustive-initialization
type EventRuleMatch struct {
	MatchedEvent      string  `json:"matchedEvent"`
	MatchedTopicIndex int     `json:"matchedTopicIndex,omitempty"`
	RawLog            *RawLog `json:"rawLog,omitempty"`
}

// lint:require-exhaustive-initialization
type FilterReason struct {
	Reason FilterReasonType `json:"reason"`
	*EventRuleMatch
}

// lint:require-exhaustive-initialization
type FilteredAddressRecord struct {
	Address common.Address `json:"address"`
	FilterReason
}

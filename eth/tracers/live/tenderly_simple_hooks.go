package live

import (
	"encoding/json"
	gethtracing "github.com/ethereum/go-ethereum/core/tracing"
	gethtracers "github.com/ethereum/go-ethereum/eth/tracers"
)

// init registers the tenderly tracer hooks with the gethtracers.LiveDirectory
func init() {
	gethtracers.LiveDirectory.Register("tenderly_simple_tracer_hooks", NewTenderlySimpleTracerHooks)
}

// NewTenderlySimpleTracerHooks creates a new set of tracer hooks for the tenderly tracer.
func NewTenderlySimpleTracerHooks(jsonConfig json.RawMessage) (*gethtracing.Hooks, error) {
	// tracer
	tracer := NewTenderlySimpleTracer()

	// hook the tracer
	return &gethtracing.Hooks{
		OnTxStart: tracer.OnTxStart,
		OnTxEnd:   tracer.OnTxEnd,
		OnEnter:   tracer.OnEnter,
		OnExit:    tracer.OnExit,
		OnOpcode:  tracer.OnOpcode,

		OnClose:           tracer.OnClose,
		OnBlockStart:      tracer.OnBlockStart,
		OnBlockEnd:        tracer.OnBlockEnd,
		OnSystemCallStart: tracer.OnSystemCallStart,
		OnSystemCallEnd:   tracer.OnSystemCallEnd,
		//OnSystemCallStartV2: tracer.OnSystemCallStartV2,

		OnBalanceChange: tracer.OnBalanceChange,
		OnNonceChange:   tracer.OnNonceChange,
		OnCodeChange:    tracer.OnCodeChange,
		OnStorageChange: tracer.OnStorageChange,

		//OnBalanceRead:  inner.OnBalanceRead,
		//OnNonceRead:    inner.OnNonceRead,
		//OnCodeRead:     inner.OnCodeRead,
		//OnCodeSizeRead: inner.OnCodeSizeRead,
		//OnCodeHashRead: inner.OnCodeHashRead,
		//OnStorageRead:  inner.OnStorageRead,
	}, nil
}

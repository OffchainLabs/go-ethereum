package arbitrum

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

type ArbTransactionAPI struct {
	b *APIBackend
}

func NewArbTransactionAPI(b *APIBackend) *ArbTransactionAPI {
	return &ArbTransactionAPI{b}
}

func (s *ArbTransactionAPI) SendRawTransactionConditional(ctx context.Context, input hexutil.Bytes, options *ConditionalOptions) (common.Hash, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(input); err != nil {
		return common.Hash{}, err
	}
	return SubmitConditionalTransaction(ctx, s.b, tx, options)
}

func SubmitConditionalTransaction(ctx context.Context, b *APIBackend, tx *types.Transaction, options *ConditionalOptions) (common.Hash, error) {
	// If the transaction fee cap is already specified, ensure the
	// fee of the given transaction is _reasonable_.
	if err := ethapi.CheckTxFee(tx.GasPrice(), tx.Gas(), b.RPCTxFeeCap()); err != nil {
		return common.Hash{}, err
	}
	if !b.UnprotectedAllowed() && !tx.Protected() {
		// Ensure only eip155 signed transactions are submitted if EIP155Required is set.
		return common.Hash{}, errors.New("only replay-protected (EIP-155) transactions allowed over RPC")
	}
	if err := b.SendConditionalTx(ctx, tx, options); err != nil {
		return common.Hash{}, err
	}
	// Print a log with full tx details for manual investigations and interventions
	signer := types.MakeSigner(b.ChainConfig(), b.CurrentBlock().Number())
	from, err := types.Sender(signer, tx)
	if err != nil {
		return common.Hash{}, err
	}

	if tx.To() == nil {
		addr := crypto.CreateAddress(from, tx.Nonce())
		log.Info("Submitted contract creation", "hash", tx.Hash().Hex(), "from", from, "nonce", tx.Nonce(), "contract", addr.Hex(), "value", tx.Value())
	} else {
		log.Info("Submitted transaction", "hash", tx.Hash().Hex(), "from", from, "nonce", tx.Nonce(), "recipient", tx.To(), "value", tx.Value())
	}
	return tx.Hash(), nil
}

type RootHashOrSlots struct {
	RootHash  *common.Hash
	SlotValue map[common.Hash]common.Hash
}

func (r *RootHashOrSlots) UnmarshalJSON(data []byte) error {
	var hash common.Hash
	var err error
	if err = json.Unmarshal(data, &hash); err == nil {
		r.RootHash = &hash
		return nil
	}
	return json.Unmarshal(data, &r.SlotValue)
}

func (r RootHashOrSlots) MarshalJSON() ([]byte, error) {
	if r.RootHash != nil {
		return json.Marshal(*r.RootHash)
	}
	if len(r.SlotValue) > 0 {
		slotValue := r.SlotValue
		return json.Marshal(slotValue)
	}
	return nil, nil
}

// TODO rename to just "Options" ?
type ConditionalOptions struct {
	KnownAccounts map[common.Address]RootHashOrSlots `json:"knownAccounts"`
}

func SendConditionalTransactionRPC(ctx context.Context, rpc *rpc.Client, tx *types.Transaction, options *ConditionalOptions) error {
	data, err := tx.MarshalBinary()
	if err != nil {
		return err
	}
	return rpc.CallContext(ctx, nil, "eth_sendRawTransactionConditional", hexutil.Encode(data), options)
}

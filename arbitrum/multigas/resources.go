package multigas

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/bits"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
)

// ResourceKind represents a dimension for the multi-dimensional gas.
type ResourceKind uint8

//go:generate stringer -type=ResourceKind -trimprefix=ResourceKind
const (
	ResourceKindUnknown ResourceKind = iota
	ResourceKindComputation
	ResourceKindHistoryGrowth
	ResourceKindStorageAccess
	ResourceKindStorageGrowth
	ResourceKindL1Calldata
	ResourceKindL2Calldata
	ResourceKindWasmComputation
	NumResourceKind
)

// CheckResourceKind checks whether the given id is a valid resource.
func CheckResourceKind(id uint8) (ResourceKind, error) {
	if id <= uint8(ResourceKindUnknown) || id >= uint8(NumResourceKind) {
		return ResourceKindUnknown, fmt.Errorf("invalid resource id: %v", id)
	}
	return ResourceKind(id), nil
}

// MultiGas tracks gas usage across multiple resource kinds, while also
// maintaining a single-dimensional total gas sum and refund amount.
type MultiGas struct {
	gas    [NumResourceKind]uint64
	total  uint64
	refund uint64
}

// Pair represents a single resource kind and its associated gas amount.
type Pair struct {
	Kind   ResourceKind
	Amount uint64
}

// ZeroGas creates a MultiGas value with all fields set to zero.
func ZeroGas() MultiGas {
	return MultiGas{}
}

// NewMultiGas creates a new MultiGas with the given resource kind initialized to `amount`.
// All other kinds are zero. The total is also set to `amount`.
func NewMultiGas(kind ResourceKind, amount uint64) MultiGas {
	var mg MultiGas
	mg.gas[kind] = amount
	mg.total = amount
	return mg
}

// MultiGasFromPairs creates a new MultiGas from resource–amount pairs.
// Intended for constant-like construction; panics on overflow.
func MultiGasFromPairs(pairs ...Pair) MultiGas {
	var mg MultiGas
	for _, p := range pairs {
		mg.gas[p.Kind] = p.Amount
	}
	if mg.recomputeTotal() {
		panic("multigas overflow")
	}
	return mg
}

// ComputationGas returns a MultiGas initialized with computation gas.
func ComputationGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindComputation, amount)
}

// HistoryGrowthGas returns a MultiGas initialized with history growth gas.
func HistoryGrowthGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindHistoryGrowth, amount)
}

// StorageAccessGas returns a MultiGas initialized with storage access gas.
func StorageAccessGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindStorageAccess, amount)
}

// StorageGrowthGas returns a MultiGas initialized with storage growth gas.
func StorageGrowthGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindStorageGrowth, amount)
}

// L1CalldataGas returns a MultiGas initialized with L1 calldata gas.
func L1CalldataGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindL1Calldata, amount)
}

// L2CalldataGas returns a MultiGas initialized with L2 calldata gas.
func L2CalldataGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindL2Calldata, amount)
}

// WasmComputationGas returns a MultiGas initialized with computation gas used for WASM (Stylus contracts).
func WasmComputationGas(amount uint64) MultiGas {
	return NewMultiGas(ResourceKindWasmComputation, amount)
}

// Get returns the gas amount for the specified resource kind.
func (z MultiGas) Get(kind ResourceKind) uint64 {
	return z.gas[kind]
}

// With returns a copy of z with the given resource kind set to amount.
// The total is adjusted accordingly. It returns the updated value and true if an overflow occurred.
func (z MultiGas) With(kind ResourceKind, amount uint64) (MultiGas, bool) {
	res, overflow := z, false

	res.total, overflow = saturatingScalarAdd(z.total-z.gas[kind], amount)
	if overflow {
		return z, true
	}

	res.gas[kind] = amount
	return res, false
}

// GetRefund gets the SSTORE refund computed at the end of the transaction.
func (z MultiGas) GetRefund() uint64 {
	return z.refund
}

// WithRefund returns a copy of z with its refund set to amount.
func (z MultiGas) WithRefund(amount uint64) MultiGas {
	res := z
	res.refund = amount
	return res
}

// SafeAdd returns a copy of z with the per-kind, total, and refund gas
// added to the values from x. It returns the updated value and true if
// an overflow occurred.
func (z MultiGas) SafeAdd(x MultiGas) (MultiGas, bool) {
	res, overflow := z, false

	for i := 0; i < int(NumResourceKind); i++ {
		res.gas[i], overflow = saturatingScalarAdd(res.gas[i], x.gas[i])
		if overflow {
			return z, true
		}
	}

	res.total, overflow = saturatingScalarAdd(res.total, x.total)
	if overflow {
		return z, true
	}
	res.refund, overflow = saturatingScalarAdd(res.refund, x.refund)
	if overflow {
		return z, true
	}

	return res, false
}

// SaturatingAdd returns a copy of z with the per-kind, total, and refund gas
// added to the values from x. On overflow, the affected field(s) are clamped
// to MaxUint64.
func (z MultiGas) SaturatingAdd(x MultiGas) MultiGas {
	res := z

	for i := 0; i < int(NumResourceKind); i++ {
		res.gas[i], _ = saturatingScalarAdd(res.gas[i], x.gas[i])
	}

	res.total, _ = saturatingScalarAdd(res.total, x.total)
	res.refund, _ = saturatingScalarAdd(res.refund, x.refund)
	return res
}

// SaturatingAddInto adds x into z in place (per kind, total, and refund).
// On overflow, the affected field(s) are clamped to MaxUint64.
// This is a hot-path helper; the public immutable API remains preferred elsewhere.
func (z *MultiGas) SaturatingAddInto(x MultiGas) {
	for i := 0; i < int(NumResourceKind); i++ {
		z.gas[i], _ = saturatingScalarAdd(z.gas[i], x.gas[i])
	}
	z.total, _ = saturatingScalarAdd(z.total, x.total)
	z.refund, _ = saturatingScalarAdd(z.refund, x.refund)
}

// SafeSub returns a copy of z with the per-kind, total, and refund gas
// subtracted by the values from x. It returns the updated value and true if
// a underflow occurred.
func (z MultiGas) SafeSub(x MultiGas) (MultiGas, bool) {
	res, underflow := z, false

	for i := 0; i < int(NumResourceKind); i++ {
		res.gas[i], underflow = saturatingScalarSub(res.gas[i], x.gas[i])
		if underflow {
			return z, true
		}
	}

	res.refund, underflow = saturatingScalarSub(res.refund, x.refund)
	if underflow {
		return z, true
	}

	res.recomputeTotal()

	return res, false
}

// SaturatingSub returns a copy of z with the per-kind, total, and refund gas
// subtracted by the values from x. On underflow, the affected field(s) are
// clamped to zero.
func (z MultiGas) SaturatingSub(x MultiGas) MultiGas {
	res := z
	for i := 0; i < int(NumResourceKind); i++ {
		res.gas[i], _ = saturatingScalarSub(res.gas[i], x.gas[i])
	}
	res.refund, _ = saturatingScalarSub(res.refund, x.refund)
	res.recomputeTotal()
	return res
}

// SafeIncrement returns a copy of z with the given resource kind
// and the total incremented by gas. It returns the updated value and true if
// an overflow occurred.
func (z MultiGas) SafeIncrement(kind ResourceKind, gas uint64) (MultiGas, bool) {
	res, overflow := z, false

	res.gas[kind], overflow = saturatingScalarAdd(z.gas[kind], gas)
	if overflow {
		return z, true
	}

	res.total, overflow = saturatingScalarAdd(z.total, gas)
	if overflow {
		return z, true
	}

	return res, false
}

// SaturatingIncrement returns a copy of z with the given resource kind
// and the total incremented by gas. On overflow, the field(s) are clamped to MaxUint64.
func (z MultiGas) SaturatingIncrement(kind ResourceKind, gas uint64) MultiGas {
	res := z
	res.gas[kind], _ = saturatingScalarAdd(z.gas[kind], gas)
	res.total, _ = saturatingScalarAdd(z.total, gas)
	return res
}

// SaturatingIncrementInto increments the given resource kind and the total
// in place by gas. On overflow, the affected field(s) are clamped to MaxUint64.
// Unlike SaturatingIncrement, this method mutates the receiver directly and
// is intended for VM hot paths where avoiding value copies is critical.
func (z *MultiGas) SaturatingIncrementInto(kind ResourceKind, gas uint64) {
	z.gas[kind], _ = saturatingScalarAdd(z.gas[kind], gas)
	z.total, _ = saturatingScalarAdd(z.total, gas)
}

// SingleGas returns the single-dimensional total gas.
func (z MultiGas) SingleGas() uint64 {
	return z.total - z.refund
}

func (z MultiGas) IsZero() bool {
	return z.total == 0 && z.refund == 0 && z.gas == [NumResourceKind]uint64{}
}

// multiGasJSON is an auxiliary type for JSON marshaling/unmarshaling of MultiGas.
type multiGasJSON struct {
	Unknown         hexutil.Uint64 `json:"unknown"`
	Computation     hexutil.Uint64 `json:"computation"`
	HistoryGrowth   hexutil.Uint64 `json:"historyGrowth"`
	StorageAccess   hexutil.Uint64 `json:"storageAccess"`
	StorageGrowth   hexutil.Uint64 `json:"storageGrowth"`
	L1Calldata      hexutil.Uint64 `json:"l1Calldata"`
	L2Calldata      hexutil.Uint64 `json:"l2Calldata"`
	WasmComputation hexutil.Uint64 `json:"wasmComputation"`
	Refund          hexutil.Uint64 `json:"refund"`
	Total           hexutil.Uint64 `json:"total"`
}

// MarshalJSON implements json.Marshaler for MultiGas.
func (z MultiGas) MarshalJSON() ([]byte, error) {
	return json.Marshal(multiGasJSON{
		Unknown:         hexutil.Uint64(z.gas[ResourceKindUnknown]),
		Computation:     hexutil.Uint64(z.gas[ResourceKindComputation]),
		HistoryGrowth:   hexutil.Uint64(z.gas[ResourceKindHistoryGrowth]),
		StorageAccess:   hexutil.Uint64(z.gas[ResourceKindStorageAccess]),
		StorageGrowth:   hexutil.Uint64(z.gas[ResourceKindStorageGrowth]),
		L1Calldata:      hexutil.Uint64(z.gas[ResourceKindL1Calldata]),
		L2Calldata:      hexutil.Uint64(z.gas[ResourceKindL2Calldata]),
		WasmComputation: hexutil.Uint64(z.gas[ResourceKindWasmComputation]),
		Refund:          hexutil.Uint64(z.refund),
		Total:           hexutil.Uint64(z.total),
	})
}

// UnmarshalJSON implements json.Unmarshaler for MultiGas.
func (z *MultiGas) UnmarshalJSON(data []byte) error {
	var j multiGasJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	*z = ZeroGas()
	z.gas[ResourceKindUnknown] = uint64(j.Unknown)
	z.gas[ResourceKindComputation] = uint64(j.Computation)
	z.gas[ResourceKindHistoryGrowth] = uint64(j.HistoryGrowth)
	z.gas[ResourceKindStorageAccess] = uint64(j.StorageAccess)
	z.gas[ResourceKindStorageGrowth] = uint64(j.StorageGrowth)
	z.gas[ResourceKindL1Calldata] = uint64(j.L1Calldata)
	z.gas[ResourceKindL2Calldata] = uint64(j.L2Calldata)
	z.gas[ResourceKindWasmComputation] = uint64(j.WasmComputation)
	z.refund = uint64(j.Refund)
	z.total = uint64(j.Total)
	return nil
}

// EncodeRLP encodes MultiGas as:
// [ total, refund, gas[0], gas[1], ..., gas[NumResourceKind-1] ]
func (z *MultiGas) EncodeRLP(w io.Writer) error {
	enc := rlp.NewEncoderBuffer(w)
	l := enc.List()

	enc.WriteUint64(z.total)
	enc.WriteUint64(z.refund)
	for i := 0; i < int(NumResourceKind); i++ {
		enc.WriteUint64(z.gas[i])
	}

	enc.ListEnd(l)
	return enc.Flush()
}

// DecodeRLP decodes MultiGas in a forward/backward-compatible way.
// Extra per-dimension entries are skipped; missing ones are treated as zero.
func (z *MultiGas) DecodeRLP(s *rlp.Stream) error {
	if _, err := s.List(); err != nil {
		return err
	}

	total, err := s.Uint64()
	if err != nil {
		return err
	}
	refund, err := s.Uint64()
	if err != nil {
		return err
	}

	for i := 0; ; i++ {
		val, err := s.Uint64()
		if err == rlp.EOL {
			break // end of list
		}
		if err != nil {
			return err
		}
		if i < int(NumResourceKind) {
			z.gas[i] = val
		}
		// if i >= NumResourceKind, just skip extra lines
	}

	if err := s.ListEnd(); err != nil {
		return err
	}

	z.total = total
	z.refund = refund
	return nil
}

// recomputeTotal recomputes the total gas from the per-kind amounts. Returns
// true if an overflow occurred (and the total was set to MaxUint64).
func (z *MultiGas) recomputeTotal() (overflow bool) {
	z.total = 0
	for i := 0; i < int(NumResourceKind); i++ {
		z.total, overflow = saturatingScalarAdd(z.total, z.gas[i])
		if overflow {
			return
		}
	}
	return
}

// saturatingScalarAdd adds two uint64 values, returning the sum and a boolean
// indicating whether an overflow occurred. If an overflow occurs, the sum is
// set to math.MaxUint64.
func saturatingScalarAdd(a, b uint64) (uint64, bool) {
	sum, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64, true
	}
	return sum, false
}

// saturatingScalarSub subtracts two uint64 values, returning the difference and a boolean
// indicating whether an underflow occurred. If an underflow occurs, the difference is
// set to 0.
func saturatingScalarSub(a, b uint64) (uint64, bool) {
	diff, borrow := bits.Sub64(a, b, 0)
	if borrow != 0 {
		return 0, true
	}
	return diff, false
}

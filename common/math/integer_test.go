// Copyright 2017 The go-ethereum Authors
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

package math

import (
	"encoding/json"
	"testing"
)

type operation byte

const (
	sub operation = iota
	add
	mul
)

func TestOverflow(t *testing.T) {
	for i, test := range []struct {
		x        uint64
		y        uint64
		overflow bool
		op       operation
	}{
		// add operations
		{MaxUint64, 1, true, add},
		{MaxUint64 - 1, 1, false, add},

		// sub operations
		{0, 1, true, sub},
		{0, 0, false, sub},

		// mul operations
		{0, 0, false, mul},
		{10, 10, false, mul},
		{MaxUint64, 2, true, mul},
		{MaxUint64, 1, false, mul},
	} {
		var overflows bool
		switch test.op {
		case sub:
			_, overflows = SafeSub(test.x, test.y)
		case add:
			_, overflows = SafeAdd(test.x, test.y)
		case mul:
			_, overflows = SafeMul(test.x, test.y)
		}

		if test.overflow != overflows {
			t.Errorf("%d failed. Expected test to be %v, got %v", i, test.overflow, overflows)
		}
	}
}

func TestHexOrDecimal64(t *testing.T) {
	tests := []struct {
		input string
		num   uint64
		ok    bool
	}{
		{"", 0, true},
		{"0", 0, true},
		{"0x0", 0, true},
		{"12345678", 12345678, true},
		{"0x12345678", 0x12345678, true},
		{"0X12345678", 0x12345678, true},
		// Tests for leading zero behaviour:
		{"0123456789", 123456789, true}, // note: not octal
		{"0x00", 0, true},
		{"0x012345678abc", 0x12345678abc, true},
		// Invalid syntax:
		{"abcdef", 0, false},
		{"0xgg", 0, false},
		// Doesn't fit into 64 bits:
		{"18446744073709551617", 0, false},
	}
	for _, test := range tests {
		var num HexOrDecimal64
		err := num.UnmarshalText([]byte(test.input))
		if (err == nil) != test.ok {
			t.Errorf("ParseUint64(%q) -> (err == nil) = %t, want %t", test.input, err == nil, test.ok)
			continue
		}
		if err == nil && uint64(num) != test.num {
			t.Errorf("ParseUint64(%q) -> %d, want %d", test.input, num, test.num)
		}
	}
}

func TestMustParseUint64(t *testing.T) {
	if v := MustParseUint64("12345"); v != 12345 {
		t.Errorf(`MustParseUint64("12345") = %d, want 12345`, v)
	}
}

func TestMustParseUint64Panic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParseBig should've panicked")
		}
	}()
	MustParseUint64("ggg")
}

type marshalUnMarshalTest struct {
	input   interface{}
	want    interface{}
	wantErr bool // if true, decoding must fail on any platform
}

var (
	marshalHexOrDecimal64Tests = []marshalUnMarshalTest{
		{input: uint64(0), want: "0x0"},
		{input: uint64(1), want: "0x1"},
		{input: uint64(16), want: "0x10"},
		{input: uint64(255), want: "0xff"},
		{input: uint64(0xff), want: "0xff"},
		{input: uint64(0x1122334455667788), want: "0x1122334455667788"},
	}

	UnMarshalHexOrDecimal64Tests = []marshalUnMarshalTest{
		// invalid encoding
		{input: "", wantErr: true},
		{input: "null", wantErr: true},
		{input: "\"0x\"", wantErr: true},
		{input: "\"0xfffffffffffffffff\"", wantErr: true},
		{input: `"1ab"`, wantErr: true},
		{input: "\"xx\"", wantErr: true},
		{input: "\"0x1zz01\"", wantErr: true},

		// valid encoding
		{input: `""`, want: uint64(0)},
		{input: `"0"`, want: uint64(0)},
		{input: `"10"`, want: uint64(10)},
		{input: `"100"`, want: uint64(100)},
		{input: `"12344678"`, want: uint64(12344678)},
		{input: `"1111111111111111"`, want: uint64(1111111111111111)},
		{input: "\"0x0\"", want: uint64(0)},
		{input: "\"0x2\"", want: uint64(0x2)},
		{input: "\"0x2F2\"", want: uint64(0x2f2)},
		{input: "\"0x1122aaff\"", want: uint64(0x1122aaff)},
		{input: "\"0xbbb\"", want: uint64(0xbbb)},
		{input: "\"0xffffffffffffffff\"", want: uint64(0xffffffffffffffff)},
	}
)

func TestMarshalHexOrDecimal64(t *testing.T) {
	for _, test := range marshalHexOrDecimal64Tests {
		in := test.input.(uint64)
		out, err := json.Marshal(HexOrDecimal64(in))
		if err != nil {
			t.Errorf("%d: %v", in, err)
			continue
		}
		if want := `"` + test.want.(string) + `"`; string(out) != want {
			t.Errorf("%d: MarshalJSON output mismatch: got %q, want %q", in, out, want)
			continue
		}
	}
}

func TestUnMarshalHexOrDecimal64(t *testing.T) {
	for _, test := range UnMarshalHexOrDecimal64Tests {
		var v HexOrDecimal64
		err := json.Unmarshal([]byte(test.input.(string)), &v)
		if test.wantErr {
			if err == nil {
				t.Errorf("%s: UnMarshalJSON did not error on invalid encoding: got %q, want <nil>", test.input, err)
			}
			continue
		}

		if uint64(v) != test.want.(uint64) {
			t.Errorf("input %s: value mismatch: got %d, want %d", test.input, v, test.want)
			continue
		}
	}
}

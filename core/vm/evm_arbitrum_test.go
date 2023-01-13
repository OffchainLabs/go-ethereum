package vm

import (
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

func TestIsStylusProgram(t *testing.T) {
	type test struct {
		input string
		want  bool
	}

	tests := []test{
		{input: "0x00", want: false},
		{input: "0xEFEF", want: false},
		{input: "0xEF00", want: false},
		{input: "0xEF0001", want: false},
		{input: "0xEF0000", want: true},
		{input: "0xEF000054", want: true},
		{input: "0xEF00a054", want: false},
		{input: "0xEFa00054", want: false},
	}

	for _, tc := range tests {
		b := hexutil.MustDecode(tc.input)
		got := IsStylusProgram(b)
		if got != tc.want {
			t.Fatalf("expected: %v, got: %v", tc.want, got)
		}
	}
}

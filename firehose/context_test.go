package firehose

import (
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessList_marshal(t *testing.T) {
	tests := []struct {
		name    string
		l       AccessList
		wantOut string
	}{
		{"empty", nil, "00"},

		{
			"one address no keys",
			AccessList{
				types.AccessTuple{
					Address: address(t, "0x1234567890123456789012345678901234567890"),
				},
			},
			"01123456789012345678901234567890123456789000",
		},

		{
			"one address one key",
			AccessList{
				types.AccessTuple{
					Address:     address(t, "0x1234567890123456789012345678901234567890"),
					StorageKeys: []common.Hash{hash(t, "AB")},
				},
			},
			"0112345678901234567890123456789012345678900100000000000000000000000000000000000000000000000000000000000000ab",
		},

		{
			"one address multi keys",
			AccessList{
				types.AccessTuple{
					Address:     address(t, "0x1234567890123456789012345678901234567890"),
					StorageKeys: []common.Hash{hash(t, "AB"), hash(t, "EF")},
				},
			},
			`
				01
				1234567890123456789012345678901234567890
				  02
				  00000000000000000000000000000000000000000000000000000000000000ab
				  00000000000000000000000000000000000000000000000000000000000000ef
		   `,
		},

		{
			"multi address multi keys",
			AccessList{
				types.AccessTuple{
					Address:     address(t, "0x1234567890123456789012345678901234567890"),
					StorageKeys: []common.Hash{hash(t, "AB"), hash(t, "EF")},
				},
				types.AccessTuple{
					Address: address(t, "0xabcdefabcdefabcdefabcdefabcdefabcdef0910"),
				},
				types.AccessTuple{
					Address:     address(t, "0x1234567890123456789012345678901234567890"),
					StorageKeys: []common.Hash{hash(t, "12")},
				},
			},
			`
				03
				1234567890123456789012345678901234567890
					02
					00000000000000000000000000000000000000000000000000000000000000ab
					00000000000000000000000000000000000000000000000000000000000000ef
				abcdefabcdefabcdefabcdefabcdefabcdef0910
					00
				1234567890123456789012345678901234567890
					01
					0000000000000000000000000000000000000000000000000000000000000012
		   `,
		},
	}

	spaceRegex := regexp.MustCompile(`[\s\n\t]+`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, spaceRegex.ReplaceAllString(tt.wantOut, ""), hex.EncodeToString(tt.l.marshal()))
		})
	}
}

func address(t *testing.T, in string) common.Address {
	t.Helper()

	addr, err := common.NewMixedcaseAddressFromString(in)
	require.NoError(t, err)

	return addr.Address()
}

func hash(t *testing.T, in string) common.Hash {
	t.Helper()

	return common.HexToHash(in)
}

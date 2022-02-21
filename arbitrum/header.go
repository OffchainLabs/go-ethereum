package arbitrum

import (
	"encoding/binary"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type HeaderInfo struct {
	SendRoot      common.Hash
	SendCount     uint64
	L1BlockNumber uint64
}

func (info HeaderInfo) Extra() []byte {
	return info.SendRoot[:]
}

func (info HeaderInfo) MixDigest() [32]byte {
	mixDigest := common.Hash{}
	binary.BigEndian.PutUint64(mixDigest[:8], info.SendCount)
	binary.BigEndian.PutUint64(mixDigest[8:16], info.L1BlockNumber)
	return mixDigest
}

func DeserializeHeaderExtraInformation(header *types.Header) (HeaderInfo, error) {
	if header.Number.Sign() == 0 || len(header.Extra) == 0 {
		// The genesis block doesn't have an ArbOS encoded extra field
		return HeaderInfo{}, nil
	}
	if len(header.Extra) != 32 {
		return HeaderInfo{}, fmt.Errorf("unexpected header extra field length %v", len(header.Extra))
	}
	extra := HeaderInfo{}
	copy(extra.SendRoot[:], header.Extra)
	extra.SendCount = binary.BigEndian.Uint64(header.MixDigest[:8])
	extra.L1BlockNumber = binary.BigEndian.Uint64(header.MixDigest[8:16])
	return extra, nil
}

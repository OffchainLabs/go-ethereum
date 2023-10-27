package arb

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
)

// Constants to match up protocol versions and messages
const (
	ARB1 = 1
)

// ProtocolName is the official short name of the `snap` protocol used during
// devp2p capability negotiation.
const ProtocolName = "arb"

// ProtocolVersions are the supported versions of the `snap` protocol (first
// is primary).
var ProtocolVersions = []uint{ARB1}

// protocolLengths are the number of implemented message corresponding to
// different protocol versions.
var protocolLengths = map[uint]uint64{ARB1: 8}

const (
	GetLastConfirmedMsg         = 0x00
	LastConfirmedMsg            = 0x01
	GetLastCheckpointMsg        = 0x02
	LastCheckpointMsg           = 0x03
	QueryCheckpointSupportedMsg = 0x04
	CheckpointSupportedMsg      = 0x05
	ProtocolLenArb1             = 6
)

type LastConfirmedMsgPacket struct {
	header types.Header
	node   uint64
}

// NodeInfo represents a short summary of the `arb` sub-protocol metadata
// known about the host peer.
type NodeInfo struct{}

// nodeInfo retrieves some `arb` protocol metadata about the running host node.
func nodeInfo() *NodeInfo {
	return &NodeInfo{}
}

type Backend interface {
	PeerInfo(id enode.ID) interface{}
	LastConfirmed(id enode.ID, confirmed *types.Header) error
}

func MakeProtocols(backend Backend, dnsdisc enode.Iterator) []p2p.Protocol {
	protocols := make([]p2p.Protocol, len(ProtocolVersions))
	for i, version := range ProtocolVersions {
		version := version // Closure

		protocols[i] = p2p.Protocol{
			Name:    ProtocolName,
			Version: version,
			Length:  protocolLengths[version],
			Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
				peer := NewPeer(p, rw)
				return peer.Run(backend)
			},
			NodeInfo: func() interface{} {
				return nodeInfo()
			},
			PeerInfo: func(id enode.ID) interface{} {
				return backend.PeerInfo(id)
			},
			Attributes:     []enr.Entry{&enrEntry{}},
			DialCandidates: dnsdisc,
		}
	}
	return protocols
}

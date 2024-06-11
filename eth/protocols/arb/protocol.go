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
var protocolLengths = map[uint]uint64{ARB1: ProtocolLenArb1}

const (
	GetLastConfirmedMsg  = 0x00
	LastConfirmedMsg     = 0x01
	GetLastCheckpointMsg = 0x02
	CheckpointQueryMsg   = 0x03
	CheckpointMsg        = 0x04
	ProtocolLenArb1      = 5
)

type LastConfirmedMsgPacket struct {
	Header        *types.Header
	L1BlockNumber uint64
	Node          uint64
}

type CheckpointMsgPacket struct {
	Header   *types.Header
	HasState bool
}

type CheckpointQueryPacket struct {
	Header *types.Header
}

// NodeInfo represents a short summary of the `arb` sub-protocol metadata
// known about the host peer.
type NodeInfo struct{}

// nodeInfo retrieves some `arb` protocol metadata about the running host node.
func nodeInfo() *NodeInfo {
	return &NodeInfo{}
}

type Handler func(peer *Peer) error

// Backend defines the data retrieval methods to serve remote requests and the
// callback methods to invoke on remote deliveries.
type Backend interface {
	PeerInfo(id enode.ID) interface{}
	HandleLastConfirmed(peer *Peer, confirmed *types.Header, l1BlockNumber uint64, node uint64)
	HandleCheckpoint(peer *Peer, header *types.Header, supported bool)
	LastConfirmed() (*types.Header, uint64, uint64, error)
	LastCheckpoint() (*types.Header, error)
	CheckpointSupported(*types.Header) (bool, error)
	// RunPeer is invoked when a peer joins on the `eth` protocol. The handler
	// should do any peer maintenance work, handshakes and validations. If all
	// is passed, control should be given back to the `handler` to process the
	// inbound messages going forward.
	RunPeer(peer *Peer, handler Handler) error
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
				return backend.RunPeer(peer, func(peer *Peer) error {
					return Handle(backend, peer)
				})
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

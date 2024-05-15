package arb

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

type Peer struct {
	p2pPeer *p2p.Peer
	rw      p2p.MsgReadWriter
}

func NewPeer(p2pPeer *p2p.Peer, rw p2p.MsgReadWriter) *Peer {
	return &Peer{
		p2pPeer: p2pPeer,
		rw:      rw,
	}
}

func (p *Peer) RequestCheckpoint(header *types.Header) error {
	if header == nil {
		return p2p.Send(p.rw, GetLastCheckpointMsg, struct{}{})
	}
	return p2p.Send(p.rw, CheckpointQueryMsg, &CheckpointQueryPacket{
		Header: header,
	})
}

func (p *Peer) RequestLastConfirmed() error {
	return p2p.Send(p.rw, GetLastConfirmedMsg, struct{}{})
}

func (p *Peer) ID() string {
	return p.p2pPeer.ID().String()
}

func (p *Peer) Node() *enode.Node {
	return p.p2pPeer.Node()
}

// Handle is the callback invoked to manage the life cycle of a `snap` peer.
// When this function terminates, the peer is disconnected.
func Handle(backend Backend, peer *Peer) error {
	for {
		if err := HandleMessage(backend, peer); err != nil {
			log.Debug("Message handling failed in `arb`", "err", err)
			return err
		}
	}
}

// HandleMessage is invoked whenever an inbound message is received from a
// remote peer on the `snap` protocol. The remote connection is torn down upon
// returning any error.
func HandleMessage(backend Backend, peer *Peer) error {
	// Read the next message from the remote peer, and ensure it's fully consumed
	msg, err := peer.rw.ReadMsg()
	if err != nil {
		return err
	}
	// if msg.Size > maxMessageSize {
	// 	return fmt.Errorf("%w: %v > %v", errMsgTooLarge, msg.Size, maxMessageSize)
	// }
	defer msg.Discard()
	start := time.Now()
	// Track the amount of time it takes to serve the request and run the handler
	if metrics.Enabled {
		h := fmt.Sprintf("%s/%s/%d/%#02x", p2p.HandleHistName, ProtocolName, ARB1, msg.Code)
		defer func(start time.Time) {
			sampler := func() metrics.Sample {
				return metrics.NewBoundedHistogramSample()
			}
			metrics.GetOrRegisterHistogramLazy(h, nil, sampler).Update(time.Since(start).Microseconds())
		}(start)
	}
	switch {
	case msg.Code == GetLastConfirmedMsg:
		confirmed, l1BlockNumber, node, err := backend.LastConfirmed()
		if err != nil || confirmed == nil {
			return err
		}
		response := LastConfirmedMsgPacket{
			Header:        confirmed,
			L1BlockNumber: l1BlockNumber,
			Node:          node,
		}
		return p2p.Send(peer.rw, LastConfirmedMsg, &response)
	case msg.Code == LastConfirmedMsg:
		var incoming LastConfirmedMsgPacket
		err := msg.Decode(&incoming)
		if err != nil {
			return err
		}
		if incoming.Header == nil {
			return nil
		}
		backend.HandleLastConfirmed(peer, incoming.Header, incoming.L1BlockNumber, incoming.Node)
		return nil
	case msg.Code == GetLastCheckpointMsg:
		checkpoint, err := backend.LastCheckpoint()
		if err != nil {
			return err
		}
		response := CheckpointMsgPacket{
			Header:   checkpoint,
			HasState: true,
		}
		return p2p.Send(peer.rw, CheckpointMsg, &response)
	case msg.Code == CheckpointQueryMsg:
		incoming := CheckpointQueryPacket{}
		err := msg.Decode(&incoming)
		if err != nil {
			return err
		}
		hasState, err := backend.CheckpointSupported(incoming.Header)
		if err != nil {
			return err
		}
		response := CheckpointMsgPacket{
			Header:   incoming.Header,
			HasState: hasState,
		}
		return p2p.Send(peer.rw, CheckpointMsg, &response)
	case msg.Code == CheckpointMsg:
		incoming := CheckpointMsgPacket{}
		err := msg.Decode(&incoming)
		if err != nil {
			return err
		}
		backend.HandleCheckpoint(peer, incoming.Header, incoming.HasState)
		return nil
	}
	return fmt.Errorf("Invalid message: %v", msg.Code)
}

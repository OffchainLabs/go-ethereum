package arb

import (
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/p2p"
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

func (p *Peer) Run(backend Backend) error {
	return Handle(backend, p)
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
	if msg.Code == GetLastConfirmedMsg {
		response := LastConfirmedMsgPacket{}
		return p2p.Send(peer.rw, LastConfirmedMsg, &response)
	}
	if msg.Code == LastConfirmedMsg {
		incoming := LastConfirmedMsgPacket{}
		msg.Decode(&incoming)
		return backend.LastConfirmed(peer.p2pPeer.ID(), &incoming.header)
	}
	return fmt.Errorf("Invalid message: %v", msg.Code)
}

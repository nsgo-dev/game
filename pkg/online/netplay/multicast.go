package netplay

import "errors"

// Multicaster is implemented by transports that can send one logical payload
// to a deterministic peer set. SendMany reports peers that could not receive.
type Multicaster interface {
	SendMany(peers []PeerID, ch Channel, payload []byte) (failed []PeerID, err error)
}

// SendMany sends to peers in slice order and returns every failed peer. It is
// the portable fallback for transports that do not implement Multicaster.
func SendMany(tx Transport, peers []PeerID, ch Channel, payload []byte) (failed []PeerID, err error) {
	if tx == nil {
		return append([]PeerID(nil), peers...), errors.New("net: nil transport")
	}
	if m, ok := tx.(Multicaster); ok {
		return m.SendMany(peers, ch, payload)
	}
	for _, peer := range peers {
		if e := tx.Send(peer, ch, payload); e != nil {
			failed = append(failed, peer)
			if err == nil {
				err = e
			}
		}
	}
	return failed, err
}

// PeerGroup is an ordered, deterministic set of peers suitable for rooms,
// teams, and spectators.
type PeerGroup struct{ peers []PeerID }

// Add inserts peer once and reports whether the group changed.
func (g *PeerGroup) Add(peer PeerID) bool {
	if g == nil || peer == 0 {
		return false
	}
	for _, p := range g.peers {
		if p == peer {
			return false
		}
	}
	g.peers = append(g.peers, peer)
	return true
}

// Remove deletes peer and reports whether it was present.
func (g *PeerGroup) Remove(peer PeerID) bool {
	if g == nil {
		return false
	}
	for i, p := range g.peers {
		if p == peer {
			copy(g.peers[i:], g.peers[i+1:])
			g.peers = g.peers[:len(g.peers)-1]
			return true
		}
	}
	return false
}

// Members returns a copy in join order.
func (g *PeerGroup) Members() []PeerID {
	if g == nil {
		return nil
	}
	return append([]PeerID(nil), g.peers...)
}

// Broadcast sends to the current members except the optional peer IDs.
func (g *PeerGroup) Broadcast(tx Transport, ch Channel, payload []byte, except ...PeerID) []PeerID {
	if g == nil {
		return nil
	}
	blocked := func(p PeerID) bool {
		for _, e := range except {
			if p == e {
				return true
			}
		}
		return false
	}
	peers := make([]PeerID, 0, len(g.peers))
	for _, p := range g.peers {
		if !blocked(p) {
			peers = append(peers, p)
		}
	}
	failed, _ := SendMany(tx, peers, ch, payload)
	return failed
}

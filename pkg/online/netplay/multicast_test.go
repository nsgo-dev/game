package netplay

import "testing"

func TestPeerGroupBroadcastOrderExceptAndFailed(t *testing.T) {
	g := PeerGroup{}
	for _, p := range []PeerID{3, 1, 2, 3} {
		g.Add(p)
	}
	if got := g.Members(); len(got) != 3 || got[0] != 3 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("members = %v", got)
	}
	tx := &recordingTransport{fail: 2}
	failed := g.Broadcast(tx, ChannelUnreliable, []byte("x"), 1)
	if len(failed) != 1 || failed[0] != 2 || len(tx.sent) != 1 || tx.sent[0] != 3 {
		t.Fatalf("failed=%v sent=%v", failed, tx.sent)
	}
}

type recordingTransport struct {
	sent []PeerID
	fail PeerID
}

func (t *recordingTransport) Send(peer PeerID, _ Channel, _ []byte) error {
	if peer == t.fail {
		return ErrUnknownPeer
	}
	t.sent = append(t.sent, peer)
	return nil
}
func (t *recordingTransport) Recv() (Packet, bool) { return Packet{}, false }
func (t *recordingTransport) LocalID() PeerID      { return 1 }
func (t *recordingTransport) Peers() []PeerID      { return append([]PeerID(nil), t.sent...) }
func (t *recordingTransport) Close() error         { return nil }

//go:build !js

package netplay

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
)

func FuzzWireUDPSealedDatagram(f *testing.F) {
	f.Add(uint8(0), uint32(2), uint32(1), uint8(ChannelUnreliable), []byte("udp-data"))
	f.Add(uint8(1), uint32(1), uint32(55), uint8(ChannelUnreliable), []byte(nil))
	f.Add(uint8(2), uint32(99), uint32(12345), uint8(ChannelUnreliable), []byte{0, 255})
	f.Add(uint8(5), uint32(7), uint32(42), uint8(ChannelUnreliable), []byte("peer-data"))

	types := []byte{udpData, udpServerData, udpPing, udpPong, udpBye, udpPeerData, udpKick}

	f.Fuzz(func(t *testing.T, typIdx uint8, peer, seq uint32, rawCh uint8, payload []byte) {
		typ := types[int(typIdx)%len(types)]
		ch := Channel(rawCh % 2)
		var key [hsKeySize]byte
		for i := range key {
			key[i] = byte(i + int(peer))
		}
		dir := udpDirClientToServer
		switch typ {
		case udpServerData, udpPong, udpKick:
			dir = udpDirServerToClient
		case udpPeerData:
			dir = peer + 1
		}
		c := newUDPCipher(key)
		want := c.seal(nil, dir, typ, PeerID(peer), seq, ch, payload)
		sealed := want[udpCleartextHeader:]

		got := wire.AppendUDPSealedDatagram(nil, &wire.UDPSealedDatagram{
			Type:    wire.UDPDatagramType(typ),
			Peer:    peer,
			Seq:     seq,
			Channel: wire.Channel(ch),
			Sealed:  sealed,
		})
		if !bytes.Equal(got, want) {
			t.Fatalf("UDPSealedDatagram encode mismatch: got %x, want %x", got, want)
		}

		sh, ok := parseSealed(want)
		var dec wire.UDPSealedDatagram
		n, err := wire.DecodeUDPSealedDatagram(want, &dec)
		if !ok || err != nil || n != len(want) {
			t.Fatalf("UDPSealedDatagram decode failed: ok=%v err=%v n=%d/%d", ok, err, n, len(want))
		}
		if byte(dec.Type) != sh.typ || PeerID(dec.Peer) != sh.peer || dec.Seq != sh.seq || Channel(dec.Channel) != sh.ch || !bytes.Equal(dec.Sealed, sealed) {
			t.Fatalf("UDPSealedDatagram decode mismatch")
		}
		pt, opened := c.open(nil, dir, got)
		if !opened || !bytes.Equal(pt, payload) {
			t.Fatalf("UDPSealedDatagram AEAD open mismatch: opened=%v got %x, want %x", opened, pt, payload)
		}

		// Also verify UDPPeerFrame against udp_native.go's 9-byte header layout (from(4) + wireFrame).
		wireFrame := encodeWireFrame(ch, seq, payload)
		wantPeer := make([]byte, 4+len(wireFrame))
		binary.BigEndian.PutUint32(wantPeer[0:4], peer)
		copy(wantPeer[4:], wireFrame)
		gotPeer := wire.AppendUDPPeerFrame(nil, &wire.UDPPeerFrame{
			From:    peer,
			Channel: wire.Channel(ch),
			Seq:     seq,
			Payload: payload,
		})
		if !bytes.Equal(gotPeer, wantPeer) {
			t.Fatalf("UDPPeerFrame encode mismatch")
		}
		var decPeer wire.UDPPeerFrame
		if n, err := wire.DecodeUDPPeerFrame(wantPeer, &decPeer); err != nil || n != len(wantPeer) || decPeer.From != peer || Channel(decPeer.Channel) != ch || decPeer.Seq != seq || !bytes.Equal(decPeer.Payload, payload) {
			t.Fatalf("UDPPeerFrame decode mismatch")
		}
	})
}

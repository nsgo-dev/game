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

	types := []byte{udpData, udpServerData, udpPing, udpPong, udpBye}

	f.Fuzz(func(t *testing.T, typIdx uint8, peer, seq uint32, rawCh uint8, payload []byte) {
		typ := types[int(typIdx)%len(types)]
		ch := Channel(rawCh % 2)
		var key [hsKeySize]byte
		for i := range key {
			key[i] = byte(i + int(peer))
		}
		want := sealUDP(&key, typ, PeerID(peer), seq, ch, payload)
		var tag [8]uint8
		copy(tag[:], want[10:18])

		got := wire.AppendUDPSealedDatagram(nil, &wire.UDPSealedDatagram{
			Type:    wire.UDPDatagramType(typ),
			Peer:    peer,
			Seq:     seq,
			Channel: wire.Channel(ch),
			Tag:     tag,
			Payload: payload,
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
		if byte(dec.Type) != sh.typ || PeerID(dec.Peer) != sh.peer || dec.Seq != sh.seq || Channel(dec.Channel) != sh.ch || dec.Tag != tag || !bytes.Equal(dec.Payload, payload) {
			t.Fatalf("UDPSealedDatagram decode mismatch")
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

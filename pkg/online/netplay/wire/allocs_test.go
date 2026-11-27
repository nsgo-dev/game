package wire_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
)

func TestGeneratedCodecsZeroAllocs(t *testing.T) {
	t.Run("TransportFrame", func(t *testing.T) {
		in := wire.TransportFrame{
			Channel: wire.ChannelUnreliable,
			Seq:     42,
			Payload: []byte("hello world"),
		}
		buf := make([]byte, 0, 128)
		var out wire.TransportFrame
		out.Payload = make([]byte, 0, 128)

		// Warm up.
		buf = wire.AppendTransportFrame(buf[:0], &in)
		if _, err := wire.DecodeTransportFrame(buf, &out); err != nil {
			t.Fatal(err)
		}

		gamedebug.AssertZeroAllocs(t, 100, func() {
			buf = wire.AppendTransportFrame(buf[:0], &in)
			if _, err := wire.DecodeTransportFrame(buf, &out); err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("UDPPeerFrame", func(t *testing.T) {
		in := wire.UDPPeerFrame{
			From:    7,
			Channel: wire.ChannelUnreliable,
			Seq:     99,
			Payload: []byte{1, 2, 3, 4},
		}
		buf := make([]byte, 0, 128)
		var out wire.UDPPeerFrame
		out.Payload = make([]byte, 0, 128)

		buf = wire.AppendUDPPeerFrame(buf[:0], &in)
		if _, err := wire.DecodeUDPPeerFrame(buf, &out); err != nil {
			t.Fatal(err)
		}
		gamedebug.AssertZeroAllocs(t, 100, func() {
			buf = wire.AppendUDPPeerFrame(buf[:0], &in)
			if _, err := wire.DecodeUDPPeerFrame(buf, &out); err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("UDPSealedDatagram", func(t *testing.T) {
		in := wire.UDPSealedDatagram{
			Type:    wire.UDPDatagramTypeData,
			Peer:    5,
			Seq:     1234,
			Channel: wire.ChannelUnreliable,
			Sealed:  []byte("sealed ciphertext + 16B poly tag"),
		}
		buf := make([]byte, 0, 128)
		var out wire.UDPSealedDatagram
		out.Sealed = make([]byte, 0, 128)

		buf = wire.AppendUDPSealedDatagram(buf[:0], &in)
		if _, err := wire.DecodeUDPSealedDatagram(buf, &out); err != nil {
			t.Fatal(err)
		}
		gamedebug.AssertZeroAllocs(t, 100, func() {
			buf = wire.AppendUDPSealedDatagram(buf[:0], &in)
			if _, err := wire.DecodeUDPSealedDatagram(buf, &out); err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("HandshakeMessage", func(t *testing.T) {
		hello := wire.HandshakeMessage{
			Tag: wire.HandshakeMessageTagHello,
			Hello: wire.HandshakeHello{
				Nonce:        [16]uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
				ResumePeer:   4,
				ResumeSecret: [16]uint8{16, 15, 14, 13},
				Token:        []byte("jwt-token-bytes"),
			},
		}
		welcome := wire.HandshakeMessage{
			Tag: wire.HandshakeMessageTagWelcome,
			Welcome: wire.HandshakeWelcome{
				Nonce:   hello.Hello.Nonce,
				Peer:    2,
				Server:  1,
				Resumed: true,
				Secret:  [16]uint8{9, 8, 7},
				Key:     [32]uint8{1, 2, 3, 4},
			},
		}
		reject := wire.HandshakeMessage{
			Tag: wire.HandshakeMessageTagReject,
			Reject: wire.HandshakeReject{
				Nonce:  hello.Hello.Nonce,
				Reason: "server full",
			},
		}

		buf := make([]byte, 0, 256)
		var out wire.HandshakeMessage
		out.Hello.Token = make([]byte, 0, 64)
		// Warm up all three variants so caller-owned slices and repeated reason string are populated.
		for _, msg := range []*wire.HandshakeMessage{&hello, &welcome, &reject} {
			buf = wire.AppendHandshakeMessage(buf[:0], msg)
			if _, err := wire.DecodeHandshakeMessage(buf, &out); err != nil {
				t.Fatal(err)
			}
		}

		gamedebug.AssertZeroAllocs(t, 100, func() {
			buf = wire.AppendHandshakeMessage(buf[:0], &hello)
			if _, err := wire.DecodeHandshakeMessage(buf, &out); err != nil {
				t.Fatal(err)
			}
			buf = wire.AppendHandshakeMessage(buf[:0], &welcome)
			if _, err := wire.DecodeHandshakeMessage(buf, &out); err != nil {
				t.Fatal(err)
			}
			buf = wire.AppendHandshakeMessage(buf[:0], &reject)
			if _, err := wire.DecodeHandshakeMessage(buf, &out); err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("Relay", func(t *testing.T) {
		frame := wire.RelayFrame{Peer: 9, Body: []byte{1, 2, 3}}
		members := wire.RelayMembers{Epoch: 3, Peers: []uint32{1, 2, 5, 9}}
		rel := wire.RelayBody{
			Tag:      wire.RelayBodyTagReliable,
			Reliable: wire.RelayReliable{Seq: 77, Payload: []byte("reliable")},
		}
		ack := wire.RelayBody{
			Tag: wire.RelayBodyTagAck,
			Ack: wire.RelayAck{Next: 78, Mask: 0x55aa},
		}

		buf := make([]byte, 0, 256)
		var outFrame wire.RelayFrame
		var outMembers wire.RelayMembers
		var outBody wire.RelayBody

		// Warm up.
		buf = wire.AppendRelayFrame(buf[:0], &frame)
		if _, err := wire.DecodeRelayFrame(buf, &outFrame); err != nil {
			t.Fatal(err)
		}
		buf = wire.AppendRelayMembers(buf[:0], &members)
		if _, err := wire.DecodeRelayMembers(buf, &outMembers); err != nil {
			t.Fatal(err)
		}
		buf = wire.AppendRelayBody(buf[:0], &rel)
		if _, err := wire.DecodeRelayBody(buf, &outBody); err != nil {
			t.Fatal(err)
		}
		buf = wire.AppendRelayBody(buf[:0], &ack)
		if _, err := wire.DecodeRelayBody(buf, &outBody); err != nil {
			t.Fatal(err)
		}

		gamedebug.AssertZeroAllocs(t, 100, func() {
			buf = wire.AppendRelayFrame(buf[:0], &frame)
			if _, err := wire.DecodeRelayFrame(buf, &outFrame); err != nil {
				t.Fatal(err)
			}
			buf = wire.AppendRelayMembers(buf[:0], &members)
			if _, err := wire.DecodeRelayMembers(buf, &outMembers); err != nil {
				t.Fatal(err)
			}
			buf = wire.AppendRelayBody(buf[:0], &rel)
			if _, err := wire.DecodeRelayBody(buf, &outBody); err != nil {
				t.Fatal(err)
			}
			buf = wire.AppendRelayBody(buf[:0], &ack)
			if _, err := wire.DecodeRelayBody(buf, &outBody); err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("ReplicationMessage_And_Delta", func(t *testing.T) {
		base := wire.EntityRecord{
			ID:         1,
			FieldsMask: wire.EntityRecordMaskValid,
			Kind:       3,
			X:          10.5,
			Y:          -4.25,
			VX:         1.0,
			VY:         0.0,
			Rotation:   0.5,
			Flags:      7,
			Props:      []byte{10, 20, 30},
		}
		cur := base
		cur.X = 12.75
		cur.Props = []byte{10, 20, 31}

		full := wire.ReplicationMessage{
			Tag:     wire.ReplicationMessageTagFull,
			Version: wire.ReplicationMessageVersion,
			Full: wire.SnapshotFull{
				Epoch: 0x12345678,
				Tick:  500,
				Entities: []wire.EntityRecord{
					base,
					{
						ID:         2,
						FieldsMask: wire.EntityRecordFieldKind | wire.EntityRecordFieldX | wire.EntityRecordFieldProps,
						Kind:       4,
						X:          99,
						Props:      []byte{1, 2},
					},
				},
			},
		}
		delta := wire.ReplicationMessage{
			Tag:     wire.ReplicationMessageTagDelta,
			Version: wire.ReplicationMessageVersion,
			Delta: wire.SnapshotDelta{
				Epoch:        0x12345678,
				Tick:         501,
				BaselineTick: 500,
				Despawns:     []uint32{2},
				Updates:      []wire.EntityRecord{cur},
			},
		}
		delta.Delta.Updates[0].FieldsMask = wire.DiffEntityRecord(&base, &cur)

		buf := make([]byte, 0, 512)
		var out wire.ReplicationMessage
		applied := base
		applied.Props = append(make([]byte, 0, 16), base.Props...)

		// Warm up.
		buf = wire.AppendReplicationMessage(buf[:0], &full)
		if _, err := wire.DecodeReplicationMessage(buf, &out); err != nil {
			t.Fatal(err)
		}
		buf = wire.AppendReplicationMessage(buf[:0], &delta)
		if _, err := wire.DecodeReplicationMessage(buf, &out); err != nil {
			t.Fatal(err)
		}
		wire.ApplyEntityRecord(&applied, &out.Delta.Updates[0])

		gamedebug.AssertZeroAllocs(t, 100, func() {
			buf = wire.AppendReplicationMessage(buf[:0], &full)
			if _, err := wire.DecodeReplicationMessage(buf, &out); err != nil {
				t.Fatal(err)
			}
			mask := wire.DiffEntityRecord(&base, &cur)
			delta.Delta.Updates[0].FieldsMask = mask
			buf = wire.AppendReplicationMessage(buf[:0], &delta)
			if _, err := wire.DecodeReplicationMessage(buf, &out); err != nil {
				t.Fatal(err)
			}
			wire.ApplyEntityRecord(&applied, &out.Delta.Updates[0])
		})
	})

	t.Run("RollbackAndMembership", func(t *testing.T) {
		rb := wire.RollbackMessage{
			Tag: wire.RollbackMessageTagInputBundle,
			InputBundle: wire.InputBundle{
				Inputs: []wire.InputFrame{
					{Tick: 10, Peer: 1, Bits: 0x123456789abcdef0, Axes: [2]int16{-300, 500}},
					{Tick: 11, Peer: 1, Bits: 0x0fedcba987654321, Axes: [2]int16{100, -200}},
				},
			},
		}
		mem := wire.MembershipMessage{
			Tag: wire.MembershipMessageTagChange,
			Change: wire.MemberChange{
				Kind:   wire.MemberChangeKindLeave,
				Peer:   2,
				Tick:   100,
				Inputs: rb.InputBundle.Inputs,
			},
		}
		js := wire.JoinState{
			Tick: 100,
			Members: []wire.MatchMember{
				{ID: 1, From: 0, HasLeave: false, Until: 0},
				{ID: 2, From: 10, HasLeave: true, Until: 105},
			},
			Inputs: rb.InputBundle.Inputs,
			State:  []byte("deterministic-world-snapshot"),
		}

		buf := make([]byte, 0, 512)
		var outRB wire.RollbackMessage
		var outMem wire.MembershipMessage
		var outJS wire.JoinState

		// Warm up.
		buf = wire.AppendRollbackMessage(buf[:0], &rb)
		if _, err := wire.DecodeRollbackMessage(buf, &outRB); err != nil {
			t.Fatal(err)
		}
		buf = wire.AppendMembershipMessage(buf[:0], &mem)
		if _, err := wire.DecodeMembershipMessage(buf, &outMem); err != nil {
			t.Fatal(err)
		}
		buf = wire.AppendJoinState(buf[:0], &js)
		if _, err := wire.DecodeJoinState(buf, &outJS); err != nil {
			t.Fatal(err)
		}

		gamedebug.AssertZeroAllocs(t, 100, func() {
			buf = wire.AppendRollbackMessage(buf[:0], &rb)
			if _, err := wire.DecodeRollbackMessage(buf, &outRB); err != nil {
				t.Fatal(err)
			}
			buf = wire.AppendMembershipMessage(buf[:0], &mem)
			if _, err := wire.DecodeMembershipMessage(buf, &outMem); err != nil {
				t.Fatal(err)
			}
			buf = wire.AppendJoinState(buf[:0], &js)
			if _, err := wire.DecodeJoinState(buf, &outJS); err != nil {
				t.Fatal(err)
			}
		})
	})
}

package netplay

import (
	"bytes"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
)

func FuzzWireTransportFrame(f *testing.F) {
	f.Add(uint8(ChannelReliableOrdered), uint32(0), []byte(nil))
	f.Add(uint8(ChannelUnreliable), uint32(42), []byte("hello"))
	f.Add(uint8(ChannelReliableOrdered), uint32(math.MaxUint32), []byte{0, 1, 2, 255})
	f.Add(uint8(9), uint32(1), []byte("bad-ch"))

	f.Fuzz(func(t *testing.T, rawCh uint8, seq uint32, payload []byte) {
		ch := Channel(rawCh % 2)
		want := encodeWireFrame(ch, seq, payload)
		msg := wire.TransportFrame{
			Channel: wire.Channel(ch),
			Seq:     seq,
			Payload: payload,
		}
		got := wire.AppendTransportFrame(nil, &msg)
		if !bytes.Equal(got, want) {
			t.Fatalf("TransportFrame encode mismatch: got %x, want %x", got, want)
		}

		// Differential decode on valid frame.
		wantCh, wantSeq, wantPayload, wantErr := decodeWireFrame(want)
		var dec wire.TransportFrame
		n, gotErr := wire.DecodeTransportFrame(want, &dec)
		if wantErr != nil || gotErr != nil || n != len(want) {
			t.Fatalf("decode valid frame: wantErr=%v gotErr=%v n=%d/%d", wantErr, gotErr, n, len(want))
		}
		if Channel(dec.Channel) != wantCh || dec.Seq != wantSeq || !bytes.Equal(dec.Payload, wantPayload) {
			t.Fatalf("decode mismatch: got %+v, want ch=%d seq=%d payload=%x", dec, wantCh, wantSeq, wantPayload)
		}

		// Differential decode on arbitrary bytes (rawCh + seq + payload).
		raw := encodeWireFrame(Channel(rawCh), seq, payload)
		_, _, _, handErr := decodeWireFrame(raw)
		_, genErr := wire.DecodeTransportFrame(raw, &dec)
		if (handErr == nil) != (genErr == nil) {
			t.Fatalf("invalid frame error agreement failed for rawCh=%d: handErr=%v genErr=%v", rawCh, handErr, genErr)
		}
	})
}

func FuzzWireHandshake(f *testing.F) {
	f.Add(uint32(0), uint32(1), uint32(2), true, []byte("tok"), "server full", uint16(128))
	f.Add(uint32(7), uint32(10), uint32(1), false, []byte(nil), "", uint16(0))
	f.Add(uint32(math.MaxUint32), uint32(5), uint32(1), true, []byte{1, 2, 3, 4}, "invalid token", uint16(200))

	f.Fuzz(func(t *testing.T, resumePeer, peer, server uint32, resumed bool, token []byte, reason string, minSize uint16) {
		if len(token) > MaxTokenSize {
			token = token[:MaxTokenSize]
		}
		if len(reason) > hsMaxReason {
			reason = reason[:hsMaxReason]
		}
		var nonce [hsNonceSize]byte
		var secret [hsSecretSize]byte
		var key [hsKeySize]byte
		for i := range nonce {
			nonce[i] = byte(i + int(peer))
		}
		for i := range secret {
			secret[i] = byte(i + int(resumePeer))
		}
		for i := range key {
			key[i] = byte(i + int(server))
		}

		// Hello (unpadded exact bytes match; padded hello decodes identically).
		h := hello{nonce: nonce, resumePeer: PeerID(resumePeer), resumeSecret: secret, token: token}
		wantHello := encodeHello(h, 0)
		genHelloMsg := wire.HandshakeMessage{
			Tag: wire.HandshakeMessageTagHello,
			Hello: wire.HandshakeHello{
				Nonce:        nonce,
				ResumePeer:   resumePeer,
				ResumeSecret: secret,
				Token:        token,
			},
		}
		gotHello := wire.AppendHandshakeMessage(nil, &genHelloMsg)
		if !bytes.Equal(gotHello, wantHello) {
			t.Fatalf("Hello encode mismatch: got %x, want %x", gotHello, wantHello)
		}

		padded := encodeHello(h, int(minSize%256))
		handH, handErr := decodeHello(padded)
		var decMsg wire.HandshakeMessage
		_, genErr := wire.DecodeHandshakeMessage(padded, &decMsg)
		if handErr != nil || genErr != nil {
			t.Fatalf("Hello decode error: handErr=%v genErr=%v", handErr, genErr)
		}
		if decMsg.Tag != wire.HandshakeMessageTagHello ||
			decMsg.Hello.Nonce != handH.nonce ||
			PeerID(decMsg.Hello.ResumePeer) != handH.resumePeer ||
			decMsg.Hello.ResumeSecret != handH.resumeSecret ||
			!bytes.Equal(decMsg.Hello.Token, handH.token) {
			t.Fatalf("Hello decode mismatch: got %+v, want %+v", decMsg.Hello, handH)
		}

		// Welcome.
		if peer == 0 {
			peer = 2
		}
		if server == 0 || server == peer {
			server = peer + 1
			if server == 0 {
				server = 1
			}
		}
		w := welcome{nonce: nonce, peer: PeerID(peer), server: PeerID(server), resumed: resumed, secret: secret, key: key}
		wantWelcome := encodeWelcome(w)
		genWelcomeMsg := wire.HandshakeMessage{
			Tag: wire.HandshakeMessageTagWelcome,
			Welcome: wire.HandshakeWelcome{
				Nonce:   nonce,
				Peer:    peer,
				Server:  server,
				Resumed: resumed,
				Secret:  secret,
				Key:     key,
			},
		}
		gotWelcome := wire.AppendHandshakeMessage(nil, &genWelcomeMsg)
		if !bytes.Equal(gotWelcome, wantWelcome) {
			t.Fatalf("Welcome encode mismatch: got %x, want %x", gotWelcome, wantWelcome)
		}
		handW, handWErr := decodeServerHandshake(wantWelcome)
		n, genWErr := wire.DecodeHandshakeMessage(wantWelcome, &decMsg)
		if handWErr != nil || genWErr != nil || n != len(wantWelcome) {
			t.Fatalf("Welcome decode error: hand=%v gen=%v n=%d/%d", handWErr, genWErr, n, len(wantWelcome))
		}
		if decMsg.Tag != wire.HandshakeMessageTagWelcome ||
			decMsg.Welcome.Nonce != handW.nonce ||
			PeerID(decMsg.Welcome.Peer) != handW.peer ||
			PeerID(decMsg.Welcome.Server) != handW.server ||
			decMsg.Welcome.Resumed != handW.resumed ||
			decMsg.Welcome.Secret != handW.secret ||
			decMsg.Welcome.Key != handW.key {
			t.Fatalf("Welcome decode mismatch")
		}

		// Reject.
		wantReject := encodeReject(nonce, reason, 0)
		genRejectMsg := wire.HandshakeMessage{
			Tag: wire.HandshakeMessageTagReject,
			Reject: wire.HandshakeReject{
				Nonce:  nonce,
				Reason: reason,
			},
		}
		gotReject := wire.AppendHandshakeMessage(nil, &genRejectMsg)
		if !bytes.Equal(gotReject, wantReject) {
			t.Fatalf("Reject encode mismatch: got %x, want %x", gotReject, wantReject)
		}
		_, handRErr := decodeServerHandshake(wantReject)
		n, genRErr := wire.DecodeHandshakeMessage(wantReject, &decMsg)
		if genRErr != nil || n != len(wantReject) {
			t.Fatalf("Reject decode error: %v n=%d/%d", genRErr, n, len(wantReject))
		}
		re, ok := handRErr.(*RejectError)
		if !ok || decMsg.Tag != wire.HandshakeMessageTagReject || decMsg.Reject.Reason != re.Reason || decMsg.Reject.Nonce != nonce {
			t.Fatalf("Reject decode mismatch: hand=%v gen=%+v", handRErr, decMsg.Reject)
		}
	})
}

func FuzzWireRelay(f *testing.F) {
	f.Add(uint32(1), uint32(10), uint32(99), uint64(0x123456789abcdef0), []byte("payload"), uint8(3))
	f.Add(uint32(0), uint32(0), uint32(0), uint64(0), []byte(nil), uint8(0))

	f.Fuzz(func(t *testing.T, peer, epoch, seq uint32, mask uint64, payload []byte, memberCount uint8) {
		// RelayFrame.
		wantFrame := encodeRelayFrame(PeerID(peer), payload)
		gotFrame := wire.AppendRelayFrame(nil, &wire.RelayFrame{Peer: peer, Body: payload})
		if !bytes.Equal(gotFrame, wantFrame) {
			t.Fatalf("RelayFrame encode mismatch: got %x, want %x", gotFrame, wantFrame)
		}
		handPeer, handBody, handOK := decodeRelayFrame(wantFrame)
		var decFrame wire.RelayFrame
		n, genErr := wire.DecodeRelayFrame(wantFrame, &decFrame)
		if !handOK || genErr != nil || n != len(wantFrame) || PeerID(decFrame.Peer) != handPeer || !bytes.Equal(decFrame.Body, handBody) {
			t.Fatalf("RelayFrame decode mismatch")
		}

		// RelayMembers (wrapped in RelayFrame(0, body)).
		count := int(memberCount % 16)
		peers := make([]PeerID, count)
		wirePeers := make([]uint32, count)
		for i := range count {
			id := uint32(i+1) * 3
			peers[i] = PeerID(id)
			wirePeers[i] = id
		}
		wantMembersFrame := encodeRelayMembers(epoch, peers)
		gotMembersBody := wire.AppendRelayMembers(nil, &wire.RelayMembers{Epoch: epoch, Peers: wirePeers})
		gotMembersFrame := wire.AppendRelayFrame(nil, &wire.RelayFrame{Peer: 0, Body: gotMembersBody})
		if !bytes.Equal(gotMembersFrame, wantMembersFrame) {
			t.Fatalf("RelayMembers encode mismatch: got %x, want %x", gotMembersFrame, wantMembersFrame)
		}
		handEpoch, handMembers, ok := decodeRelayMembers(wantMembersFrame[relayFrameHeader:])
		var decMembers wire.RelayMembers
		n, genErr = wire.DecodeRelayMembers(wantMembersFrame[relayFrameHeader:], &decMembers)
		if !ok || genErr != nil || n != len(wantMembersFrame)-relayFrameHeader || decMembers.Epoch != handEpoch || len(decMembers.Peers) != len(handMembers) {
			t.Fatalf("RelayMembers decode mismatch: ok=%v err=%v", ok, genErr)
		}
		for i := range handMembers {
			if PeerID(decMembers.Peers[i]) != handMembers[i] {
				t.Fatalf("RelayMembers[%d] = %d, want %d", i, decMembers.Peers[i], handMembers[i])
			}
		}

		// RelayBody: Unreliable, Reliable, Ack.
		for _, kind := range []byte{relayBodyUnreliable, relayBodyReliable} {
			wantData := encodeRelayData(kind, seq, payload)
			var body wire.RelayBody
			if kind == relayBodyUnreliable {
				body.Tag = wire.RelayBodyTagUnreliable
				body.Unreliable = wire.RelayUnreliable{Seq: seq, Payload: payload}
			} else {
				body.Tag = wire.RelayBodyTagReliable
				body.Reliable = wire.RelayReliable{Seq: seq, Payload: payload}
			}
			gotData := wire.AppendRelayBody(nil, &body)
			if !bytes.Equal(gotData, wantData) {
				t.Fatalf("RelayData(kind=%d) encode mismatch", kind)
			}
			var decBody wire.RelayBody
			n, err := wire.DecodeRelayBody(wantData, &decBody)
			if err != nil || n != len(wantData) || decBody.Tag != kind {
				t.Fatalf("RelayData decode failed: err=%v n=%d/%d tag=%d", err, n, len(wantData), decBody.Tag)
			}
		}

		wantAck := encodeRelayAck(seq, mask)
		gotAck := wire.AppendRelayBody(nil, &wire.RelayBody{
			Tag: wire.RelayBodyTagAck,
			Ack: wire.RelayAck{Next: seq, Mask: mask},
		})
		if !bytes.Equal(gotAck, wantAck) {
			t.Fatalf("RelayAck encode mismatch: got %x, want %x", gotAck, wantAck)
		}
		var decAck wire.RelayBody
		if n, err := wire.DecodeRelayBody(wantAck, &decAck); err != nil || n != len(wantAck) || decAck.Ack.Next != seq || decAck.Ack.Mask != mask {
			t.Fatalf("RelayAck decode mismatch: err=%v n=%d dec=%+v", err, n, decAck)
		}
	})
}

func FuzzWireReplication(f *testing.F) {
	f.Add(uint32(0xdeadbeef), uint32(10), uint32(9), uint32(1), uint16(2), uint32(0x3f800000), uint32(0xbf800000), uint32(0), uint32(0x80000000), uint32(0x40490fdb), uint32(7), []byte{1, 2, 3})
	f.Add(uint32(0), uint32(0), uint32(0), uint32(1), uint16(0), uint32(0), uint32(0), uint32(0), uint32(0), uint32(0), uint32(0), []byte(nil))
	f.Add(uint32(1), uint32(math.MaxUint32), uint32(math.MaxUint32-1), uint32(42), uint16(math.MaxUint16), uint32(0x7fc00001), uint32(0x80000000), uint32(0x3f000000), uint32(0xbf000000), uint32(0x42c80000), uint32(math.MaxUint32), []byte("props"))

	f.Fuzz(func(t *testing.T, epoch, tick, baseTick, id uint32, kind uint16, xBits, yBits, vxBits, vyBits, rotBits, flags uint32, props []byte) {
		if id == 0 {
			id = 1
		}
		comp := ComponentState{
			Kind:     kind,
			X:        math.Float32frombits(xBits),
			Y:        math.Float32frombits(yBits),
			VX:       math.Float32frombits(vxBits),
			VY:       math.Float32frombits(vyBits),
			Rotation: math.Float32frombits(rotBits),
			Flags:    flags,
			Props:    props,
		}
		var zeroComp ComponentState
		handMask := diffComponentMask(zeroComp, comp)

		var zeroRec wire.EntityRecord
		rec := wire.EntityRecord{
			ID:       id,
			Kind:     kind,
			X:        comp.X,
			Y:        comp.Y,
			VX:       comp.VX,
			VY:       comp.VY,
			Rotation: comp.Rotation,
			Flags:    flags,
			Props:    props,
		}
		genMask := wire.DiffEntityRecord(&zeroRec, &rec)
		if genMask != handMask {
			t.Fatalf("DiffEntityRecord mismatch: got 0x%02x, want 0x%02x", genMask, handMask)
		}
		rec.FieldsMask = genMask

		// Full snapshot.
		ents := map[EntityID]EntityState{EntityID(id): {ID: EntityID(id), ComponentState: comp}}
		wantFull := encodeFullSnapshot(epoch, tick, ents)
		gotFull := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
			Tag:     wire.ReplicationMessageTagFull,
			Version: ReplicationVersion,
			Full: wire.SnapshotFull{
				Epoch:    epoch,
				Tick:     tick,
				Entities: []wire.EntityRecord{rec},
			},
		})
		if !bytes.Equal(gotFull, wantFull) {
			t.Fatalf("SnapshotFull encode mismatch:\ngot:  %x\nwant: %x", gotFull, wantFull)
		}

		var decMsg wire.ReplicationMessage
		n, err := wire.DecodeReplicationMessage(wantFull, &decMsg)
		if err != nil || n != len(wantFull) || decMsg.Tag != wire.ReplicationMessageTagFull || decMsg.Version != ReplicationVersion {
			t.Fatalf("SnapshotFull decode failed: err=%v n=%d/%d", err, n, len(wantFull))
		}
		if decMsg.Full.Epoch != epoch || decMsg.Full.Tick != tick || len(decMsg.Full.Entities) != 1 {
			t.Fatalf("SnapshotFull header mismatch")
		}
		// Compare against readEntityFields.
		_, _, _, body, status := readReplHeader(wantFull)
		if status != replCurrent {
			t.Fatalf("readReplHeader status = %d", status)
		}
		// Skip tick and count varints from body to reach entity bytes.
		_, tn := decodeUvarintForTest(body)
		_, cn := decodeUvarintForTest(body[tn:])
		handID, handComp, rest, ok := readEntityFields(body[tn+cn:], zeroComp)
		if !ok || len(rest) != 0 {
			t.Fatalf("readEntityFields failed")
		}
		de := decMsg.Full.Entities[0]
		if EntityID(de.ID) != handID ||
			de.Kind != handComp.Kind ||
			math.Float32bits(de.X) != math.Float32bits(handComp.X) ||
			math.Float32bits(de.Y) != math.Float32bits(handComp.Y) ||
			math.Float32bits(de.VX) != math.Float32bits(handComp.VX) ||
			math.Float32bits(de.VY) != math.Float32bits(handComp.VY) ||
			math.Float32bits(de.Rotation) != math.Float32bits(handComp.Rotation) ||
			de.Flags != handComp.Flags ||
			!bytes.Equal(de.Props, handComp.Props) {
			t.Fatalf("EntityRecord decode mismatch")
		}

		// Delta snapshot with one despawn and one updated entity.
		despawnID := id + 1
		if despawnID == 0 {
			despawnID = 2
		}
		baseEnts := map[EntityID]EntityState{
			EntityID(despawnID): {ID: EntityID(despawnID)},
			EntityID(id):        {ID: EntityID(id), ComponentState: ComponentState{Kind: kind ^ 1, X: comp.X}},
		}
		wantDelta := encodeDeltaSnapshot(epoch, tick, baseTick, baseEnts, ents)
		baseRec := wire.EntityRecord{ID: id, Kind: kind ^ 1, X: comp.X}
		deltaRec := rec
		deltaRec.FieldsMask = wire.DiffEntityRecord(&baseRec, &rec)
		var updates []wire.EntityRecord
		if deltaRec.FieldsMask != 0 {
			updates = []wire.EntityRecord{deltaRec}
		}
		gotDelta := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
			Tag:     wire.ReplicationMessageTagDelta,
			Version: ReplicationVersion,
			Delta: wire.SnapshotDelta{
				Epoch:        epoch,
				Tick:         tick,
				BaselineTick: baseTick,
				Despawns:     []uint32{despawnID},
				Updates:      updates,
			},
		})
		if !bytes.Equal(gotDelta, wantDelta) {
			t.Fatalf("SnapshotDelta encode mismatch:\ngot:  %x\nwant: %x", gotDelta, wantDelta)
		}
		if n, err := wire.DecodeReplicationMessage(wantDelta, &decMsg); err != nil || n != len(wantDelta) || decMsg.Tag != wire.ReplicationMessageTagDelta {
			t.Fatalf("SnapshotDelta decode failed: err=%v n=%d/%d", err, n, len(wantDelta))
		}
		if len(updates) == 1 {
			applied := baseRec
			wire.ApplyEntityRecord(&applied, &decMsg.Delta.Updates[0])
			if applied.Kind != comp.Kind ||
				math.Float32bits(applied.X) != math.Float32bits(comp.X) ||
				math.Float32bits(applied.Y) != math.Float32bits(comp.Y) ||
				math.Float32bits(applied.VX) != math.Float32bits(comp.VX) ||
				math.Float32bits(applied.VY) != math.Float32bits(comp.VY) ||
				math.Float32bits(applied.Rotation) != math.Float32bits(comp.Rotation) ||
				applied.Flags != comp.Flags ||
				!bytes.Equal(applied.Props, comp.Props) {
				t.Fatalf("ApplyEntityRecord did not reproduce target ComponentState")
			}
		}

		// SnapshotAck.
		wantAck := encodeSnapshotAck(epoch, tick)
		gotAck := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
			Tag:     wire.ReplicationMessageTagAck,
			Version: ReplicationVersion,
			Ack:     wire.SnapshotAck{Epoch: epoch, Tick: tick},
		})
		if !bytes.Equal(gotAck, wantAck) {
			t.Fatalf("SnapshotAck encode mismatch")
		}
		if n, err := wire.DecodeReplicationMessage(wantAck, &decMsg); err != nil || n != len(wantAck) || decMsg.Ack.Epoch != epoch || decMsg.Ack.Tick != tick {
			t.Fatalf("SnapshotAck decode mismatch")
		}

		// ResyncRequest.
		wantResync := appendReplHeader(nil, replMsgResync, epoch)
		gotResync := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
			Tag:     wire.ReplicationMessageTagResync,
			Version: ReplicationVersion,
			Resync:  wire.ResyncRequest{Epoch: epoch},
		})
		if !bytes.Equal(gotResync, wantResync) {
			t.Fatalf("ResyncRequest encode mismatch")
		}
		if n, err := wire.DecodeReplicationMessage(wantResync, &decMsg); err != nil || n != len(wantResync) || decMsg.Resync.Epoch != epoch {
			t.Fatalf("ResyncRequest decode mismatch")
		}

		// SnapshotFullPart.
		parts, err := encodeFullSnapshotParts(epoch, tick, ents, DefaultMaxSnapshotSize)
		if err == nil && len(parts) == 1 {
			gotPart := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
				Tag:     wire.ReplicationMessageTagFullPart,
				Version: ReplicationVersion,
				FullPart: wire.SnapshotFullPart{
					Epoch:    epoch,
					Tick:     tick,
					Index:    0,
					Total:    1,
					Entities: []wire.EntityRecord{rec},
				},
			})
			if !bytes.Equal(gotPart, parts[0]) {
				t.Fatalf("SnapshotFullPart encode mismatch")
			}
			if n, err := wire.DecodeReplicationMessage(parts[0], &decMsg); err != nil || n != len(parts[0]) || decMsg.FullPart.Total != 1 {
				t.Fatalf("SnapshotFullPart decode mismatch")
			}
		}
	})
}

func decodeUvarintForTest(b []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, c := range b {
		if c < 0x80 {
			return x | uint64(c)<<s, i + 1
		}
		x |= uint64(c&0x7f) << s
		s += 7
	}
	return 0, 0
}

func FuzzWireRollback(f *testing.F) {
	f.Add(uint32(100), uint32(2), uint64(0xdeadbeefcafebabe), int16(-32768), int16(32767), uint64(0x1122334455667788))
	f.Add(uint32(0), uint32(1), uint64(0), int16(0), int16(0), uint64(0))

	f.Fuzz(func(t *testing.T, tick, peer uint32, bits uint64, ax0, ax1 int16, checksum uint64) {
		in := InputFrame{Tick: tick, Peer: PeerID(peer), Bits: bits, Axes: [2]int16{ax0, ax1}}
		wIn := wire.InputFrame{Tick: tick, Peer: peer, Bits: bits, Axes: [2]int16{ax0, ax1}}

		// SingleInput.
		wantSingle := encodeSingleInput(in)
		gotSingle := wire.AppendRollbackMessage(nil, &wire.RollbackMessage{
			Tag:         wire.RollbackMessageTagSingleInput,
			SingleInput: wire.SingleInput{Input: wIn},
		})
		if !bytes.Equal(gotSingle, wantSingle) {
			t.Fatalf("SingleInput encode mismatch")
		}
		handIn, ok := decodeInputFrame(wantSingle[1:])
		var dec wire.RollbackMessage
		n, err := wire.DecodeRollbackMessage(wantSingle, &dec)
		if !ok || err != nil || n != len(wantSingle) || dec.Tag != wire.RollbackMessageTagSingleInput ||
			dec.SingleInput.Input.Tick != handIn.Tick ||
			PeerID(dec.SingleInput.Input.Peer) != handIn.Peer ||
			dec.SingleInput.Input.Bits != handIn.Bits ||
			dec.SingleInput.Input.Axes != handIn.Axes {
			t.Fatalf("SingleInput decode mismatch")
		}

		// InputBundle.
		in2 := InputFrame{Tick: tick + 1, Peer: PeerID(peer), Bits: ^bits, Axes: [2]int16{ax1, ax0}}
		wIn2 := wire.InputFrame{Tick: tick + 1, Peer: peer, Bits: ^bits, Axes: [2]int16{ax1, ax0}}
		wantBundle := encodeInputBundle([]InputFrame{in, in2})
		gotBundle := wire.AppendRollbackMessage(nil, &wire.RollbackMessage{
			Tag:         wire.RollbackMessageTagInputBundle,
			InputBundle: wire.InputBundle{Inputs: []wire.InputFrame{wIn, wIn2}},
		})
		if !bytes.Equal(gotBundle, wantBundle) {
			t.Fatalf("InputBundle encode mismatch")
		}
		if n, err := wire.DecodeRollbackMessage(wantBundle, &dec); err != nil || n != len(wantBundle) || len(dec.InputBundle.Inputs) != 2 || dec.InputBundle.Inputs[1] != wIn2 {
			t.Fatalf("InputBundle decode mismatch")
		}

		// ChecksumMsg.
		wantSum := encodeChecksumMsg(tick, PeerID(peer), checksum)
		gotSum := wire.AppendRollbackMessage(nil, &wire.RollbackMessage{
			Tag:      wire.RollbackMessageTagChecksum,
			Checksum: wire.ChecksumMsg{Tick: tick, Peer: peer, Checksum: checksum},
		})
		if !bytes.Equal(gotSum, wantSum) {
			t.Fatalf("ChecksumMsg encode mismatch")
		}
		ht, hp, hs, ok := decodeChecksumMsg(wantSum[1:])
		if !ok {
			t.Fatal("decodeChecksumMsg failed")
		}
		if n, err := wire.DecodeRollbackMessage(wantSum, &dec); err != nil || n != len(wantSum) ||
			dec.Checksum.Tick != ht || PeerID(dec.Checksum.Peer) != hp || dec.Checksum.Checksum != hs {
			t.Fatalf("ChecksumMsg decode mismatch")
		}
	})
}

func FuzzWireMembership(f *testing.F) {
	f.Add(uint32(50), uint32(2), uint32(10), uint32(60), true, uint64(0xabcd), int16(-5), int16(7), []byte("sim-state"))
	f.Add(uint32(1), uint32(3), uint32(0), uint32(0), false, uint64(0), int16(0), int16(0), []byte(nil))

	f.Fuzz(func(t *testing.T, tick, peer, from, until uint32, hasLeave bool, bits uint64, ax0, ax1 int16, state []byte) {
		if peer == 0 {
			peer = 1
		}
		if !hasLeave {
			until = 0
		} else {
			// Ensure valid leave window: from < until and until > tick.
			from = tick
			until = tick + 5
		}

		// MemberChange (join).
		mcJoin := memberChange{kind: memberJoin, peer: PeerID(peer), tick: tick}
		wantJoin := encodeMemberChange(mcJoin)
		gotJoin := wire.AppendMembershipMessage(nil, &wire.MembershipMessage{
			Tag: wire.MembershipMessageTagChange,
			Change: wire.MemberChange{
				Kind: wire.MemberChangeKindJoin,
				Peer: peer,
				Tick: tick,
			},
		})
		if !bytes.Equal(gotJoin, wantJoin) {
			t.Fatalf("MemberChange(join) encode mismatch")
		}
		handMC, ok := decodeMemberChange(wantJoin)
		var decMem wire.MembershipMessage
		n, err := wire.DecodeMembershipMessage(wantJoin, &decMem)
		if !ok || err != nil || n != len(wantJoin) || byte(decMem.Change.Kind) != handMC.kind || PeerID(decMem.Change.Peer) != handMC.peer || decMem.Change.Tick != handMC.tick {
			t.Fatalf("MemberChange(join) decode mismatch")
		}

		// MemberChange (leave with one input for tick-1).
		leaveInput := InputFrame{Tick: tick - 1, Peer: PeerID(peer), Bits: bits, Axes: [2]int16{ax0, ax1}}
		mcLeave := memberChange{kind: memberLeave, peer: PeerID(peer), tick: tick, inputs: []InputFrame{leaveInput}}
		wantLeave := encodeMemberChange(mcLeave)
		gotLeave := wire.AppendMembershipMessage(nil, &wire.MembershipMessage{
			Tag: wire.MembershipMessageTagChange,
			Change: wire.MemberChange{
				Kind: wire.MemberChangeKindLeave,
				Peer: peer,
				Tick: tick,
				Inputs: []wire.InputFrame{
					{Tick: tick - 1, Peer: peer, Bits: bits, Axes: [2]int16{ax0, ax1}},
				},
			},
		})
		if !bytes.Equal(gotLeave, wantLeave) {
			t.Fatalf("MemberChange(leave) encode mismatch")
		}
		handMC, ok = decodeMemberChange(wantLeave)
		n, err = wire.DecodeMembershipMessage(wantLeave, &decMem)
		if !ok || err != nil || n != len(wantLeave) || len(decMem.Change.Inputs) != len(handMC.inputs) {
			t.Fatalf("MemberChange(leave) decode mismatch")
		}

		// JoinState + JoinStatePart.
		var jsInputs []InputFrame
		var wireJSInputs []wire.InputFrame
		if hasLeave {
			jsInputs = []InputFrame{{Tick: tick, Peer: PeerID(peer), Bits: bits, Axes: [2]int16{ax0, ax1}}}
			wireJSInputs = []wire.InputFrame{{Tick: tick, Peer: peer, Bits: bits, Axes: [2]int16{ax0, ax1}}}
		}
		js := joinState{
			tick:    tick,
			members: []matchMember{{id: PeerID(peer), from: from, hasLeave: hasLeave, until: until}},
			inputs:  jsInputs,
			state:   state,
		}
		wantJS := encodeJoinState(js)
		gotJS := wire.AppendJoinState(nil, &wire.JoinState{
			Tick:    tick,
			Members: []wire.MatchMember{{ID: peer, From: from, HasLeave: hasLeave, Until: until}},
			Inputs:  wireJSInputs,
			State:   state,
		})
		if !bytes.Equal(gotJS, wantJS) {
			t.Fatalf("JoinState encode mismatch:\ngot:  %x\nwant: %x", gotJS, wantJS)
		}
		handJS, ok := decodeJoinState(wantJS)
		var decJS wire.JoinState
		n, err = wire.DecodeJoinState(wantJS, &decJS)
		if !ok || err != nil || n != len(wantJS) || decJS.Tick != handJS.tick || len(decJS.Members) != 1 || !bytes.Equal(decJS.State, handJS.state) {
			t.Fatalf("JoinState decode mismatch: ok=%v err=%v", ok, err)
		}

		// JoinStatePart.
		parts := encodeJoinParts(wantJS)
		for i, p := range parts {
			hp, hparts, hdata, ok := decodeJoinPart(p)
			if !ok {
				t.Fatalf("decodeJoinPart(%d) failed", i)
			}
			gotPart := wire.AppendMembershipMessage(nil, &wire.MembershipMessage{
				Tag: wire.MembershipMessageTagJoinStatePart,
				JoinStatePart: wire.JoinStatePart{
					Part:  uint16(hp),
					Parts: uint16(hparts),
					Data:  hdata,
				},
			})
			if !bytes.Equal(gotPart, p) {
				t.Fatalf("JoinStatePart(%d) encode mismatch", i)
			}
			n, err := wire.DecodeMembershipMessage(p, &decMem)
			if err != nil || n != len(p) || decMem.Tag != wire.MembershipMessageTagJoinStatePart ||
				int(decMem.JoinStatePart.Part) != hp || int(decMem.JoinStatePart.Parts) != hparts || !bytes.Equal(decMem.JoinStatePart.Data, hdata) {
				t.Fatalf("JoinStatePart(%d) decode mismatch", i)
			}
		}
	})
}

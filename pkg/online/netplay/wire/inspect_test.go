package wire_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
	"go.yaml.in/yaml/v3"
)

func TestDynamicCodecRoundTripAllBuiltins(t *testing.T) {
	reg, err := wire.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	cases := []struct {
		name    string
		isUnion bool
		target  string
		raw     []byte
	}{
		{
			name:   "TransportFrame",
			target: "TransportFrame",
			raw:    wire.AppendTransportFrame(nil, &wire.TransportFrame{Channel: wire.ChannelUnreliable, Seq: 42, Payload: []byte{1, 2, 3, 4}}),
		},
		{
			name:   "UDPPeerFrame",
			target: "UDPPeerFrame",
			raw:    wire.AppendUDPPeerFrame(nil, &wire.UDPPeerFrame{From: 7, Channel: wire.ChannelReliableOrdered, Seq: 9, Payload: []byte{9, 8}}),
		},
		{
			name:   "UDPSealedDatagram",
			target: "UDPSealedDatagram",
			raw: wire.AppendUDPSealedDatagram(nil, &wire.UDPSealedDatagram{
				Type:    wire.UDPDatagramTypeData,
				Peer:    3,
				Seq:     11,
				Channel: wire.ChannelUnreliable,
				Sealed:  []byte{0xAA, 0xBB, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			}),
		},
		{
			name:    "HandshakeHello",
			isUnion: true,
			target:  "HandshakeMessage",
			raw: wire.AppendHandshakeMessage(nil, &wire.HandshakeMessage{
				Tag: wire.HandshakeMessageTagHello,
				Hello: wire.HandshakeHello{
					Nonce:        [16]uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
					ResumePeer:   9,
					ResumeSecret: [16]uint8{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
					Token:        []byte("jwt-token"),
				},
			}),
		},
		{
			name:    "HandshakeReject",
			isUnion: true,
			target:  "HandshakeMessage",
			raw: wire.AppendHandshakeMessage(nil, &wire.HandshakeMessage{
				Tag: wire.HandshakeMessageTagReject,
				Reject: wire.HandshakeReject{
					Nonce:  [16]uint8{9, 9, 9},
					Reason: "session full",
				},
			}),
		},
		{
			name:   "RelayMembers",
			target: "RelayMembers",
			raw:    wire.AppendRelayMembers(nil, &wire.RelayMembers{Epoch: 3, Peers: []uint32{2, 5, 8}}),
		},
		{
			name:    "RelayAck",
			isUnion: true,
			target:  "RelayBody",
			raw: wire.AppendRelayBody(nil, &wire.RelayBody{
				Tag: wire.RelayBodyTagAck,
				Ack: wire.RelayAck{Next: 12, Mask: 0x05},
			}),
		},
		{
			name:    "ReplicationFull",
			isUnion: true,
			target:  "ReplicationMessage",
			raw: wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
				Tag: wire.ReplicationMessageTagFull,
				Full: wire.SnapshotFull{
					Epoch: 1,
					Tick:  10,
					Entities: []wire.EntityRecord{
						{
							ID:         1,
							FieldsMask: wire.EntityRecordMaskValid,
							Kind:       2,
							X:          12.5,
							Y:          -4.25,
							VX:         1.5,
							VY:         0,
							Rotation:   0.75,
							Flags:      0x13,
							Props:      []byte("hero"),
						},
					},
				},
			}),
		},
		{
			name:    "ReplicationDelta",
			isUnion: true,
			target:  "ReplicationMessage",
			raw: wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
				Tag: wire.ReplicationMessageTagDelta,
				Delta: wire.SnapshotDelta{
					Epoch:        1,
					Tick:         11,
					BaselineTick: 10,
					Despawns:     []uint32{99},
					Updates: []wire.EntityRecord{
						{
							ID:         1,
							FieldsMask: wire.EntityRecordFieldX | wire.EntityRecordFieldY,
							X:          14.0,
							Y:          -4.0,
						},
					},
				},
			}),
		},
		{
			name:    "RollbackBundle",
			isUnion: true,
			target:  "RollbackMessage",
			raw: wire.AppendRollbackMessage(nil, &wire.RollbackMessage{
				Tag: wire.RollbackMessageTagInputBundle,
				InputBundle: wire.InputBundle{
					Inputs: []wire.InputFrame{
						{Tick: 1, Peer: 2, Bits: 0x101, Axes: [2]int16{120, -45}},
						{Tick: 2, Peer: 2, Bits: 0x202, Axes: [2]int16{-10, 300}},
					},
				},
			}),
		},
		{
			name:    "MembershipChange",
			isUnion: true,
			target:  "MembershipMessage",
			raw: wire.AppendMembershipMessage(nil, &wire.MembershipMessage{
				Tag: wire.MembershipMessageTagChange,
				Change: wire.MemberChange{
					Kind: wire.MemberChangeKindLeave,
					Peer: 4,
					Tick: 50,
					Inputs: []wire.InputFrame{
						{Tick: 49, Peer: 4, Bits: 7, Axes: [2]int16{1, -1}},
					},
				},
			}),
		},
		{
			name:   "JoinState",
			target: "JoinState",
			raw: wire.AppendJoinState(nil, &wire.JoinState{
				Tick: 30,
				Members: []wire.MatchMember{
					{ID: 1, From: 0, HasLeave: false, Until: 0},
					{ID: 2, From: 10, HasLeave: true, Until: 25},
				},
				Inputs: []wire.InputFrame{{Tick: 29, Peer: 1, Bits: 3}},
				State:  []byte("sim-state"),
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var reencoded []byte
			if tc.isUnion {
				du, n, err := reg.DecodeUnion(tc.target, tc.raw)
				if err != nil {
					t.Fatalf("DecodeUnion(%s): %v", tc.target, err)
				}
				if n != len(tc.raw) {
					t.Fatalf("DecodeUnion(%s) consumed %d of %d bytes", tc.target, n, len(tc.raw))
				}
				reencoded, err = reg.EncodeUnion(du)
				if err != nil {
					t.Fatalf("EncodeUnion(%s): %v", tc.target, err)
				}
			} else {
				obj, n, err := reg.DecodeMessage(tc.target, tc.raw)
				if err != nil {
					t.Fatalf("DecodeMessage(%s): %v", tc.target, err)
				}
				if n != len(tc.raw) {
					t.Fatalf("DecodeMessage(%s) consumed %d of %d bytes", tc.target, n, len(tc.raw))
				}
				reencoded, err = reg.EncodeMessage(tc.target, obj)
				if err != nil {
					t.Fatalf("EncodeMessage(%s): %v", tc.target, err)
				}
			}
			if !bytes.Equal(reencoded, tc.raw) {
				t.Fatalf("reencoded mismatch:\ngot  %x\nwant %x", reencoded, tc.raw)
			}
		})
	}
}

func TestCustomSchemaPropsDecodeResolveAndReencode(t *testing.T) {
	custom, err := wire.LoadSchemaPath(filepath.Join("testdata", "custom_props.wire.yaml"))
	if err != nil {
		t.Fatalf("LoadSchemaPath: %v", err)
	}
	reg, err := wire.NewRegistry(custom...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Build a valid FighterProps payload using the schema's dynamic encoder so we exercise bits, 12-bit qfloat, enum, svarint, fixed, and masked 10-bit qfloat + string + u8.
	propsObj := wire.DecodedObject{
		{Name: "team", Value: uint64(3)},
		{Name: "combo", Value: uint64(7)},
		{Name: "aim_x", Value: float32(0.5)},
		{Name: "aim_y", Value: float32(-0.25)},
		{Name: "stance", Value: wire.DecodedEnum{Name: "jumping", Value: 2}},
		{Name: "hp_delta", Value: int64(-15)},
		{Name: "stamina", Value: float64(73.25)},
		{Name: "state", Value: wire.DecodedMask{
			Mask: 0x07,
			Fields: wire.DecodedObject{
				{Name: "shield", Value: float32(50.0)},
				{Name: "tag", Value: "parry"},
				{Name: "emote", Value: uint64(4)},
			},
		}},
	}
	propsBytes, err := reg.EncodeMessage("FighterProps", propsObj)
	if err != nil {
		t.Fatalf("EncodeMessage(FighterProps): %v", err)
	}

	fullRaw := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
		Tag: wire.ReplicationMessageTagFull,
		Full: wire.SnapshotFull{
			Epoch: 1,
			Tick:  1,
			Entities: []wire.EntityRecord{
				{
					ID:         1,
					FieldsMask: wire.EntityRecordFieldKind | wire.EntityRecordFieldX | wire.EntityRecordFieldY | wire.EntityRecordFieldProps,
					Kind:       1,
					X:          10,
					Y:          20,
					Props:      propsBytes,
				},
			},
		},
	})
	deltaRaw := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
		Tag: wire.ReplicationMessageTagDelta,
		Delta: wire.SnapshotDelta{
			Epoch:        1,
			Tick:         2,
			BaselineTick: 1,
			Updates: []wire.EntityRecord{
				{
					ID:         1,
					FieldsMask: wire.EntityRecordFieldX,
					X:          25,
				},
			},
		},
	})

	ins, err := wire.NewInspector(reg, wire.InspectOptions{ResolveDeltas: true})
	if err != nil {
		t.Fatalf("NewInspector: %v", err)
	}

	f0 := ins.InspectCaptureRecord(0, 0, "send", 2, uint8(wire.ChannelUnreliable), 1, fullRaw)
	f1 := ins.InspectCaptureRecord(1, 16_000_000, "send", 2, uint8(wire.ChannelUnreliable), 2, deltaRaw)

	// Re-encoding both frames must reproduce original payload bytes verbatim, including the re-encoded FighterProps.
	re0, err := ins.ReencodePayload(f0)
	if err != nil {
		t.Fatalf("ReencodePayload(full): %v", err)
	}
	if !bytes.Equal(re0, fullRaw) {
		t.Fatalf("full reencode mismatch:\ngot  %x\nwant %x", re0, fullRaw)
	}
	re1, err := ins.ReencodePayload(f1)
	if err != nil {
		t.Fatalf("ReencodePayload(delta): %v", err)
	}
	if !bytes.Equal(re1, deltaRaw) {
		t.Fatalf("delta reencode mismatch:\ngot  %x\nwant %x", re1, deltaRaw)
	}

	// Verify custom Props decoded in both the full snapshot and the resolved delta entity state.
	yamlBytes, err := yaml.Marshal([]wire.DecodedFrame{f0, f1})
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	out := string(yamlBytes)
	for _, want := range []string{
		"message: FighterProps",
		"stance: jumping",
		"tag: parry",
		"resolved:",
		"x: 25",
		"y: 20",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("YAML output missing %q:\n%s", want, out)
		}
	}

	// Verify JSON marshaling preserves valid JSON.
	jsonBytes, err := json.Marshal([]wire.DecodedFrame{f0, f1})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !json.Valid(jsonBytes) {
		t.Fatalf("invalid JSON output: %s", jsonBytes)
	}
}

func TestDecodeRawFrameTextHexAndBase64(t *testing.T) {
	ins, err := wire.NewInspector(nil, wire.InspectOptions{})
	if err != nil {
		t.Fatalf("NewInspector: %v", err)
	}
	payload := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
		Tag: wire.ReplicationMessageTagAck,
		Ack: wire.SnapshotAck{Epoch: 7, Tick: 99},
	})
	tf := wire.AppendTransportFrame(nil, &wire.TransportFrame{
		Channel: wire.ChannelReliableOrdered,
		Seq:     5,
		Payload: payload,
	})

	hexStr := hex.EncodeToString(tf)
	b64Str := base64.StdEncoding.EncodeToString(tf)

	for _, tc := range []struct {
		name string
		in   string
	}{
		{"plain_hex", hexStr},
		{"0x_hex", "0x" + hexStr},
		{"spaced_hex", strings.Join([]string{hexStr[:4], hexStr[4:8], hexStr[8:]}, " ")},
		{"base64", b64Str},
		{"raw_base64", base64.RawStdEncoding.EncodeToString(tf)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ins.DecodeRawFrameText(tc.in)
			if err != nil {
				t.Fatalf("DecodeRawFrameText(%s): %v", tc.name, err)
			}
			if !bytes.Equal(got, tf) {
				t.Fatalf("got %x, want %x", got, tf)
			}
			fr := ins.InspectRawFrame(0, got)
			if len(fr.Layers) != 2 || fr.Layers[0].Layer != "transport" || fr.Layers[1].Layer != "replication" || fr.Layers[1].Case != "ack" {
				t.Fatalf("unexpected layers: %+v", fr.Layers)
			}
			re, err := ins.ReencodePayload(fr)
			if err != nil {
				t.Fatalf("ReencodePayload: %v", err)
			}
			if !bytes.Equal(re, tf) {
				t.Fatalf("reencode mismatch: got %x, want %x", re, tf)
			}
		})
	}
}

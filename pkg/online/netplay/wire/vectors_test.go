package wire_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
	"go.yaml.in/yaml/v3"
)

var updateVectors = flag.Bool("update-vectors", false, "regenerate pkg/online/netplay/wire/testdata/vectors/*.yaml")

type quotedHex string

func (q quotedHex) MarshalYAML() (any, error) {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Style: yaml.DoubleQuotedStyle,
		Value: string(q),
	}, nil
}

type vectorFile struct {
	Version int             `yaml:"version"`
	Schema  string          `yaml:"schema"`
	Vectors []messageVector `yaml:"vectors,omitempty"`
	Deltas  []deltaVector   `yaml:"deltas,omitempty"`
	Invalid []invalidVector `yaml:"invalid,omitempty"`
}

type messageVector struct {
	Name    string    `yaml:"name"`
	Type    string    `yaml:"type"`
	Hex     quotedHex `yaml:"hex"`
	Value   yaml.Node `yaml:"value"`
	Decoded yaml.Node `yaml:"decoded,omitempty"`
}

type deltaVector struct {
	Name     string    `yaml:"name"`
	Type     string    `yaml:"type"`
	Base     yaml.Node `yaml:"base"`
	Cur      yaml.Node `yaml:"cur"`
	DiffMask uint64    `yaml:"diffMask"`
	DeltaHex quotedHex `yaml:"deltaHex"`
	Expected yaml.Node `yaml:"expected"`
}

type invalidVector struct {
	Name  string    `yaml:"name"`
	Type  string    `yaml:"type"`
	Hex   quotedHex `yaml:"hex"`
	Error string    `yaml:"error"`
}

type quantizeVectorFile struct {
	Version       int                  `yaml:"version"`
	Schema        string               `yaml:"schema"`
	QFloat        []qfloatVector       `yaml:"qfloat"`
	FixedSigned   []fixedSignedVector  `yaml:"fixedSigned"`
	FixedUnsigned []fixedUnsignedEntry `yaml:"fixedUnsigned"`
}

type qfloatVector struct {
	Name        string  `yaml:"name"`
	VBits       string  `yaml:"vBits"`
	Min         float32 `yaml:"min"`
	Max         float32 `yaml:"max"`
	Bits        int     `yaml:"bits"`
	WantQ       uint32  `yaml:"wantQ"`
	WantDeqBits string  `yaml:"wantDeqBits"`
}

type fixedSignedVector struct {
	Name        string `yaml:"name"`
	VBits       string `yaml:"vBits"`
	Scale       string `yaml:"scale"`
	MinInt      string `yaml:"minInt"`
	MaxInt      string `yaml:"maxInt"`
	WantQ       string `yaml:"wantQ"`
	WantDeqBits string `yaml:"wantDeqBits"`
}

type fixedUnsignedEntry struct {
	Name        string `yaml:"name"`
	VBits       string `yaml:"vBits"`
	Scale       string `yaml:"scale"`
	MaxUint     string `yaml:"maxUint"`
	WantQ       string `yaml:"wantQ"`
	WantDeqBits string `yaml:"wantDeqBits"`
}

// YAML helpers preserving -0.0, .nan, .inf, -.inf, decimal strings for 64-bit ints, and hex strings for byte slices.
func yamlStr(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func yamlHex(b []byte) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: yaml.DoubleQuotedStyle, Value: hex.EncodeToString(b)}
}

func yamlInt(v int64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(v, 10)}
}

func yamlUint(v uint64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(v, 10)}
}

func yamlBigUint(v uint64) *yaml.Node {
	return yamlStr(strconv.FormatUint(v, 10))
}

func yamlBigInt(v int64) *yaml.Node {
	return yamlStr(strconv.FormatInt(v, 10))
}

func yamlBool(v bool) *yaml.Node {
	if v {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
}

func yamlF32(v float32) *yaml.Node {
	bits := math.Float32bits(v)
	switch {
	case math.IsNaN(float64(v)):
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".nan"}
	case math.IsInf(float64(v), 1):
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".inf"}
	case math.IsInf(float64(v), -1):
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: "-.inf"}
	case bits == 0x80000000:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: "-0.0"}
	case bits == 0:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "0"}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(float64(v), 'g', -1, 32)}
	}
}

func yamlF64(v float64) *yaml.Node {
	bits := math.Float64bits(v)
	switch {
	case math.IsNaN(v):
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".nan"}
	case math.IsInf(v, 1):
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".inf"}
	case math.IsInf(v, -1):
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: "-.inf"}
	case bits == 0x8000000000000000:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: "-0.0"}
	case bits == 0:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "0"}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(v, 'g', -1, 64)}
	}
}

func yamlMap(kv ...any) yaml.Node {
	n := yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i < len(kv); i += 2 {
		k := kv[i].(string)
		val := kv[i+1].(*yaml.Node)
		n.Content = append(n.Content, yamlStr(k), val)
	}
	return n
}

func yamlSeq(items ...*yaml.Node) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: items}
	return n
}

func nodePtr(n yaml.Node) *yaml.Node { return &n }

type fieldMap map[string]*yaml.Node

func asFieldMap(t *testing.T, n *yaml.Node) fieldMap {
	t.Helper()
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		t.Fatalf("expected YAML mapping, got kind %d", n.Kind)
	}
	m := make(fieldMap, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		m[n.Content[i].Value] = n.Content[i+1]
	}
	return m
}

func (m fieldMap) u64(t *testing.T, k string) uint64 {
	t.Helper()
	n, ok := m[k]
	if !ok {
		t.Fatalf("missing field %q", k)
	}
	v, err := strconv.ParseUint(n.Value, 10, 64)
	if err != nil {
		t.Fatalf("field %q = %q: %v", k, n.Value, err)
	}
	return v
}

func (m fieldMap) i64(t *testing.T, k string) int64 {
	t.Helper()
	n, ok := m[k]
	if !ok {
		t.Fatalf("missing field %q", k)
	}
	v, err := strconv.ParseInt(n.Value, 10, 64)
	if err != nil {
		t.Fatalf("field %q = %q: %v", k, n.Value, err)
	}
	return v
}

func (m fieldMap) boolVal(t *testing.T, k string) bool {
	t.Helper()
	n, ok := m[k]
	if !ok {
		t.Fatalf("missing field %q", k)
	}
	return n.Value == "true"
}

func (m fieldMap) strVal(t *testing.T, k string) string {
	t.Helper()
	n, ok := m[k]
	if !ok {
		t.Fatalf("missing field %q", k)
	}
	return n.Value
}

func (m fieldMap) hexBytes(t *testing.T, k string) []byte {
	t.Helper()
	s := m.strVal(t, k)
	if s == "" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("field %q hex: %v", k, err)
	}
	return b
}

func (m fieldMap) f32(t *testing.T, k string) float32 {
	t.Helper()
	n, ok := m[k]
	if !ok {
		t.Fatalf("missing field %q", k)
	}
	switch n.Value {
	case ".nan", ".NaN", ".NAN":
		return float32(math.NaN())
	case ".inf", ".Inf", ".INF", "+.inf":
		return float32(math.Inf(1))
	case "-.inf", "-.Inf", "-.INF":
		return float32(math.Inf(-1))
	case "-0.0", "-0":
		return float32(math.Copysign(0, -1))
	}
	v, err := strconv.ParseFloat(n.Value, 32)
	if err != nil {
		t.Fatalf("field %q float32 %q: %v", k, n.Value, err)
	}
	return float32(v)
}

func (m fieldMap) f64(t *testing.T, k string) float64 {
	t.Helper()
	n, ok := m[k]
	if !ok {
		t.Fatalf("missing field %q", k)
	}
	switch n.Value {
	case ".nan", ".NaN", ".NAN":
		return math.NaN()
	case ".inf", ".Inf", ".INF", "+.inf":
		return math.Inf(1)
	case "-.inf", "-.Inf", "-.INF":
		return math.Inf(-1)
	case "-0.0", "-0":
		return math.Copysign(0, -1)
	}
	v, err := strconv.ParseFloat(n.Value, 64)
	if err != nil {
		t.Fatalf("field %q float64 %q: %v", k, n.Value, err)
	}
	return v
}

func (m fieldMap) seq(t *testing.T, k string) []*yaml.Node {
	t.Helper()
	n, ok := m[k]
	if !ok {
		t.Fatalf("missing field %q", k)
	}
	if n.Kind != yaml.SequenceNode {
		t.Fatalf("field %q: expected sequence, got kind %d", k, n.Kind)
	}
	return n.Content
}

func (m fieldMap) obj(t *testing.T, k string) fieldMap {
	t.Helper()
	n, ok := m[k]
	if !ok {
		t.Fatalf("missing field %q", k)
	}
	return asFieldMap(t, n)
}

func errorCodeFor(err error) string {
	switch {
	case errors.Is(err, wire.ErrShortBuffer):
		return "short_buffer"
	case errors.Is(err, wire.ErrOverflow):
		return "overflow"
	case errors.Is(err, wire.ErrInvalidBool):
		return "invalid_bool"
	case errors.Is(err, wire.ErrInvalidEnum):
		return "invalid_enum"
	case errors.Is(err, wire.ErrInvalidTag):
		return "invalid_tag"
	case errors.Is(err, wire.ErrInvalidVersion):
		return "invalid_version"
	case errors.Is(err, wire.ErrInvalidConst):
		return "invalid_const"
	case errors.Is(err, wire.ErrInvalidMask):
		return "invalid_mask"
	case errors.Is(err, wire.ErrNonZeroPadding):
		return "non_zero_padding"
	case errors.Is(err, wire.ErrLimitExceeded):
		return "limit_exceeded"
	default:
		return fmt.Sprintf("unknown(%v)", err)
	}
}

func eqF32(a, b float32) bool {
	return math.Float32bits(a) == math.Float32bits(b)
}

func eqF64(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b)
}

// Type codecs for YAML <-> Go structs for every message/union in the golden vector suite.
type vectorCodec struct {
	encode func(t *testing.T, n *yaml.Node) []byte
	decode func(t *testing.T, raw []byte) (int, yaml.Node, error)
	equal  func(t *testing.T, want, got *yaml.Node) error
	diff   func(t *testing.T, base, cur *yaml.Node) (uint64, []byte, yaml.Node)
	apply  func(t *testing.T, base *yaml.Node, deltaRaw []byte) (yaml.Node, error)
}

func toYAMLTransportFrame(m wire.TransportFrame) yaml.Node {
	return yamlMap("channel", yamlUint(uint64(m.Channel)), "seq", yamlUint(uint64(m.Seq)), "payload", yamlHex(m.Payload))
}
func fromYAMLTransportFrame(t *testing.T, n *yaml.Node) wire.TransportFrame {
	f := asFieldMap(t, n)
	return wire.TransportFrame{Channel: wire.Channel(f.u64(t, "channel")), Seq: uint32(f.u64(t, "seq")), Payload: f.hexBytes(t, "payload")}
}

func toYAMLUDPPeerFrame(m wire.UDPPeerFrame) yaml.Node {
	return yamlMap("from", yamlUint(uint64(m.From)), "channel", yamlUint(uint64(m.Channel)), "seq", yamlUint(uint64(m.Seq)), "payload", yamlHex(m.Payload))
}
func fromYAMLUDPPeerFrame(t *testing.T, n *yaml.Node) wire.UDPPeerFrame {
	f := asFieldMap(t, n)
	return wire.UDPPeerFrame{From: uint32(f.u64(t, "from")), Channel: wire.Channel(f.u64(t, "channel")), Seq: uint32(f.u64(t, "seq")), Payload: f.hexBytes(t, "payload")}
}

func toYAMLUDPSealedDatagram(m wire.UDPSealedDatagram) yaml.Node {
	return yamlMap(
		"type", yamlUint(uint64(m.Type)),
		"peer", yamlUint(uint64(m.Peer)),
		"seq", yamlUint(uint64(m.Seq)),
		"channel", yamlUint(uint64(m.Channel)),
		"sealed", yamlHex(m.Sealed),
	)
}
func fromYAMLUDPSealedDatagram(t *testing.T, n *yaml.Node) wire.UDPSealedDatagram {
	f := asFieldMap(t, n)
	var m wire.UDPSealedDatagram
	m.Type = wire.UDPDatagramType(f.u64(t, "type"))
	m.Peer = uint32(f.u64(t, "peer"))
	m.Seq = uint32(f.u64(t, "seq"))
	m.Channel = wire.Channel(f.u64(t, "channel"))
	m.Sealed = f.hexBytes(t, "sealed")
	return m
}

func toYAMLHandshakeMessage(m wire.HandshakeMessage) yaml.Node {
	switch m.Tag {
	case wire.HandshakeMessageTagHello:
		return yamlMap(
			"tag", yamlUint(uint64(m.Tag)),
			"hello", nodePtr(yamlMap(
				"nonce", yamlHex(m.Hello.Nonce[:]),
				"resumePeer", yamlUint(uint64(m.Hello.ResumePeer)),
				"resumeSecret", yamlHex(m.Hello.ResumeSecret[:]),
				"token", yamlHex(m.Hello.Token),
			)),
		)
	case wire.HandshakeMessageTagWelcome:
		return yamlMap(
			"tag", yamlUint(uint64(m.Tag)),
			"welcome", nodePtr(yamlMap(
				"nonce", yamlHex(m.Welcome.Nonce[:]),
				"peer", yamlUint(uint64(m.Welcome.Peer)),
				"server", yamlUint(uint64(m.Welcome.Server)),
				"resumed", yamlBool(m.Welcome.Resumed),
				"secret", yamlHex(m.Welcome.Secret[:]),
				"key", yamlHex(m.Welcome.Key[:]),
			)),
		)
	default:
		return yamlMap(
			"tag", yamlUint(uint64(m.Tag)),
			"reject", nodePtr(yamlMap(
				"nonce", yamlHex(m.Reject.Nonce[:]),
				"reason", yamlStr(m.Reject.Reason),
			)),
		)
	}
}
func fromYAMLHandshakeMessage(t *testing.T, n *yaml.Node) wire.HandshakeMessage {
	f := asFieldMap(t, n)
	var m wire.HandshakeMessage
	m.Tag = uint8(f.u64(t, "tag"))
	switch m.Tag {
	case wire.HandshakeMessageTagHello:
		h := f.obj(t, "hello")
		copy(m.Hello.Nonce[:], h.hexBytes(t, "nonce"))
		m.Hello.ResumePeer = uint32(h.u64(t, "resumePeer"))
		copy(m.Hello.ResumeSecret[:], h.hexBytes(t, "resumeSecret"))
		m.Hello.Token = h.hexBytes(t, "token")
	case wire.HandshakeMessageTagWelcome:
		w := f.obj(t, "welcome")
		copy(m.Welcome.Nonce[:], w.hexBytes(t, "nonce"))
		m.Welcome.Peer = uint32(w.u64(t, "peer"))
		m.Welcome.Server = uint32(w.u64(t, "server"))
		m.Welcome.Resumed = w.boolVal(t, "resumed")
		copy(m.Welcome.Secret[:], w.hexBytes(t, "secret"))
		copy(m.Welcome.Key[:], w.hexBytes(t, "key"))
	case wire.HandshakeMessageTagReject:
		r := f.obj(t, "reject")
		copy(m.Reject.Nonce[:], r.hexBytes(t, "nonce"))
		m.Reject.Reason = r.strVal(t, "reason")
	}
	return m
}

func toYAMLRelayFrame(m wire.RelayFrame) yaml.Node {
	return yamlMap("peer", yamlUint(uint64(m.Peer)), "body", yamlHex(m.Body))
}
func fromYAMLRelayFrame(t *testing.T, n *yaml.Node) wire.RelayFrame {
	f := asFieldMap(t, n)
	return wire.RelayFrame{Peer: uint32(f.u64(t, "peer")), Body: f.hexBytes(t, "body")}
}

func toYAMLRelayMembers(m wire.RelayMembers) yaml.Node {
	peers := make([]*yaml.Node, len(m.Peers))
	for i, p := range m.Peers {
		peers[i] = yamlUint(uint64(p))
	}
	return yamlMap("epoch", yamlUint(uint64(m.Epoch)), "peers", yamlSeq(peers...))
}
func fromYAMLRelayMembers(t *testing.T, n *yaml.Node) wire.RelayMembers {
	f := asFieldMap(t, n)
	seq := f.seq(t, "peers")
	peers := make([]uint32, len(seq))
	for i, item := range seq {
		v, err := strconv.ParseUint(item.Value, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		peers[i] = uint32(v)
	}
	return wire.RelayMembers{Epoch: uint32(f.u64(t, "epoch")), Peers: peers}
}

func toYAMLRelayBody(m wire.RelayBody) yaml.Node {
	switch m.Tag {
	case wire.RelayBodyTagUnreliable:
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "unreliable", nodePtr(yamlMap("seq", yamlUint(uint64(m.Unreliable.Seq)), "payload", yamlHex(m.Unreliable.Payload))))
	case wire.RelayBodyTagReliable:
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "reliable", nodePtr(yamlMap("seq", yamlUint(uint64(m.Reliable.Seq)), "payload", yamlHex(m.Reliable.Payload))))
	default:
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "ack", nodePtr(yamlMap("next", yamlUint(uint64(m.Ack.Next)), "mask", yamlBigUint(m.Ack.Mask))))
	}
}
func fromYAMLRelayBody(t *testing.T, n *yaml.Node) wire.RelayBody {
	f := asFieldMap(t, n)
	var m wire.RelayBody
	m.Tag = uint8(f.u64(t, "tag"))
	switch m.Tag {
	case wire.RelayBodyTagUnreliable:
		u := f.obj(t, "unreliable")
		m.Unreliable.Seq = uint32(u.u64(t, "seq"))
		m.Unreliable.Payload = u.hexBytes(t, "payload")
	case wire.RelayBodyTagReliable:
		r := f.obj(t, "reliable")
		m.Reliable.Seq = uint32(r.u64(t, "seq"))
		m.Reliable.Payload = r.hexBytes(t, "payload")
	case wire.RelayBodyTagAck:
		a := f.obj(t, "ack")
		m.Ack.Next = uint32(a.u64(t, "next"))
		m.Ack.Mask = a.u64(t, "mask")
	}
	return m
}

func toYAMLEntityRecord(m wire.EntityRecord) yaml.Node {
	return yamlMap(
		"id", yamlUint(uint64(m.ID)),
		"fieldsMask", yamlUint(uint64(m.FieldsMask)),
		"kind", yamlUint(uint64(m.Kind)),
		"x", yamlF32(m.X),
		"y", yamlF32(m.Y),
		"vx", yamlF32(m.VX),
		"vy", yamlF32(m.VY),
		"rotation", yamlF32(m.Rotation),
		"flags", yamlUint(uint64(m.Flags)),
		"props", yamlHex(m.Props),
	)
}
func fromYAMLEntityRecord(t *testing.T, n *yaml.Node) wire.EntityRecord {
	f := asFieldMap(t, n)
	return wire.EntityRecord{
		ID:         uint32(f.u64(t, "id")),
		FieldsMask: uint8(f.u64(t, "fieldsMask")),
		Kind:       uint16(f.u64(t, "kind")),
		X:          f.f32(t, "x"),
		Y:          f.f32(t, "y"),
		VX:         f.f32(t, "vx"),
		VY:         f.f32(t, "vy"),
		Rotation:   f.f32(t, "rotation"),
		Flags:      uint32(f.u64(t, "flags")),
		Props:      f.hexBytes(t, "props"),
	}
}
func eqEntityRecord(a, b wire.EntityRecord) error {
	if a.ID != b.ID || a.FieldsMask != b.FieldsMask || a.Kind != b.Kind ||
		!eqF32(a.X, b.X) || !eqF32(a.Y, b.Y) || !eqF32(a.VX, b.VX) || !eqF32(a.VY, b.VY) || !eqF32(a.Rotation, b.Rotation) ||
		a.Flags != b.Flags || !bytes.Equal(a.Props, b.Props) {
		return fmt.Errorf("EntityRecord mismatch:\nwant %+v\ngot  %+v", a, b)
	}
	return nil
}

func toYAMLReplicationMessage(m wire.ReplicationMessage) yaml.Node {
	switch m.Tag {
	case wire.ReplicationMessageTagFull:
		ents := make([]*yaml.Node, len(m.Full.Entities))
		for i, e := range m.Full.Entities {
			ents[i] = nodePtr(toYAMLEntityRecord(e))
		}
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "version", yamlUint(uint64(m.Version)), "full", nodePtr(yamlMap("epoch", yamlUint(uint64(m.Full.Epoch)), "tick", yamlUint(uint64(m.Full.Tick)), "entities", yamlSeq(ents...))))
	case wire.ReplicationMessageTagDelta:
		desp := make([]*yaml.Node, len(m.Delta.Despawns))
		for i, d := range m.Delta.Despawns {
			desp[i] = yamlUint(uint64(d))
		}
		upds := make([]*yaml.Node, len(m.Delta.Updates))
		for i, e := range m.Delta.Updates {
			upds[i] = nodePtr(toYAMLEntityRecord(e))
		}
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "version", yamlUint(uint64(m.Version)), "delta", nodePtr(yamlMap("epoch", yamlUint(uint64(m.Delta.Epoch)), "tick", yamlUint(uint64(m.Delta.Tick)), "baselineTick", yamlUint(uint64(m.Delta.BaselineTick)), "despawns", yamlSeq(desp...), "updates", yamlSeq(upds...))))
	case wire.ReplicationMessageTagAck:
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "version", yamlUint(uint64(m.Version)), "ack", nodePtr(yamlMap("epoch", yamlUint(uint64(m.Ack.Epoch)), "tick", yamlUint(uint64(m.Ack.Tick)))))
	case wire.ReplicationMessageTagResync:
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "version", yamlUint(uint64(m.Version)), "resync", nodePtr(yamlMap("epoch", yamlUint(uint64(m.Resync.Epoch)))))
	default:
		ents := make([]*yaml.Node, len(m.FullPart.Entities))
		for i, e := range m.FullPart.Entities {
			ents[i] = nodePtr(toYAMLEntityRecord(e))
		}
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "version", yamlUint(uint64(m.Version)), "fullPart", nodePtr(yamlMap("epoch", yamlUint(uint64(m.FullPart.Epoch)), "tick", yamlUint(uint64(m.FullPart.Tick)), "index", yamlUint(uint64(m.FullPart.Index)), "total", yamlUint(uint64(m.FullPart.Total)), "entities", yamlSeq(ents...))))
	}
}
func fromYAMLReplicationMessage(t *testing.T, n *yaml.Node) wire.ReplicationMessage {
	f := asFieldMap(t, n)
	var m wire.ReplicationMessage
	m.Tag = uint8(f.u64(t, "tag"))
	m.Version = uint8(f.u64(t, "version"))
	switch m.Tag {
	case wire.ReplicationMessageTagFull:
		sub := f.obj(t, "full")
		m.Full.Epoch = uint32(sub.u64(t, "epoch"))
		m.Full.Tick = uint32(sub.u64(t, "tick"))
		for _, item := range sub.seq(t, "entities") {
			m.Full.Entities = append(m.Full.Entities, fromYAMLEntityRecord(t, item))
		}
	case wire.ReplicationMessageTagDelta:
		sub := f.obj(t, "delta")
		m.Delta.Epoch = uint32(sub.u64(t, "epoch"))
		m.Delta.Tick = uint32(sub.u64(t, "tick"))
		m.Delta.BaselineTick = uint32(sub.u64(t, "baselineTick"))
		for _, item := range sub.seq(t, "despawns") {
			v, _ := strconv.ParseUint(item.Value, 10, 32)
			m.Delta.Despawns = append(m.Delta.Despawns, uint32(v))
		}
		for _, item := range sub.seq(t, "updates") {
			m.Delta.Updates = append(m.Delta.Updates, fromYAMLEntityRecord(t, item))
		}
	case wire.ReplicationMessageTagAck:
		sub := f.obj(t, "ack")
		m.Ack.Epoch = uint32(sub.u64(t, "epoch"))
		m.Ack.Tick = uint32(sub.u64(t, "tick"))
	case wire.ReplicationMessageTagResync:
		sub := f.obj(t, "resync")
		m.Resync.Epoch = uint32(sub.u64(t, "epoch"))
	case wire.ReplicationMessageTagFullPart:
		sub := f.obj(t, "fullPart")
		m.FullPart.Epoch = uint32(sub.u64(t, "epoch"))
		m.FullPart.Tick = uint32(sub.u64(t, "tick"))
		m.FullPart.Index = uint32(sub.u64(t, "index"))
		m.FullPart.Total = uint32(sub.u64(t, "total"))
		for _, item := range sub.seq(t, "entities") {
			m.FullPart.Entities = append(m.FullPart.Entities, fromYAMLEntityRecord(t, item))
		}
	}
	return m
}

func toYAMLInputFrame(m wire.InputFrame) yaml.Node {
	return yamlMap(
		"tick", yamlUint(uint64(m.Tick)),
		"peer", yamlUint(uint64(m.Peer)),
		"bits", yamlBigUint(m.Bits),
		"axes", yamlSeq(yamlInt(int64(m.Axes[0])), yamlInt(int64(m.Axes[1]))),
	)
}
func fromYAMLInputFrame(t *testing.T, n *yaml.Node) wire.InputFrame {
	f := asFieldMap(t, n)
	axes := f.seq(t, "axes")
	a0, _ := strconv.ParseInt(axes[0].Value, 10, 16)
	a1, _ := strconv.ParseInt(axes[1].Value, 10, 16)
	return wire.InputFrame{Tick: uint32(f.u64(t, "tick")), Peer: uint32(f.u64(t, "peer")), Bits: f.u64(t, "bits"), Axes: [2]int16{int16(a0), int16(a1)}}
}

func toYAMLRollbackMessage(m wire.RollbackMessage) yaml.Node {
	switch m.Tag {
	case wire.RollbackMessageTagSingleInput:
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "singleInput", nodePtr(yamlMap("input", nodePtr(toYAMLInputFrame(m.SingleInput.Input)))))
	case wire.RollbackMessageTagInputBundle:
		items := make([]*yaml.Node, len(m.InputBundle.Inputs))
		for i, in := range m.InputBundle.Inputs {
			items[i] = nodePtr(toYAMLInputFrame(in))
		}
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "inputBundle", nodePtr(yamlMap("inputs", yamlSeq(items...))))
	default:
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "checksum", nodePtr(yamlMap("tick", yamlUint(uint64(m.Checksum.Tick)), "peer", yamlUint(uint64(m.Checksum.Peer)), "checksum", yamlBigUint(m.Checksum.Checksum))))
	}
}
func fromYAMLRollbackMessage(t *testing.T, n *yaml.Node) wire.RollbackMessage {
	f := asFieldMap(t, n)
	var m wire.RollbackMessage
	m.Tag = uint8(f.u64(t, "tag"))
	switch m.Tag {
	case wire.RollbackMessageTagSingleInput:
		sub := f.obj(t, "singleInput")
		m.SingleInput.Input = fromYAMLInputFrame(t, sub["input"])
	case wire.RollbackMessageTagInputBundle:
		sub := f.obj(t, "inputBundle")
		for _, item := range sub.seq(t, "inputs") {
			m.InputBundle.Inputs = append(m.InputBundle.Inputs, fromYAMLInputFrame(t, item))
		}
	case wire.RollbackMessageTagChecksum:
		sub := f.obj(t, "checksum")
		m.Checksum.Tick = uint32(sub.u64(t, "tick"))
		m.Checksum.Peer = uint32(sub.u64(t, "peer"))
		m.Checksum.Checksum = sub.u64(t, "checksum")
	}
	return m
}

func toYAMLMatchMember(m wire.MatchMember) yaml.Node {
	return yamlMap("id", yamlUint(uint64(m.ID)), "from", yamlUint(uint64(m.From)), "hasLeave", yamlBool(m.HasLeave), "until", yamlUint(uint64(m.Until)))
}
func fromYAMLMatchMember(t *testing.T, n *yaml.Node) wire.MatchMember {
	f := asFieldMap(t, n)
	return wire.MatchMember{ID: uint32(f.u64(t, "id")), From: uint32(f.u64(t, "from")), HasLeave: f.boolVal(t, "hasLeave"), Until: uint32(f.u64(t, "until"))}
}

func toYAMLJoinState(m wire.JoinState) yaml.Node {
	mems := make([]*yaml.Node, len(m.Members))
	for i, mem := range m.Members {
		mems[i] = nodePtr(toYAMLMatchMember(mem))
	}
	ins := make([]*yaml.Node, len(m.Inputs))
	for i, in := range m.Inputs {
		ins[i] = nodePtr(toYAMLInputFrame(in))
	}
	return yamlMap("tick", yamlUint(uint64(m.Tick)), "members", yamlSeq(mems...), "inputs", yamlSeq(ins...), "state", yamlHex(m.State))
}
func fromYAMLJoinState(t *testing.T, n *yaml.Node) wire.JoinState {
	f := asFieldMap(t, n)
	var m wire.JoinState
	m.Tick = uint32(f.u64(t, "tick"))
	for _, item := range f.seq(t, "members") {
		m.Members = append(m.Members, fromYAMLMatchMember(t, item))
	}
	for _, item := range f.seq(t, "inputs") {
		m.Inputs = append(m.Inputs, fromYAMLInputFrame(t, item))
	}
	m.State = f.hexBytes(t, "state")
	return m
}

func toYAMLMembershipMessage(m wire.MembershipMessage) yaml.Node {
	switch m.Tag {
	case wire.MembershipMessageTagChange:
		ins := make([]*yaml.Node, len(m.Change.Inputs))
		for i, in := range m.Change.Inputs {
			ins[i] = nodePtr(toYAMLInputFrame(in))
		}
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "change", nodePtr(yamlMap("kind", yamlUint(uint64(m.Change.Kind)), "peer", yamlUint(uint64(m.Change.Peer)), "tick", yamlUint(uint64(m.Change.Tick)), "inputs", yamlSeq(ins...))))
	default:
		return yamlMap("tag", yamlUint(uint64(m.Tag)), "joinStatePart", nodePtr(yamlMap("part", yamlUint(uint64(m.JoinStatePart.Part)), "parts", yamlUint(uint64(m.JoinStatePart.Parts)), "data", yamlHex(m.JoinStatePart.Data))))
	}
}
func fromYAMLMembershipMessage(t *testing.T, n *yaml.Node) wire.MembershipMessage {
	f := asFieldMap(t, n)
	var m wire.MembershipMessage
	m.Tag = uint8(f.u64(t, "tag"))
	switch m.Tag {
	case wire.MembershipMessageTagChange:
		sub := f.obj(t, "change")
		m.Change.Kind = wire.MemberChangeKind(sub.u64(t, "kind"))
		m.Change.Peer = uint32(sub.u64(t, "peer"))
		m.Change.Tick = uint32(sub.u64(t, "tick"))
		for _, item := range sub.seq(t, "inputs") {
			m.Change.Inputs = append(m.Change.Inputs, fromYAMLInputFrame(t, item))
		}
	case wire.MembershipMessageTagJoinStatePart:
		sub := f.obj(t, "joinStatePart")
		m.JoinStatePart.Part = uint16(sub.u64(t, "part"))
		m.JoinStatePart.Parts = uint16(sub.u64(t, "parts"))
		m.JoinStatePart.Data = sub.hexBytes(t, "data")
	}
	return m
}

func toYAMLFighterProps(m FighterProps) yaml.Node {
	return yamlMap(
		"team", yamlUint(uint64(m.Team)),
		"combo", yamlUint(uint64(m.Combo)),
		"aimX", yamlF32(m.AimX),
		"aimY", yamlF32(m.AimY),
		"stance", yamlUint(uint64(m.Stance)),
		"hpDelta", yamlInt(int64(m.HpDelta)),
		"stamina", yamlF64(m.Stamina),
		"stateMask", yamlUint(uint64(m.StateMask)),
		"shield", yamlF32(m.Shield),
		"tag", yamlStr(m.Tag),
		"emote", yamlUint(uint64(m.Emote)),
	)
}
func fromYAMLFighterProps(t *testing.T, n *yaml.Node) FighterProps {
	f := asFieldMap(t, n)
	return FighterProps{
		Team:      uint8(f.u64(t, "team")),
		Combo:     uint8(f.u64(t, "combo")),
		AimX:      f.f32(t, "aimX"),
		AimY:      f.f32(t, "aimY"),
		Stance:    Stance(f.u64(t, "stance")),
		HpDelta:   int32(f.i64(t, "hpDelta")),
		Stamina:   f.f64(t, "stamina"),
		StateMask: uint8(f.u64(t, "stateMask")),
		Shield:    f.f32(t, "shield"),
		Tag:       f.strVal(t, "tag"),
		Emote:     uint8(f.u64(t, "emote")),
	}
}

func toYAMLMixedEndian(m MixedEndian) yaml.Node {
	return yamlMap(
		"beU16", yamlUint(uint64(m.BeU16)),
		"leU16", yamlUint(uint64(m.LeU16)),
		"beU32", yamlUint(uint64(m.BeU32)),
		"leU32", yamlUint(uint64(m.LeU32)),
		"beU64", yamlBigUint(m.BeU64),
		"leU64", yamlBigUint(m.LeU64),
		"beI64", yamlBigInt(m.BeI64),
		"leI64", yamlBigInt(m.LeI64),
		"beF32", yamlF32(m.BeF32),
		"leF32", yamlF32(m.LeF32),
		"mode", yamlBigUint(uint64(m.Mode)),
	)
}
func fromYAMLMixedEndian(t *testing.T, n *yaml.Node) MixedEndian {
	f := asFieldMap(t, n)
	return MixedEndian{
		BeU16: uint16(f.u64(t, "beU16")),
		LeU16: uint16(f.u64(t, "leU16")),
		BeU32: uint32(f.u64(t, "beU32")),
		LeU32: uint32(f.u64(t, "leU32")),
		BeU64: f.u64(t, "beU64"),
		LeU64: f.u64(t, "leU64"),
		BeI64: f.i64(t, "beI64"),
		LeI64: f.i64(t, "leI64"),
		BeF32: f.f32(t, "beF32"),
		LeF32: f.f32(t, "leF32"),
		Mode:  BigMode(f.u64(t, "mode")),
	}
}

func toYAMLVarintEdge(m VarintEdge) yaml.Node {
	return yamlMap(
		"u8V", yamlUint(uint64(m.U8V)),
		"u16V", yamlUint(uint64(m.U16V)),
		"u32V", yamlUint(uint64(m.U32V)),
		"u64V", yamlBigUint(m.U64V),
		"s8V", yamlInt(int64(m.S8V)),
		"s16V", yamlInt(int64(m.S16V)),
		"s32V", yamlInt(int64(m.S32V)),
		"s64V", yamlBigInt(m.S64V),
	)
}
func fromYAMLVarintEdge(t *testing.T, n *yaml.Node) VarintEdge {
	f := asFieldMap(t, n)
	return VarintEdge{
		U8V:  uint8(f.u64(t, "u8V")),
		U16V: uint16(f.u64(t, "u16V")),
		U32V: uint32(f.u64(t, "u32V")),
		U64V: f.u64(t, "u64V"),
		S8V:  int8(f.i64(t, "s8V")),
		S16V: int16(f.i64(t, "s16V")),
		S32V: int32(f.i64(t, "s32V")),
		S64V: f.i64(t, "s64V"),
	}
}

func toYAMLQuantizedPacket(m QuantizedPacket) yaml.Node {
	return yamlMap(
		"flag1", yamlUint(uint64(m.Flag1)),
		"tag3", yamlUint(uint64(m.Tag3)),
		"wide40", yamlBigUint(m.Wide40),
		"q1Unit", yamlF32(m.Q1Unit),
		"q12Angle", yamlF32(m.Q12Angle),
		"q10Asym", yamlF32(m.Q10Asym),
		"q8Le", yamlF32(m.Q8Le),
		"q16Le", yamlF32(m.Q16Le),
		"q32Le", yamlF32(m.Q32Le),
		"fxI16", yamlF64(m.FxI16),
		"fxSvar", yamlF64(m.FxSvar),
		"fxU64", yamlF64(m.FxU64),
	)
}
func fromYAMLQuantizedPacket(t *testing.T, n *yaml.Node) QuantizedPacket {
	f := asFieldMap(t, n)
	return QuantizedPacket{
		Flag1:    uint8(f.u64(t, "flag1")),
		Tag3:     uint8(f.u64(t, "tag3")),
		Wide40:   f.u64(t, "wide40"),
		Q1Unit:   f.f32(t, "q1Unit"),
		Q12Angle: f.f32(t, "q12Angle"),
		Q10Asym:  f.f32(t, "q10Asym"),
		Q8Le:     f.f32(t, "q8Le"),
		Q16Le:    f.f32(t, "q16Le"),
		Q32Le:    f.f32(t, "q32Le"),
		FxI16:    f.f64(t, "fxI16"),
		FxSvar:   f.f64(t, "fxSvar"),
		FxU64:    f.f64(t, "fxU64"),
	}
}

func toYAMLBytesBoundary(m BytesBoundary) yaml.Node {
	return yamlMap(
		"small", yamlHex(m.Small),
		"medium", yamlHex(m.Medium),
		"label", yamlStr(m.Label),
		"tail", yamlHex(m.Tail),
	)
}
func fromYAMLBytesBoundary(t *testing.T, n *yaml.Node) BytesBoundary {
	f := asFieldMap(t, n)
	return BytesBoundary{
		Small:  f.hexBytes(t, "small"),
		Medium: f.hexBytes(t, "medium"),
		Label:  f.strVal(t, "label"),
		Tail:   f.hexBytes(t, "tail"),
	}
}

func toYAMLDeltaEdge(m DeltaEdge) yaml.Node {
	return yamlMap(
		"seq", yamlUint(uint64(m.Seq)),
		"bodyMask", yamlUint(uint64(m.BodyMask)),
		"posX", yamlF32(m.PosX),
		"aim", yamlF32(m.Aim),
		"energy", yamlF64(m.Energy),
		"hash", yamlBigUint(m.Hash),
		"blob", yamlHex(m.Blob),
	)
}
func fromYAMLDeltaEdge(t *testing.T, n *yaml.Node) DeltaEdge {
	f := asFieldMap(t, n)
	return DeltaEdge{
		Seq:      uint32(f.u64(t, "seq")),
		BodyMask: uint16(f.u64(t, "bodyMask")),
		PosX:     f.f32(t, "posX"),
		Aim:      f.f32(t, "aim"),
		Energy:   f.f64(t, "energy"),
		Hash:     f.u64(t, "hash"),
		Blob:     f.hexBytes(t, "blob"),
	}
}

func toYAMLEdgeEnvelope(m EdgeEnvelope) yaml.Node {
	switch m.Tag {
	case EdgeEnvelopeTagMixed:
		return yamlMap("tag", yamlBigUint(m.Tag), "mixed", nodePtr(toYAMLMixedEndian(m.Mixed)))
	case EdgeEnvelopeTagVarint:
		return yamlMap("tag", yamlBigUint(m.Tag), "varint", nodePtr(toYAMLVarintEdge(m.Varint)))
	default:
		return yamlMap("tag", yamlBigUint(m.Tag), "quantized", nodePtr(toYAMLQuantizedPacket(m.Quantized)))
	}
}
func fromYAMLEdgeEnvelope(t *testing.T, n *yaml.Node) EdgeEnvelope {
	f := asFieldMap(t, n)
	var m EdgeEnvelope
	m.Tag = f.u64(t, "tag")
	switch m.Tag {
	case EdgeEnvelopeTagMixed:
		m.Mixed = fromYAMLMixedEndian(t, f["mixed"])
	case EdgeEnvelopeTagVarint:
		m.Varint = fromYAMLVarintEdge(t, f["varint"])
	case EdgeEnvelopeTagQuantized:
		m.Quantized = fromYAMLQuantizedPacket(t, f["quantized"])
	}
	return m
}

func vectorCodecs() map[string]vectorCodec {
	return map[string]vectorCodec{
		"TransportFrame": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLTransportFrame(t, n)
				return wire.AppendTransportFrame(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.TransportFrame
				n, err := wire.DecodeTransportFrame(raw, &m)
				return n, toYAMLTransportFrame(m), err
			},
		},
		"UDPPeerFrame": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLUDPPeerFrame(t, n)
				return wire.AppendUDPPeerFrame(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.UDPPeerFrame
				n, err := wire.DecodeUDPPeerFrame(raw, &m)
				return n, toYAMLUDPPeerFrame(m), err
			},
		},
		"UDPSealedDatagram": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLUDPSealedDatagram(t, n)
				return wire.AppendUDPSealedDatagram(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.UDPSealedDatagram
				n, err := wire.DecodeUDPSealedDatagram(raw, &m)
				return n, toYAMLUDPSealedDatagram(m), err
			},
		},
		"HandshakeMessage": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLHandshakeMessage(t, n)
				return wire.AppendHandshakeMessage(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.HandshakeMessage
				n, err := wire.DecodeHandshakeMessage(raw, &m)
				return n, toYAMLHandshakeMessage(m), err
			},
		},
		"RelayFrame": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLRelayFrame(t, n)
				return wire.AppendRelayFrame(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.RelayFrame
				n, err := wire.DecodeRelayFrame(raw, &m)
				return n, toYAMLRelayFrame(m), err
			},
		},
		"RelayMembers": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLRelayMembers(t, n)
				return wire.AppendRelayMembers(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.RelayMembers
				n, err := wire.DecodeRelayMembers(raw, &m)
				return n, toYAMLRelayMembers(m), err
			},
		},
		"RelayBody": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLRelayBody(t, n)
				return wire.AppendRelayBody(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.RelayBody
				n, err := wire.DecodeRelayBody(raw, &m)
				return n, toYAMLRelayBody(m), err
			},
		},
		"EntityRecord": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLEntityRecord(t, n)
				return wire.AppendEntityRecord(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.EntityRecord
				n, err := wire.DecodeEntityRecord(raw, &m)
				return n, toYAMLEntityRecord(m), err
			},
			equal: func(t *testing.T, want, got *yaml.Node) error {
				return eqEntityRecord(fromYAMLEntityRecord(t, want), fromYAMLEntityRecord(t, got))
			},
			diff: func(t *testing.T, baseN, curN *yaml.Node) (uint64, []byte, yaml.Node) {
				base := fromYAMLEntityRecord(t, baseN)
				cur := fromYAMLEntityRecord(t, curN)
				mask := wire.DiffEntityRecord(&base, &cur)
				delta := cur
				delta.FieldsMask = mask
				raw := wire.AppendEntityRecord(nil, &delta)
				var dec wire.EntityRecord
				if _, err := wire.DecodeEntityRecord(raw, &dec); err != nil {
					t.Fatal(err)
				}
				applied := base
				wire.ApplyEntityRecord(&applied, &dec)
				return uint64(mask), raw, toYAMLEntityRecord(applied)
			},
			apply: func(t *testing.T, baseN *yaml.Node, deltaRaw []byte) (yaml.Node, error) {
				base := fromYAMLEntityRecord(t, baseN)
				var dec wire.EntityRecord
				if _, err := wire.DecodeEntityRecord(deltaRaw, &dec); err != nil {
					return yaml.Node{}, err
				}
				wire.ApplyEntityRecord(&base, &dec)
				return toYAMLEntityRecord(base), nil
			},
		},
		"ReplicationMessage": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLReplicationMessage(t, n)
				return wire.AppendReplicationMessage(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.ReplicationMessage
				n, err := wire.DecodeReplicationMessage(raw, &m)
				return n, toYAMLReplicationMessage(m), err
			},
		},
		"InputFrame": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLInputFrame(t, n)
				return wire.AppendInputFrame(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.InputFrame
				n, err := wire.DecodeInputFrame(raw, &m)
				return n, toYAMLInputFrame(m), err
			},
		},
		"RollbackMessage": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLRollbackMessage(t, n)
				return wire.AppendRollbackMessage(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.RollbackMessage
				n, err := wire.DecodeRollbackMessage(raw, &m)
				return n, toYAMLRollbackMessage(m), err
			},
		},
		"MatchMember": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLMatchMember(t, n)
				return wire.AppendMatchMember(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.MatchMember
				n, err := wire.DecodeMatchMember(raw, &m)
				return n, toYAMLMatchMember(m), err
			},
		},
		"JoinState": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLJoinState(t, n)
				return wire.AppendJoinState(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.JoinState
				n, err := wire.DecodeJoinState(raw, &m)
				return n, toYAMLJoinState(m), err
			},
		},
		"MembershipMessage": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLMembershipMessage(t, n)
				return wire.AppendMembershipMessage(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m wire.MembershipMessage
				n, err := wire.DecodeMembershipMessage(raw, &m)
				return n, toYAMLMembershipMessage(m), err
			},
		},
		"FighterProps": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLFighterProps(t, n)
				return AppendFighterProps(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m FighterProps
				n, err := DecodeFighterProps(raw, &m)
				return n, toYAMLFighterProps(m), err
			},
			diff: func(t *testing.T, baseN, curN *yaml.Node) (uint64, []byte, yaml.Node) {
				base := fromYAMLFighterProps(t, baseN)
				cur := fromYAMLFighterProps(t, curN)
				mask := DiffFighterProps(&base, &cur)
				delta := cur
				delta.StateMask = mask
				raw := AppendFighterProps(nil, &delta)
				var dec FighterProps
				if _, err := DecodeFighterProps(raw, &dec); err != nil {
					t.Fatal(err)
				}
				applied := base
				ApplyFighterProps(&applied, &dec)
				return uint64(mask), raw, toYAMLFighterProps(applied)
			},
			apply: func(t *testing.T, baseN *yaml.Node, deltaRaw []byte) (yaml.Node, error) {
				base := fromYAMLFighterProps(t, baseN)
				var dec FighterProps
				if _, err := DecodeFighterProps(deltaRaw, &dec); err != nil {
					return yaml.Node{}, err
				}
				ApplyFighterProps(&base, &dec)
				return toYAMLFighterProps(base), nil
			},
		},
		"MixedEndian": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLMixedEndian(t, n)
				return AppendMixedEndian(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m MixedEndian
				n, err := DecodeMixedEndian(raw, &m)
				return n, toYAMLMixedEndian(m), err
			},
		},
		"VarintEdge": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLVarintEdge(t, n)
				return AppendVarintEdge(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m VarintEdge
				n, err := DecodeVarintEdge(raw, &m)
				return n, toYAMLVarintEdge(m), err
			},
		},
		"QuantizedPacket": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLQuantizedPacket(t, n)
				return AppendQuantizedPacket(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m QuantizedPacket
				n, err := DecodeQuantizedPacket(raw, &m)
				return n, toYAMLQuantizedPacket(m), err
			},
		},
		"BytesBoundary": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLBytesBoundary(t, n)
				return AppendBytesBoundary(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m BytesBoundary
				n, err := DecodeBytesBoundary(raw, &m)
				return n, toYAMLBytesBoundary(m), err
			},
		},
		"DeltaEdge": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLDeltaEdge(t, n)
				return AppendDeltaEdge(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m DeltaEdge
				n, err := DecodeDeltaEdge(raw, &m)
				return n, toYAMLDeltaEdge(m), err
			},
			diff: func(t *testing.T, baseN, curN *yaml.Node) (uint64, []byte, yaml.Node) {
				base := fromYAMLDeltaEdge(t, baseN)
				cur := fromYAMLDeltaEdge(t, curN)
				mask := DiffDeltaEdge(&base, &cur)
				delta := cur
				delta.BodyMask = mask
				raw := AppendDeltaEdge(nil, &delta)
				var dec DeltaEdge
				if _, err := DecodeDeltaEdge(raw, &dec); err != nil {
					t.Fatal(err)
				}
				applied := base
				ApplyDeltaEdge(&applied, &dec)
				return uint64(mask), raw, toYAMLDeltaEdge(applied)
			},
			apply: func(t *testing.T, baseN *yaml.Node, deltaRaw []byte) (yaml.Node, error) {
				base := fromYAMLDeltaEdge(t, baseN)
				var dec DeltaEdge
				if _, err := DecodeDeltaEdge(deltaRaw, &dec); err != nil {
					return yaml.Node{}, err
				}
				ApplyDeltaEdge(&base, &dec)
				return toYAMLDeltaEdge(base), nil
			},
		},
		"EdgeEnvelope": {
			encode: func(t *testing.T, n *yaml.Node) []byte {
				m := fromYAMLEdgeEnvelope(t, n)
				return AppendEdgeEnvelope(nil, &m)
			},
			decode: func(t *testing.T, raw []byte) (int, yaml.Node, error) {
				var m EdgeEnvelope
				n, err := DecodeEdgeEnvelope(raw, &m)
				return n, toYAMLEdgeEnvelope(m), err
			},
		},
	}
}

func makeVector(t *testing.T, codecs map[string]vectorCodec, name, typeName string, val yaml.Node) messageVector {
	t.Helper()
	c := codecs[typeName]
	raw := c.encode(t, &val)
	n, dec, err := c.decode(t, raw)
	if err != nil {
		t.Fatalf("%s (%s): decode generated vector failed: %v", name, typeName, err)
	}
	if n != len(raw) {
		t.Fatalf("%s (%s): decode consumed %d bytes, want %d", name, typeName, n, len(raw))
	}
	v := messageVector{
		Name:  name,
		Type:  typeName,
		Hex:   quotedHex(hex.EncodeToString(raw)),
		Value: val,
	}
	if !yamlNodesEqual(&val, &dec) {
		v.Decoded = dec
	}
	return v
}

func makeDeltaVector(t *testing.T, codecs map[string]vectorCodec, name, typeName string, base, cur yaml.Node) deltaVector {
	t.Helper()
	c := codecs[typeName]
	mask, deltaRaw, expected := c.diff(t, &base, &cur)
	return deltaVector{
		Name:     name,
		Type:     typeName,
		Base:     base,
		Cur:      cur,
		DiffMask: mask,
		DeltaHex: quotedHex(hex.EncodeToString(deltaRaw)),
		Expected: expected,
	}
}

func yamlNodesEqual(a, b *yaml.Node) bool {
	if a.Kind != b.Kind || a.Value != b.Value || len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		if !yamlNodesEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

func writeYAMLFile(t *testing.T, path string, doc any) {
	t.Helper()
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func generateAllGoldenVectors(t *testing.T) {
	t.Helper()
	codecs := vectorCodecs()
	vecDir := filepath.Join("testdata", "vectors")
	rng := rand.New(rand.NewPCG(0x1304_2026_1127, 0xfeed_face_cafe_beef))
	randBytes := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(rng.Uint32())
		}
		return b
	}
	negZero32 := float32(math.Copysign(0, -1))
	nan32 := float32(math.NaN())
	posInf32 := float32(math.Inf(1))
	negInf32 := float32(math.Inf(-1))

	// 1. quantize.yaml
	qfFile := quantizeVectorFile{Version: 1, Schema: "quantize"}
	qfSeeds := []struct {
		name     string
		v        float32
		min, max float32
		bits     int
	}{
		{"zero_in_sym_16", 0, -1024, 1024, 16},
		{"neg_zero_in_sym_16", negZero32, -1024, 1024, 16},
		{"min_exact_16", -1024, -1024, 1024, 16},
		{"max_exact_16", 1024, -1024, 1024, 16},
		{"below_min_clamp", -9999, -1024, 1024, 16},
		{"above_max_clamp", 9999, -1024, 1024, 16},
		{"nan_clamps_to_zero", nan32, -1024, 1024, 16},
		{"neg_inf_clamps", negInf32, -1024, 1024, 16},
		{"pos_inf_clamps", posInf32, -1024, 1024, 16},
		{"unit_1bit_low", 0.49, 0, 1, 1},
		{"unit_1bit_half_ties_up", 0.5, 0, 1, 1},
		{"unit_8bit_quarter", 0.25, 0, 1, 8},
		{"angle_12bit_pi_quarter", 0.7853982, -3.1415927, 3.1415927, 12},
		{"asymmetric_10bit", 12.345, -10, 50, 10},
		{"high_prec_24bit", 123.45679, -500, 500, 24},
		{"full_32bit_mid", 0.5, 0, 1, 32},
		{"subnormal_in_tiny_range", math.SmallestNonzeroFloat32, 0, 1e-38, 16},
		{"edge_just_above_min_12bit", math.Nextafter32(-1, 1), -1, 1, 12},
		{"edge_just_below_max_12bit", math.Nextafter32(1, -1), -1, 1, 12},
		{"halfway_3bit", 0.5 / 7.0, 0, 1, 3},
	}
	for _, s := range qfSeeds {
		q := wire.QuantizeFloat(s.v, s.min, s.max, s.bits)
		deq := wire.DequantizeFloat(q, s.min, s.max, s.bits)
		qfFile.QFloat = append(qfFile.QFloat, qfloatVector{
			Name:        s.name,
			VBits:       fmt.Sprintf("0x%08x", math.Float32bits(s.v)),
			Min:         s.min,
			Max:         s.max,
			Bits:        s.bits,
			WantQ:       q,
			WantDeqBits: fmt.Sprintf("0x%08x", math.Float32bits(deq)),
		})
	}
	bitChoices := []int{1, 3, 7, 8, 10, 12, 15, 16, 20, 24, 31, 32}
	for i := 0; i < 40; i++ {
		minV := float32(rng.IntN(2000) - 1000)
		maxV := minV + float32(rng.IntN(2000)+1)
		bits := bitChoices[i%len(bitChoices)]
		frac := rng.Float64()*1.2 - 0.1
		v := float32(float64(minV) + frac*float64(maxV-minV))
		q := wire.QuantizeFloat(v, minV, maxV, bits)
		deq := wire.DequantizeFloat(q, minV, maxV, bits)
		qfFile.QFloat = append(qfFile.QFloat, qfloatVector{
			Name:        fmt.Sprintf("fuzz_%02d", i),
			VBits:       fmt.Sprintf("0x%08x", math.Float32bits(v)),
			Min:         minV,
			Max:         maxV,
			Bits:        bits,
			WantQ:       q,
			WantDeqBits: fmt.Sprintf("0x%08x", math.Float32bits(deq)),
		})
	}
	fixedSignedSeeds := []struct {
		name   string
		v      float64
		scale  int64
		lo, hi int64
	}{
		{"zero", 0, 1000, math.MinInt32, math.MaxInt32},
		{"neg_zero", math.Copysign(0, -1), 1000, math.MinInt32, math.MaxInt32},
		{"pos_exact", 12.345, 1000, math.MinInt32, math.MaxInt32},
		{"neg_exact", -98.75, 100, math.MinInt16, math.MaxInt16},
		{"half_pos_rounds_away", 1.5, 1, math.MinInt16, math.MaxInt16},
		{"half_neg_rounds_away", -1.5, 1, math.MinInt16, math.MaxInt16},
		{"clamp_hi", 1e9, 100, math.MinInt16, math.MaxInt16},
		{"clamp_lo", -1e9, 100, math.MinInt16, math.MaxInt16},
		{"nan_to_zero", math.NaN(), 100, math.MinInt16, math.MaxInt16},
		{"pos_inf", math.Inf(1), 100, math.MinInt64, math.MaxInt64},
		{"neg_inf", math.Inf(-1), 100, math.MinInt64, math.MaxInt64},
		{"binary_scale_65536", 3.25, 65536, math.MinInt32, math.MaxInt32},
		{"large_i64_pos", 123456789.5, 1000000, math.MinInt64, math.MaxInt64},
		{"large_i64_neg", -123456789.5, 1000000, math.MinInt64, math.MaxInt64},
	}
	for _, s := range fixedSignedSeeds {
		q := wire.QuantizeFixed(s.v, s.scale, s.lo, s.hi)
		deq := wire.DequantizeFixed(q, s.scale)
		qfFile.FixedSigned = append(qfFile.FixedSigned, fixedSignedVector{
			Name:        s.name,
			VBits:       fmt.Sprintf("0x%016x", math.Float64bits(s.v)),
			Scale:       strconv.FormatInt(s.scale, 10),
			MinInt:      strconv.FormatInt(s.lo, 10),
			MaxInt:      strconv.FormatInt(s.hi, 10),
			WantQ:       strconv.FormatInt(q, 10),
			WantDeqBits: fmt.Sprintf("0x%016x", math.Float64bits(deq)),
		})
	}
	fixedUnsignedSeeds := []struct {
		name    string
		v       float64
		scale   int64
		maxUint uint64
	}{
		{"zero", 0, 100, math.MaxUint16},
		{"neg_clamps_zero", -5.0, 100, math.MaxUint16},
		{"nan_clamps_zero", math.NaN(), 100, math.MaxUint16},
		{"pos_inf_clamps_max", math.Inf(1), 100, math.MaxUint64},
		{"exact_u16", 12.34, 100, math.MaxUint16},
		{"half_tie_up", 2.5, 1, math.MaxUint16},
		{"above_2p53_u64", 1e14, 1000, math.MaxUint64},
	}
	for _, s := range fixedUnsignedSeeds {
		q := wire.QuantizeFixedUint(s.v, s.scale, s.maxUint)
		deq := wire.DequantizeFixedUint(q, s.scale)
		qfFile.FixedUnsigned = append(qfFile.FixedUnsigned, fixedUnsignedEntry{
			Name:        s.name,
			VBits:       fmt.Sprintf("0x%016x", math.Float64bits(s.v)),
			Scale:       strconv.FormatInt(s.scale, 10),
			MaxUint:     strconv.FormatUint(s.maxUint, 10),
			WantQ:       strconv.FormatUint(q, 10),
			WantDeqBits: fmt.Sprintf("0x%016x", math.Float64bits(deq)),
		})
	}
	writeYAMLFile(t, filepath.Join(vecDir, "quantize.yaml"), qfFile)

	// 2. transport.yaml
	transportVec := vectorFile{Version: 1, Schema: "transport.wire.yaml"}
	transportVec.Vectors = append(transportVec.Vectors,
		makeVector(t, codecs, "transport_empty", "TransportFrame", toYAMLTransportFrame(wire.TransportFrame{Channel: wire.ChannelReliableOrdered, Seq: 0})),
		makeVector(t, codecs, "transport_unreliable_max_seq", "TransportFrame", toYAMLTransportFrame(wire.TransportFrame{Channel: wire.ChannelUnreliable, Seq: math.MaxUint32, Payload: []byte{0xde, 0xad, 0xbe, 0xef}})),
		makeVector(t, codecs, "udp_peer_empty", "UDPPeerFrame", toYAMLUDPPeerFrame(wire.UDPPeerFrame{From: 1, Channel: wire.ChannelReliableOrdered, Seq: 42})),
		makeVector(t, codecs, "udp_peer_fuzz", "UDPPeerFrame", toYAMLUDPPeerFrame(wire.UDPPeerFrame{From: rng.Uint32(), Channel: wire.ChannelUnreliable, Seq: rng.Uint32(), Payload: randBytes(24)})),
	)
	transportVec.Invalid = []invalidVector{
		{Name: "transport_short_buffer", Type: "TransportFrame", Hex: "00000001", Error: "short_buffer"},
		{Name: "transport_invalid_channel", Type: "TransportFrame", Hex: "0200000001", Error: "invalid_enum"},
	}
	writeYAMLFile(t, filepath.Join(vecDir, "transport.yaml"), transportVec)

	// 3. udp_sealed.yaml
	udpVec := vectorFile{Version: 1, Schema: "udp_sealed.wire.yaml"}
	for i, dt := range []wire.UDPDatagramType{wire.UDPDatagramTypeData, wire.UDPDatagramTypeServerData, wire.UDPDatagramTypePing, wire.UDPDatagramTypePong, wire.UDPDatagramTypeBye, wire.UDPDatagramTypePeerData} {
		var m wire.UDPSealedDatagram
		m.Type = dt
		m.Peer = uint32(100 + i)
		m.Seq = uint32(0x10203040 + i)
		m.Channel = wire.Channel(i % 2)
		if i > 0 {
			m.Sealed = randBytes(16 + i*4)
		}
		udpVec.Vectors = append(udpVec.Vectors, makeVector(t, codecs, fmt.Sprintf("sealed_type_%d", dt), "UDPSealedDatagram", toYAMLUDPSealedDatagram(m)))
	}
	udpVec.Invalid = []invalidVector{
		{Name: "sealed_short", Type: "UDPSealedDatagram", Hex: "b100000001", Error: "short_buffer"},
		{Name: "sealed_invalid_type", Type: "UDPSealedDatagram", Hex: "ff0000000100000002000102030405060708", Error: "invalid_enum"},
		{Name: "sealed_invalid_channel", Type: "UDPSealedDatagram", Hex: "b10000000100000002090102030405060708", Error: "invalid_enum"},
	}
	writeYAMLFile(t, filepath.Join(vecDir, "udp_sealed.yaml"), udpVec)

	// 4. handshake.yaml
	hsVec := vectorFile{Version: 1, Schema: "handshake.wire.yaml"}
	var hsHelloEmpty, hsHelloMax wire.HandshakeMessage
	hsHelloEmpty.Tag = wire.HandshakeMessageTagHello
	copy(hsHelloEmpty.Hello.Nonce[:], randBytes(16))
	hsHelloMax.Tag = wire.HandshakeMessageTagHello
	copy(hsHelloMax.Hello.Nonce[:], randBytes(16))
	hsHelloMax.Hello.ResumePeer = 0xdeadbeef
	copy(hsHelloMax.Hello.ResumeSecret[:], randBytes(16))
	hsHelloMax.Hello.Token = bytes.Repeat([]byte{0xa5}, 4096)

	var hsWelcome wire.HandshakeMessage
	hsWelcome.Tag = wire.HandshakeMessageTagWelcome
	copy(hsWelcome.Welcome.Nonce[:], randBytes(16))
	hsWelcome.Welcome.Peer = 7
	hsWelcome.Welcome.Server = 1
	hsWelcome.Welcome.Resumed = true
	copy(hsWelcome.Welcome.Secret[:], randBytes(16))
	copy(hsWelcome.Welcome.Key[:], randBytes(32))

	var hsRejectEmpty, hsRejectMax wire.HandshakeMessage
	hsRejectEmpty.Tag = wire.HandshakeMessageTagReject
	copy(hsRejectEmpty.Reject.Nonce[:], randBytes(16))
	hsRejectMax.Tag = wire.HandshakeMessageTagReject
	copy(hsRejectMax.Reject.Nonce[:], randBytes(16))
	hsRejectMax.Reject.Reason = strings.Repeat("服务器已满!", 16) // 16 * 16 = 256 UTF-8 bytes

	hsVec.Vectors = append(hsVec.Vectors,
		makeVector(t, codecs, "hello_empty_token", "HandshakeMessage", toYAMLHandshakeMessage(hsHelloEmpty)),
		makeVector(t, codecs, "hello_max_4096_token", "HandshakeMessage", toYAMLHandshakeMessage(hsHelloMax)),
		makeVector(t, codecs, "welcome_resumed", "HandshakeMessage", toYAMLHandshakeMessage(hsWelcome)),
		makeVector(t, codecs, "reject_empty", "HandshakeMessage", toYAMLHandshakeMessage(hsRejectEmpty)),
		makeVector(t, codecs, "reject_max_256_utf8", "HandshakeMessage", toYAMLHandshakeMessage(hsRejectMax)),
	)
	badVer := wire.AppendHandshakeMessage(nil, &hsHelloEmpty)
	badVer[1] = 2
	badBool := wire.AppendHandshakeMessage(nil, &hsWelcome)
	badBool[1+1+16+4+4] = 2
	tooLongToken := wire.AppendHandshakeMessage(nil, &hsHelloMax)
	tooLongToken[len(tooLongToken)-4096-2] = 0x10
	tooLongToken[len(tooLongToken)-4096-1] = 0x01 // 4097 > 4096
	tooLongToken = append(tooLongToken, 0)
	hsVec.Invalid = []invalidVector{
		{Name: "wrong_version_const", Type: "HandshakeMessage", Hex: quotedHex(hex.EncodeToString(badVer)), Error: "invalid_const"},
		{Name: "invalid_bool", Type: "HandshakeMessage", Hex: quotedHex(hex.EncodeToString(badBool)), Error: "invalid_bool"},
		{Name: "token_limit_exceeded", Type: "HandshakeMessage", Hex: quotedHex(hex.EncodeToString(tooLongToken)), Error: "limit_exceeded"},
		{Name: "unknown_tag", Type: "HandshakeMessage", Hex: "a401", Error: "invalid_tag"},
	}
	writeYAMLFile(t, filepath.Join(vecDir, "handshake.yaml"), hsVec)

	// 5. relay.yaml
	relayVec := vectorFile{Version: 1, Schema: "relay.wire.yaml"}
	relayVec.Vectors = append(relayVec.Vectors,
		makeVector(t, codecs, "frame_empty", "RelayFrame", toYAMLRelayFrame(wire.RelayFrame{Peer: 0})),
		makeVector(t, codecs, "frame_payload", "RelayFrame", toYAMLRelayFrame(wire.RelayFrame{Peer: 0x01020304, Body: randBytes(19)})),
		makeVector(t, codecs, "members_empty", "RelayMembers", toYAMLRelayMembers(wire.RelayMembers{Epoch: 1})),
		makeVector(t, codecs, "members_three", "RelayMembers", toYAMLRelayMembers(wire.RelayMembers{Epoch: 99, Peers: []uint32{1, 2, 0xffffffff}})),
		makeVector(t, codecs, "body_unreliable", "RelayBody", toYAMLRelayBody(wire.RelayBody{Tag: wire.RelayBodyTagUnreliable, Unreliable: wire.RelayUnreliable{Seq: 12, Payload: []byte{1, 2, 3}}})),
		makeVector(t, codecs, "body_reliable", "RelayBody", toYAMLRelayBody(wire.RelayBody{Tag: wire.RelayBodyTagReliable, Reliable: wire.RelayReliable{Seq: 0xffffffff, Payload: nil}})),
		makeVector(t, codecs, "body_ack_above_2p53", "RelayBody", toYAMLRelayBody(wire.RelayBody{Tag: wire.RelayBodyTagAck, Ack: wire.RelayAck{Next: 77, Mask: 0xfedcba9876543211}})),
		makeVector(t, codecs, "body_ack_max_u64", "RelayBody", toYAMLRelayBody(wire.RelayBody{Tag: wire.RelayBodyTagAck, Ack: wire.RelayAck{Next: 0, Mask: math.MaxUint64}})),
	)
	relayVec.Invalid = []invalidVector{
		{Name: "frame_bad_version", Type: "RelayFrame", Hex: "0200000001", Error: "invalid_const"},
		{Name: "members_bad_kind", Type: "RelayMembers", Hex: "09000000010000", Error: "invalid_const"},
		{Name: "body_bad_tag", Type: "RelayBody", Hex: "0400000001", Error: "invalid_tag"},
	}
	writeYAMLFile(t, filepath.Join(vecDir, "relay.yaml"), relayVec)

	// 6. replication.yaml
	replVec := vectorFile{Version: 1, Schema: "replication.wire.yaml"}
	entFull := wire.EntityRecord{
		ID:         16384,
		FieldsMask: wire.EntityRecordMaskValid,
		Kind:       300,
		X:          negZero32,
		Y:          nan32,
		VX:         posInf32,
		VY:         negInf32,
		Rotation:   3.1415927,
		Flags:      0xffffffff,
		Props:      []byte{0x00, 0xff, 0x10, 0x20},
	}
	entZeroMask := wire.EntityRecord{ID: 1, FieldsMask: 0}
	replVec.Vectors = append(replVec.Vectors,
		makeVector(t, codecs, "entity_zero_mask", "EntityRecord", toYAMLEntityRecord(entZeroMask)),
		makeVector(t, codecs, "entity_all_fields_special_f32", "EntityRecord", toYAMLEntityRecord(entFull)),
		makeVector(t, codecs, "entity_empty_props_bit_set", "EntityRecord", toYAMLEntityRecord(wire.EntityRecord{ID: 127, FieldsMask: wire.EntityRecordFieldProps, Props: nil})),
		makeVector(t, codecs, "msg_full", "ReplicationMessage", toYAMLReplicationMessage(wire.ReplicationMessage{
			Tag:     wire.ReplicationMessageTagFull,
			Version: wire.ReplicationMessageVersion,
			Full: wire.SnapshotFull{
				Epoch:    0x89abcdef,
				Tick:     128,
				Entities: []wire.EntityRecord{entZeroMask, entFull},
			},
		})),
		makeVector(t, codecs, "msg_delta", "ReplicationMessage", toYAMLReplicationMessage(wire.ReplicationMessage{
			Tag:     wire.ReplicationMessageTagDelta,
			Version: wire.ReplicationMessageVersion,
			Delta: wire.SnapshotDelta{
				Epoch:        42,
				Tick:         200,
				BaselineTick: 199,
				Despawns:     []uint32{3, 16384},
				Updates:      []wire.EntityRecord{{ID: 7, FieldsMask: wire.EntityRecordFieldX | wire.EntityRecordFieldProps, X: 99.25, Props: []byte{0x42}}},
			},
		})),
		makeVector(t, codecs, "msg_ack", "ReplicationMessage", toYAMLReplicationMessage(wire.ReplicationMessage{
			Tag: wire.ReplicationMessageTagAck, Version: wire.ReplicationMessageVersion, Ack: wire.SnapshotAck{Epoch: 42, Tick: math.MaxUint32},
		})),
		makeVector(t, codecs, "msg_resync", "ReplicationMessage", toYAMLReplicationMessage(wire.ReplicationMessage{
			Tag: wire.ReplicationMessageTagResync, Version: wire.ReplicationMessageVersion, Resync: wire.ResyncRequest{Epoch: 0x01020304},
		})),
		makeVector(t, codecs, "msg_full_part", "ReplicationMessage", toYAMLReplicationMessage(wire.ReplicationMessage{
			Tag: wire.ReplicationMessageTagFullPart, Version: wire.ReplicationMessageVersion,
			FullPart: wire.SnapshotFullPart{Epoch: 7, Tick: 500, Index: 1, Total: 3, Entities: []wire.EntityRecord{{ID: 9, FieldsMask: wire.EntityRecordFieldKind, Kind: 5}}},
		})),
	)
	baseEnt := wire.EntityRecord{ID: 10, FieldsMask: wire.EntityRecordMaskValid, Kind: 1, X: 0, Y: nan32, VX: 1.5, VY: -2.5, Rotation: 0.5, Flags: 4, Props: []byte{1, 2}}
	curEntNegZero := baseEnt
	curEntNegZero.X = negZero32 // +0 -> -0 MUST trigger FieldX diff!
	curEntNegZero.Y = nan32     // NaN -> NaN MUST NOT trigger FieldY diff!
	curEntNegZero.Props = nil   // non-empty -> empty MUST trigger FieldProps diff!
	replVec.Deltas = append(replVec.Deltas,
		makeDeltaVector(t, codecs, "entity_neg_zero_and_nan_and_clear_props", "EntityRecord", toYAMLEntityRecord(baseEnt), toYAMLEntityRecord(curEntNegZero)),
		makeDeltaVector(t, codecs, "entity_identical_noop", "EntityRecord", toYAMLEntityRecord(baseEnt), toYAMLEntityRecord(baseEnt)),
		makeDeltaVector(t, codecs, "entity_all_changed", "EntityRecord", toYAMLEntityRecord(baseEnt), toYAMLEntityRecord(wire.EntityRecord{
			ID: 10, FieldsMask: wire.EntityRecordMaskValid, Kind: 2, X: 10, Y: 20, VX: 3, VY: 4, Rotation: 1, Flags: 8, Props: []byte{9, 8, 7},
		})),
	)
	replVec.Invalid = []invalidVector{
		{Name: "repl_invalid_tag", Type: "ReplicationMessage", Hex: "2601000000", Error: "invalid_tag"},
		{Name: "repl_wrong_version_v1", Type: "ReplicationMessage", Hex: "0101000000", Error: "invalid_version"},
		{Name: "repl_wrong_version_v3", Type: "ReplicationMessage", Hex: "3101000000", Error: "invalid_version"},
		{Name: "repl_short_buffer", Type: "ReplicationMessage", Hex: "210100", Error: "short_buffer"},
	}
	writeYAMLFile(t, filepath.Join(vecDir, "replication.yaml"), replVec)

	// 7. rollback.yaml
	rbVec := vectorFile{Version: 1, Schema: "rollback.wire.yaml"}
	inA := wire.InputFrame{Tick: 0, Peer: 1, Bits: (1 << 53) + 1, Axes: [2]int16{-32768, 32767}}
	inB := wire.InputFrame{Tick: math.MaxUint32, Peer: 0x80000001, Bits: math.MaxUint64, Axes: [2]int16{-1, 0}}
	rbVec.Vectors = append(rbVec.Vectors,
		makeVector(t, codecs, "input_above_2p53", "InputFrame", toYAMLInputFrame(inA)),
		makeVector(t, codecs, "input_max_u64", "InputFrame", toYAMLInputFrame(inB)),
		makeVector(t, codecs, "single_input", "RollbackMessage", toYAMLRollbackMessage(wire.RollbackMessage{Tag: wire.RollbackMessageTagSingleInput, SingleInput: wire.SingleInput{Input: inA}})),
		makeVector(t, codecs, "bundle_empty", "RollbackMessage", toYAMLRollbackMessage(wire.RollbackMessage{Tag: wire.RollbackMessageTagInputBundle})),
		makeVector(t, codecs, "bundle_two", "RollbackMessage", toYAMLRollbackMessage(wire.RollbackMessage{Tag: wire.RollbackMessageTagInputBundle, InputBundle: wire.InputBundle{Inputs: []wire.InputFrame{inA, inB}}})),
		makeVector(t, codecs, "checksum_above_2p53", "RollbackMessage", toYAMLRollbackMessage(wire.RollbackMessage{Tag: wire.RollbackMessageTagChecksum, Checksum: wire.ChecksumMsg{Tick: 120, Peer: 2, Checksum: 0x0123456789abcdef}})),
	)
	rbVec.Invalid = []invalidVector{
		{Name: "rollback_bad_tag", Type: "RollbackMessage", Hex: "0900000000", Error: "invalid_tag"},
		{Name: "rollback_short_input", Type: "InputFrame", Hex: "0000000100000002", Error: "short_buffer"},
	}
	writeYAMLFile(t, filepath.Join(vecDir, "rollback.yaml"), rbVec)

	// 8. membership.yaml
	memVec := vectorFile{Version: 1, Schema: "membership.wire.yaml"}
	memVec.Vectors = append(memVec.Vectors,
		makeVector(t, codecs, "match_member_active", "MatchMember", toYAMLMatchMember(wire.MatchMember{ID: 1, From: 0, HasLeave: false, Until: 0})),
		makeVector(t, codecs, "match_member_left", "MatchMember", toYAMLMatchMember(wire.MatchMember{ID: 2, From: 10, HasLeave: true, Until: 500})),
		makeVector(t, codecs, "join_state_full", "JoinState", toYAMLJoinState(wire.JoinState{
			Tick:    300,
			Members: []wire.MatchMember{{ID: 1, From: 0}, {ID: 2, From: 50, HasLeave: true, Until: 290}},
			Inputs:  []wire.InputFrame{inA, inB},
			State:   []byte{0xca, 0xfe, 0xba, 0xbe},
		})),
		makeVector(t, codecs, "msg_change_leave", "MembershipMessage", toYAMLMembershipMessage(wire.MembershipMessage{
			Tag: wire.MembershipMessageTagChange,
			Change: wire.MemberChange{
				Kind:   wire.MemberChangeKindLeave,
				Peer:   2,
				Tick:   400,
				Inputs: []wire.InputFrame{inA},
			},
		})),
		makeVector(t, codecs, "msg_join_state_part", "MembershipMessage", toYAMLMembershipMessage(wire.MembershipMessage{
			Tag:           wire.MembershipMessageTagJoinStatePart,
			JoinStatePart: wire.JoinStatePart{Part: 0, Parts: 2, Data: randBytes(16)},
		})),
	)
	memVec.Invalid = []invalidVector{
		{Name: "member_bad_bool", Type: "MatchMember", Hex: "00000001000000020200000003", Error: "invalid_bool"},
		{Name: "change_bad_kind", Type: "MembershipMessage", Hex: "0d0300000001000000020000", Error: "invalid_enum"},
		{Name: "membership_bad_tag", Type: "MembershipMessage", Hex: "0f00", Error: "invalid_tag"},
	}
	writeYAMLFile(t, filepath.Join(vecDir, "membership.yaml"), memVec)

	// 9. custom_props.yaml
	cpVec := vectorFile{Version: 1, Schema: "custom_props.wire.yaml"}
	fpBase := FighterProps{
		Team: 3, Combo: 15, AimX: -1, AimY: 1, Stance: StanceRunning, HpDelta: -42, Stamina: 87.5,
		StateMask: FighterPropsMaskValid, Shield: 50, Tag: "hero-α", Emote: 7,
	}
	fpClamp := FighterProps{
		Team: 1, Combo: 0, AimX: -999, AimY: nan32, Stance: StanceJumping, HpDelta: math.MinInt32, Stamina: 9999,
		StateMask: FighterPropsFieldShield, Shield: 125.5,
	}
	cpVec.Vectors = append(cpVec.Vectors,
		makeVector(t, codecs, "fighter_full", "FighterProps", toYAMLFighterProps(fpBase)),
		makeVector(t, codecs, "fighter_clamp_and_min_svarint", "FighterProps", toYAMLFighterProps(fpClamp)),
		makeVector(t, codecs, "fighter_zero_mask", "FighterProps", toYAMLFighterProps(FighterProps{AimX: 0, AimY: negZero32, Stance: StanceIdle, HpDelta: 0, Stamina: 0})),
	)
	fpCur := fpBase
	fpCur.Shield = 12.5
	fpCur.Tag = "冠军🏆"
	cpVec.Deltas = append(cpVec.Deltas,
		makeDeltaVector(t, codecs, "fighter_shield_and_utf8_tag", "FighterProps", toYAMLFighterProps(fpBase), toYAMLFighterProps(fpCur)),
	)
	// Invalid mask bit (bit 3 set, valid mask is 0x07)
	badMask := AppendFighterProps(nil, &FighterProps{Stance: StanceIdle})
	badMask[len(badMask)-1] = 0x08
	// Non-zero bit padding on masked 10-bit shield (10 bits -> 2 bytes, low 6 bits must be 0)
	badPad := AppendFighterProps(nil, &FighterProps{Stance: StanceIdle, StateMask: FighterPropsFieldShield, Shield: 50})
	badPad[len(badPad)-1] |= 0x01
	cpVec.Invalid = []invalidVector{
		{Name: "fighter_invalid_mask", Type: "FighterProps", Hex: quotedHex(hex.EncodeToString(badMask)), Error: "invalid_mask"},
		{Name: "fighter_non_zero_padding", Type: "FighterProps", Hex: quotedHex(hex.EncodeToString(badPad)), Error: "non_zero_padding"},
	}
	writeYAMLFile(t, filepath.Join(vecDir, "custom_props.yaml"), cpVec)

	// 10. edge_cases.yaml
	edgeVec := vectorFile{Version: 1, Schema: "edge_cases.wire.yaml"}
	edgeVec.Vectors = append(edgeVec.Vectors,
		makeVector(t, codecs, "mixed_endian_boundaries", "MixedEndian", toYAMLMixedEndian(MixedEndian{
			BeU16: 0x1234, LeU16: 0x1234,
			BeU32: 0x01020304, LeU32: 0x01020304,
			BeU64: (1 << 53) + 1, LeU64: math.MaxUint64,
			BeI64: math.MinInt64, LeI64: math.MaxInt64,
			BeF32: negZero32, LeF32: nan32,
			Mode: BigModeHighBit,
		})),
		makeVector(t, codecs, "mixed_endian_inf_subnormal_max_enum", "MixedEndian", toYAMLMixedEndian(MixedEndian{
			BeU16: 0xffff, LeU16: 1,
			BeU32: 0x80000000, LeU32: 0x7fffffff,
			BeU64: 0x0123456789abcdef, LeU64: 0xfedcba9876543210,
			BeI64: -(1 << 53) - 1, LeI64: -1,
			BeF32: posInf32, LeF32: math.SmallestNonzeroFloat32,
			Mode: BigModeMaxTag,
		})),
		makeVector(t, codecs, "varint_zeros", "VarintEdge", toYAMLVarintEdge(VarintEdge{})),
		makeVector(t, codecs, "varint_1byte_2byte_edge", "VarintEdge", toYAMLVarintEdge(VarintEdge{
			U8V: 127, U16V: 128, U32V: 16383, U64V: 16384,
			S8V: -1, S16V: -64, S32V: -65, S64V: 64,
		})),
		makeVector(t, codecs, "varint_mid_boundaries", "VarintEdge", toYAMLVarintEdge(VarintEdge{
			U8V: 128, U16V: 16384, U32V: (1 << 28) - 1, U64V: (1 << 53) + 1,
			S8V: -128, S16V: -32768, S32V: math.MinInt32, S64V: -(1 << 53) - 1,
		})),
		makeVector(t, codecs, "varint_max_and_min_int64", "VarintEdge", toYAMLVarintEdge(VarintEdge{
			U8V: math.MaxUint8, U16V: math.MaxUint16, U32V: math.MaxUint32, U64V: math.MaxUint64,
			S8V: math.MaxInt8, S16V: math.MaxInt16, S32V: math.MaxInt32, S64V: math.MinInt64,
		})),
		makeVector(t, codecs, "varint_max_int64_and_high_u64", "VarintEdge", toYAMLVarintEdge(VarintEdge{
			U8V: 1, U16V: 255, U32V: 1 << 28, U64V: 1 << 63,
			S8V: -2, S16V: 2, S32V: -1000000, S64V: math.MaxInt64,
		})),
		makeVector(t, codecs, "quantized_edges_min_and_nan", "QuantizedPacket", toYAMLQuantizedPacket(QuantizedPacket{
			Flag1: 0, Tag3: 0, Wide40: 0,
			Q1Unit: 0, Q12Angle: -3.1415927, Q10Asym: -999,
			Q8Le: nan32, Q16Le: -1024, Q32Le: 0,
			FxI16: -9999, FxSvar: -1.5, FxU64: -10,
		})),
		makeVector(t, codecs, "quantized_edges_max_and_ties", "QuantizedPacket", toYAMLQuantizedPacket(QuantizedPacket{
			Flag1: 1, Tag3: 7, Wide40: (1 << 40) - 1,
			Q1Unit: 0.5, Q12Angle: 3.1415927, Q10Asym: 999,
			Q8Le: 1, Q16Le: 1024, Q32Le: 1,
			FxI16: 9999, FxSvar: 1.5, FxU64: 123456.75,
		})),
		makeVector(t, codecs, "quantized_interior_and_neg_zero", "QuantizedPacket", toYAMLQuantizedPacket(QuantizedPacket{
			Flag1: 1, Tag3: 5, Wide40: 0x000000abcde12345 & ((1 << 40) - 1),
			Q1Unit: 0.49, Q12Angle: 0.7853982, Q10Asym: 12.345,
			Q8Le: 0.25, Q16Le: negZero32, Q32Le: 0.5,
			FxI16: -98.75, FxSvar: 12.345, FxU64: 3.25,
		})),
		makeVector(t, codecs, "bytes_all_empty", "BytesBoundary", toYAMLBytesBoundary(BytesBoundary{})),
		makeVector(t, codecs, "bytes_max_lengths_and_utf8", "BytesBoundary", toYAMLBytesBoundary(BytesBoundary{
			Small:  bytes.Repeat([]byte{0x5a}, 255),
			Medium: bytes.Repeat([]byte{0x3c}, 65535),
			Label:  "hello 世界 🎮 " + strings.Repeat("a", 230),
			Tail:   []byte{0x01, 0x02, 0x03, 0xfe, 0xff},
		})),
		makeVector(t, codecs, "envelope_mixed", "EdgeEnvelope", toYAMLEdgeEnvelope(EdgeEnvelope{
			Tag: EdgeEnvelopeTagMixed,
			Mixed: MixedEndian{
				BeU16: 1, LeU16: 2, BeU32: 3, LeU32: 4,
				BeU64: 1 << 54, LeU64: (1 << 63) + 7,
				BeI64: -9007199254740993, LeI64: 9007199254740993,
				BeF32: -12.5, LeF32: 42.25, Mode: BigModeNone,
			},
		})),
		makeVector(t, codecs, "envelope_varint_tag_300", "EdgeEnvelope", toYAMLEdgeEnvelope(EdgeEnvelope{
			Tag:    EdgeEnvelopeTagVarint,
			Varint: VarintEdge{U8V: 200, U16V: 40000, U32V: 3000000000, U64V: 0x0102030405060708, S8V: -42, S16V: 1234, S32V: -987654, S64V: -1234567890123456},
		})),
		makeVector(t, codecs, "envelope_quantized_tag_above_2p53", "EdgeEnvelope", toYAMLEdgeEnvelope(EdgeEnvelope{
			Tag: EdgeEnvelopeTagQuantized,
			Quantized: QuantizedPacket{
				Flag1: 1, Tag3: 2, Wide40: 0x123456789a,
				Q1Unit: 1, Q12Angle: -1.25, Q10Asym: 25.5,
				Q8Le: 0.75, Q16Le: 512.25, Q32Le: 0.125,
				FxI16: 12.34, FxSvar: -456.789, FxU64: 42.5,
			},
		})),
	)
	// Add deterministic fuzzed edge messages.
	for i := 0; i < 6; i++ {
		edgeVec.Vectors = append(edgeVec.Vectors,
			makeVector(t, codecs, fmt.Sprintf("fuzz_varint_%d", i), "VarintEdge", toYAMLVarintEdge(VarintEdge{
				U8V: uint8(rng.Uint32()), U16V: uint16(rng.Uint32()), U32V: rng.Uint32(), U64V: rng.Uint64(),
				S8V: int8(rng.Int32()), S16V: int16(rng.Int32()), S32V: rng.Int32(), S64V: rng.Int64(),
			})),
		)
	}

	deBase := DeltaEdge{Seq: 1, BodyMask: DeltaEdgeMaskValid, PosX: 0, Aim: 0, Energy: 10.5, Hash: (1 << 53) + 1, Blob: []byte{1, 2, 3}}
	deCur := deBase
	deCur.PosX = negZero32
	deCur.Aim = 0.5
	deCur.Hash = math.MaxUint64
	deCur.Blob = []byte{9}
	edgeVec.Deltas = append(edgeVec.Deltas,
		makeDeltaVector(t, codecs, "delta_edge_u16_le_mask", "DeltaEdge", toYAMLDeltaEdge(deBase), toYAMLDeltaEdge(deCur)),
		makeDeltaVector(t, codecs, "delta_edge_clear_blob", "DeltaEdge", toYAMLDeltaEdge(deCur), toYAMLDeltaEdge(DeltaEdge{
			Seq: 2, BodyMask: DeltaEdgeMaskValid, PosX: negZero32, Aim: 0.5, Energy: 10.5, Hash: math.MaxUint64, Blob: nil,
		})),
	)

	// Invalid vectors for edge cases
	badPadQP := AppendQuantizedPacket(nil, &QuantizedPacket{Flag1: 1, Tag3: 3, Wide40: 1, Q1Unit: 1, Q12Angle: 0, Q10Asym: 0})
	// Bit run is 1+3+40+1+12+10 = 67 bits = 9 bytes (5 trailing zero padding bits in byte 8).
	badPadQP[8] |= 0x01
	edgeVec.Invalid = []invalidVector{
		{Name: "uvarint_overflow_10th_byte_gt_1", Type: "VarintEdge", Hex: "000000ffffffffffffffffff0200000000", Error: "overflow"},
		{Name: "uvarint_overflow_11_bytes", Type: "VarintEdge", Hex: "000000ffffffffffffffffffff00", Error: "overflow"},
		{Name: "uvarint_u8_range_overflow", Type: "VarintEdge", Hex: "800200000000000000", Error: "overflow"},
		{Name: "svarint_s8_range_overflow", Type: "VarintEdge", Hex: "000000008002000000", Error: "overflow"},
		{Name: "quantized_non_zero_bit_padding", Type: "QuantizedPacket", Hex: quotedHex(hex.EncodeToString(badPadQP)), Error: "non_zero_padding"},
		{Name: "delta_edge_invalid_mask", Type: "DeltaEdge", Hex: "012000", Error: "invalid_mask"},
		{Name: "bytes_label_limit_exceeded", Type: "BytesBoundary", Hex: quotedHex("0000008102" + strings.Repeat("61", 257)), Error: "limit_exceeded"},
		{Name: "envelope_unknown_uvarint_tag", Type: "EdgeEnvelope", Hex: "05", Error: "invalid_tag"},
	}
	writeYAMLFile(t, filepath.Join(vecDir, "edge_cases.yaml"), edgeVec)
}

func TestGoldenVectors(t *testing.T) {
	if *updateVectors {
		generateAllGoldenVectors(t)
	}

	// Verify quantize.yaml
	t.Run("quantize.yaml", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("testdata", "vectors", "quantize.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var qf quantizeVectorFile
		if err := yaml.Unmarshal(data, &qf); err != nil {
			t.Fatal(err)
		}
		if qf.Version != 1 {
			t.Fatalf("quantize.yaml version = %d, want 1", qf.Version)
		}
		for _, tc := range qf.QFloat {
			vBits, err := strconv.ParseUint(strings.TrimPrefix(tc.VBits, "0x"), 16, 32)
			if err != nil {
				t.Fatalf("%s vBits: %v", tc.Name, err)
			}
			wantDeqBits, err := strconv.ParseUint(strings.TrimPrefix(tc.WantDeqBits, "0x"), 16, 32)
			if err != nil {
				t.Fatalf("%s wantDeqBits: %v", tc.Name, err)
			}
			gotQ := wire.QuantizeFloat(math.Float32frombits(uint32(vBits)), tc.Min, tc.Max, tc.Bits)
			if gotQ != tc.WantQ {
				t.Errorf("qfloat %s: QuantizeFloat = %d, want %d", tc.Name, gotQ, tc.WantQ)
			}
			gotDeq := wire.DequantizeFloat(gotQ, tc.Min, tc.Max, tc.Bits)
			if math.Float32bits(gotDeq) != uint32(wantDeqBits) {
				t.Errorf("qfloat %s: DequantizeFloat bits = 0x%08x, want 0x%08x", tc.Name, math.Float32bits(gotDeq), wantDeqBits)
			}
		}
		for _, tc := range qf.FixedSigned {
			vBits, err := strconv.ParseUint(strings.TrimPrefix(tc.VBits, "0x"), 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			scale, _ := strconv.ParseInt(tc.Scale, 10, 64)
			lo, _ := strconv.ParseInt(tc.MinInt, 10, 64)
			hi, _ := strconv.ParseInt(tc.MaxInt, 10, 64)
			wantQ, _ := strconv.ParseInt(tc.WantQ, 10, 64)
			wantDeq, _ := strconv.ParseUint(strings.TrimPrefix(tc.WantDeqBits, "0x"), 16, 64)
			gotQ := wire.QuantizeFixed(math.Float64frombits(vBits), scale, lo, hi)
			if gotQ != wantQ {
				t.Errorf("fixedSigned %s: QuantizeFixed = %d, want %d", tc.Name, gotQ, wantQ)
			}
			gotDeq := wire.DequantizeFixed(gotQ, scale)
			if math.Float64bits(gotDeq) != wantDeq {
				t.Errorf("fixedSigned %s: DequantizeFixed bits = 0x%016x, want 0x%016x", tc.Name, math.Float64bits(gotDeq), wantDeq)
			}
		}
		for _, tc := range qf.FixedUnsigned {
			vBits, err := strconv.ParseUint(strings.TrimPrefix(tc.VBits, "0x"), 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			scale, _ := strconv.ParseInt(tc.Scale, 10, 64)
			maxU, _ := strconv.ParseUint(tc.MaxUint, 10, 64)
			wantQ, _ := strconv.ParseUint(tc.WantQ, 10, 64)
			wantDeq, _ := strconv.ParseUint(strings.TrimPrefix(tc.WantDeqBits, "0x"), 16, 64)
			gotQ := wire.QuantizeFixedUint(math.Float64frombits(vBits), scale, maxU)
			if gotQ != wantQ {
				t.Errorf("fixedUnsigned %s: QuantizeFixedUint = %d, want %d", tc.Name, gotQ, wantQ)
			}
			gotDeq := wire.DequantizeFixedUint(gotQ, scale)
			if math.Float64bits(gotDeq) != wantDeq {
				t.Errorf("fixedUnsigned %s: DequantizeFixedUint bits = 0x%016x, want 0x%016x", tc.Name, math.Float64bits(gotDeq), wantDeq)
			}
		}
	})

	codecs := vectorCodecs()
	matches, err := filepath.Glob(filepath.Join("testdata", "vectors", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) < 10 {
		t.Fatalf("expected at least 10 vector files in testdata/vectors, found %d", len(matches))
	}

	for _, path := range matches {
		base := filepath.Base(path)
		if base == "quantize.yaml" {
			continue
		}
		t.Run(base, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var vf vectorFile
			if err := yaml.Unmarshal(data, &vf); err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			if vf.Version != 1 {
				t.Fatalf("%s: version = %d, want 1", base, vf.Version)
			}
			for _, v := range vf.Vectors {
				c, ok := codecs[v.Type]
				if !ok {
					t.Fatalf("no Go codec registered for %q", v.Type)
				}
				wantBytes, err := hex.DecodeString(string(v.Hex))
				if err != nil {
					t.Fatalf("%s: bad hex: %v", v.Name, err)
				}
				gotBytes := c.encode(t, &v.Value)
				if !bytes.Equal(gotBytes, wantBytes) {
					t.Errorf("%s (%s) encode mismatch:\nwant %x\ngot  %x", v.Name, v.Type, wantBytes, gotBytes)
				}
				n, decNode, err := c.decode(t, wantBytes)
				if err != nil {
					t.Errorf("%s (%s) decode failed: %v", v.Name, v.Type, err)
					continue
				}
				if n != len(wantBytes) {
					t.Errorf("%s (%s) decode consumed %d bytes, want %d", v.Name, v.Type, n, len(wantBytes))
				}
				wantDecoded := &v.Value
				if v.Decoded.Kind != 0 {
					wantDecoded = &v.Decoded
				}
				if c.equal != nil {
					if err := c.equal(t, wantDecoded, &decNode); err != nil {
						t.Errorf("%s (%s): %v", v.Name, v.Type, err)
					}
				} else if !yamlNodesEqual(wantDecoded, &decNode) {
					wb, _ := yaml.Marshal(wantDecoded)
					gb, _ := yaml.Marshal(&decNode)
					t.Errorf("%s (%s) decoded mismatch:\nwant:\n%sgot:\n%s", v.Name, v.Type, wb, gb)
				}
				// Re-encoding decoded value must reproduce exact same bytes.
				reencoded := c.encode(t, &decNode)
				if !bytes.Equal(reencoded, wantBytes) {
					t.Errorf("%s (%s) re-encode mismatch:\nwant %x\ngot  %x", v.Name, v.Type, wantBytes, reencoded)
				}
			}
			for _, d := range vf.Deltas {
				c, ok := codecs[d.Type]
				if !ok || c.diff == nil || c.apply == nil {
					t.Fatalf("no delta codec registered for %q", d.Type)
				}
				gotMask, gotDeltaBytes, gotApplied := c.diff(t, &d.Base, &d.Cur)
				if gotMask != d.DiffMask {
					t.Errorf("delta %s: DiffMask = 0x%x, want 0x%x", d.Name, gotMask, d.DiffMask)
				}
				wantDeltaBytes, err := hex.DecodeString(string(d.DeltaHex))
				if err != nil {
					t.Fatalf("delta %s: bad hex: %v", d.Name, err)
				}
				if !bytes.Equal(gotDeltaBytes, wantDeltaBytes) {
					t.Errorf("delta %s: delta bytes mismatch:\nwant %x\ngot  %x", d.Name, wantDeltaBytes, gotDeltaBytes)
				}
				if !yamlNodesEqual(&d.Expected, &gotApplied) {
					wb, _ := yaml.Marshal(&d.Expected)
					gb, _ := yaml.Marshal(&gotApplied)
					t.Errorf("delta %s: applied mismatch:\nwant:\n%sgot:\n%s", d.Name, wb, gb)
				}
				appliedFromHex, err := c.apply(t, &d.Base, wantDeltaBytes)
				if err != nil {
					t.Errorf("delta %s: apply from hex: %v", d.Name, err)
				} else if !yamlNodesEqual(&d.Expected, &appliedFromHex) {
					t.Errorf("delta %s: apply from hex mismatch", d.Name)
				}
			}
			for _, inv := range vf.Invalid {
				c, ok := codecs[inv.Type]
				if !ok {
					t.Fatalf("no Go codec registered for %q", inv.Type)
				}
				raw, err := hex.DecodeString(string(inv.Hex))
				if err != nil {
					t.Fatalf("invalid %s: bad hex: %v", inv.Name, err)
				}
				_, _, err = c.decode(t, raw)
				if err == nil {
					t.Errorf("invalid %s (%s): expected error %q, got nil", inv.Name, inv.Type, inv.Error)
					continue
				}
				if gotCode := errorCodeFor(err); gotCode != inv.Error {
					t.Errorf("invalid %s (%s): error = %q (%v), want %q", inv.Name, inv.Type, gotCode, err, inv.Error)
				}
			}
		})
	}
}

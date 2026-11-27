package netplay_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
	"go.yaml.in/yaml/v3"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite golden YAML files in testdata/")

type stepClock struct {
	now time.Time
}

func newStepClock() *stepClock {
	return &stepClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *stepClock) Now() time.Time { return c.now }

func (c *stepClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func TestCaptureRecordRoundTripAndValidation(t *testing.T) {
	rec := netplay.CaptureRecord{
		T:       123456,
		Dir:     netplay.CaptureSend,
		Peer:    2,
		Ch:      netplay.ChannelReliableOrdered,
		Seq:     7,
		Payload: []byte{0x21, 0x01, 0x00, 0x00, 0x00},
	}
	line, err := netplay.EncodeCaptureRecord(rec)
	if err != nil {
		t.Fatalf("EncodeCaptureRecord: %v", err)
	}
	got, err := netplay.ParseCaptureRecord(line)
	if err != nil {
		t.Fatalf("ParseCaptureRecord: %v", err)
	}
	if got.Version != netplay.CaptureVersion || got.T != rec.T || got.Dir != rec.Dir || got.Peer != rec.Peer || got.Ch != rec.Ch || got.Seq != rec.Seq || !bytes.Equal(got.Payload, rec.Payload) {
		t.Fatalf("round-trip mismatch:\ngot  %+v\nwant %+v", got, rec)
	}

	// Empty payload encodes as "" (not null) and round-trips cleanly.
	emptyRec := netplay.CaptureRecord{
		Version: netplay.CaptureVersion,
		T:       0,
		Dir:     netplay.CaptureRecv,
		Peer:    1,
		Ch:      netplay.ChannelUnreliable,
		Seq:     1,
	}
	emptyLine, err := netplay.EncodeCaptureRecord(emptyRec)
	if err != nil {
		t.Fatalf("EncodeCaptureRecord(empty): %v", err)
	}
	if !strings.Contains(string(emptyLine), `"payload":""`) {
		t.Fatalf("empty payload line = %s, want empty base64 string", emptyLine)
	}
	if parsed, err := netplay.ParseCaptureRecord(emptyLine); err != nil || len(parsed.Payload) != 0 {
		t.Fatalf("ParseCaptureRecord(empty): got %+v err=%v", parsed, err)
	}

	invalid := map[string]string{
		"empty":           "",
		"missing_version": `{"t":0,"dir":"send","peer":1,"ch":0,"seq":1,"payload":""}`,
		"zero_version":    `{"version":0,"t":0,"dir":"send","peer":1,"ch":0,"seq":1,"payload":""}`,
		"future_version":  `{"version":2,"t":0,"dir":"send","peer":1,"ch":0,"seq":1,"payload":""}`,
		"unknown_field":   `{"version":1,"t":0,"dir":"send","peer":1,"ch":0,"seq":1,"payload":"","extra":true}`,
		"trailing_data":   `{"version":1,"t":0,"dir":"send","peer":1,"ch":0,"seq":1,"payload":""} {}`,
		"bad_dir":         `{"version":1,"t":0,"dir":"drop","peer":1,"ch":0,"seq":1,"payload":""}`,
		"bad_channel":     `{"version":1,"t":0,"dir":"send","peer":1,"ch":9,"seq":1,"payload":""}`,
		"negative_t":      `{"version":1,"t":-1,"dir":"send","peer":1,"ch":0,"seq":1,"payload":""}`,
		"null_payload":    `{"version":1,"t":0,"dir":"send","peer":1,"ch":0,"seq":1,"payload":null}`,
		"missing_payload": `{"version":1,"t":0,"dir":"send","peer":1,"ch":0,"seq":1}`,
	}
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := netplay.ParseCaptureRecord([]byte(raw)); err == nil {
				t.Fatalf("ParseCaptureRecord accepted invalid case %s: %s", name, raw)
			}
		})
	}
}

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCaptureTransportFilteringSamplingAndInboxForwarding(t *testing.T) {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Seed: 1})
	p1 := hub.AddPeer(1)
	p2 := hub.AddPeer(2)
	p3 := hub.AddPeer(3)

	clk := newStepClock()
	var buf bytes.Buffer
	cap1 := netplay.NewCaptureTransport(p1, &buf, netplay.CaptureOptions{
		Channels:    []netplay.Channel{netplay.ChannelUnreliable},
		Peers:       []netplay.PeerID{2},
		SampleEvery: 2,
		Now:         clk.Now,
	})

	// InboxReporter forwarding check.
	ir, ok := cap1.(netplay.InboxReporter)
	if !ok {
		t.Fatal("CaptureTransport does not implement InboxReporter")
	}
	ir.SetInboxLimits(netplay.InboxLimits{Unreliable: 1})
	_ = p2.Send(1, netplay.ChannelUnreliable, []byte{1})
	_ = p2.Send(1, netplay.ChannelUnreliable, []byte{2})
	hub.Advance(time.Millisecond)
	if st := ir.InboxStats(); st.UnreliableDropped != 1 || st.Unreliable != 1 {
		t.Fatalf("InboxStats = %+v, want dropped=1 unreliable=1", st)
	}
	// Drain queued packet (matches channel+peer, sample #0 -> recorded).
	if _, ok := cap1.Recv(); !ok {
		t.Fatal("expected queued packet")
	}

	// Filtered out by channel.
	_ = cap1.Send(2, netplay.ChannelReliableOrdered, []byte{10})
	// Filtered out by peer.
	_ = cap1.Send(3, netplay.ChannelUnreliable, []byte{11})
	// Matches (sample #1 -> skipped because SampleEvery=2).
	clk.Advance(10 * time.Millisecond)
	_ = cap1.Send(2, netplay.ChannelUnreliable, []byte{12})
	// Matches (sample #2 -> recorded).
	clk.Advance(10 * time.Millisecond)
	_ = cap1.Send(2, netplay.ChannelUnreliable, []byte{13})

	recs, err := netplay.ReadCapture(&buf)
	if err != nil {
		t.Fatalf("ReadCapture: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d captured records, want 2: %+v", len(recs), recs)
	}
	if recs[0].Dir != netplay.CaptureRecv || recs[0].Peer != 2 || !bytes.Equal(recs[0].Payload, []byte{2}) {
		t.Fatalf("recs[0] = %+v, want recv from peer 2 payload [2]", recs[0])
	}
	if recs[1].Dir != netplay.CaptureSend || recs[1].Peer != 2 || recs[1].T != int64(20*time.Millisecond) || !bytes.Equal(recs[1].Payload, []byte{13}) {
		t.Fatalf("recs[1] = %+v, want send to peer 2 t=20ms payload [13]", recs[1])
	}

	// Probabilistic sampling is deterministic for a fixed seed.
	var sbuf1, sbuf2 bytes.Buffer
	tA := netplay.NewCaptureTransport(p2, &sbuf1, netplay.CaptureOptions{SampleRate: 0.5, SampleSeed: 99, Now: clk.Now})
	tB := netplay.NewCaptureTransport(p3, &sbuf2, netplay.CaptureOptions{SampleRate: 0.5, SampleSeed: 99, Now: clk.Now})
	for i := range 20 {
		_ = tA.Send(1, netplay.ChannelUnreliable, []byte{byte(i)})
		_ = tB.Send(1, netplay.ChannelUnreliable, []byte{byte(i)})
	}
	rA, err := netplay.ReadCapture(&sbuf1)
	if err != nil {
		t.Fatal(err)
	}
	rB, err := netplay.ReadCapture(&sbuf2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rA) == 0 || len(rA) == 20 || len(rA) != len(rB) {
		t.Fatalf("unexpected probabilistic sample counts: len(rA)=%d len(rB)=%d", len(rA), len(rB))
	}

	// Write errors notify OnError without failing Send.
	var gotErr error
	wantErr := errors.New("disk full")
	tErr := netplay.NewCaptureTransport(p2, failWriter{err: wantErr}, netplay.CaptureOptions{
		OnError: func(e error) { gotErr = e },
	})
	if err := tErr.Send(1, netplay.ChannelUnreliable, []byte{1}); err != nil {
		t.Fatalf("Send failed on capture write error: %v", err)
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("OnError got %v, want %v", gotErr, wantErr)
	}
}

func assertGoldenDecodedCapture(t *testing.T, goldenName string, captureData []byte, opts wire.InspectOptions) []wire.DecodedFrame {
	t.Helper()
	records, err := netplay.ReadCapture(bytes.NewReader(captureData))
	if err != nil {
		t.Fatalf("ReadCapture: %v", err)
	}
	if len(records) == 0 {
		t.Fatalf("%s: capture has 0 records", goldenName)
	}
	ins, err := wire.NewInspector(nil, opts)
	if err != nil {
		t.Fatalf("NewInspector: %v", err)
	}
	frames := make([]wire.DecodedFrame, 0, len(records))
	for i, rec := range records {
		fr := ins.InspectCaptureRecord(i, rec.T, string(rec.Dir), uint32(rec.Peer), uint8(rec.Ch), rec.Seq, rec.Payload)
		// Acceptance criterion: re-encoding every decoded frame must reproduce the captured payload byte for byte.
		reencoded, err := ins.ReencodePayload(fr)
		if err != nil {
			t.Fatalf("%s frame %d: ReencodePayload: %v", goldenName, i, err)
		}
		if !bytes.Equal(reencoded, rec.Payload) {
			t.Fatalf("%s frame %d (%s): reencoded payload mismatch:\ngot  %x\nwant %x",
				goldenName, i, wire.FrameMessageType(fr), reencoded, rec.Payload)
		}
		frames = append(frames, fr)
	}

	var yamlBuf bytes.Buffer
	enc := yaml.NewEncoder(&yamlBuf)
	enc.SetIndent(2)
	if err := enc.Encode(frames); err != nil {
		t.Fatalf("yaml.Encode: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("yaml.Close: %v", err)
	}

	goldenPath := filepath.Join("testdata", goldenName)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, yamlBuf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return frames
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update-golden to create)", goldenPath, err)
	}
	if !bytes.Equal(yamlBuf.Bytes(), want) {
		t.Fatalf("decoded YAML does not match golden %s:\n--- got ---\n%s\n--- want ---\n%s", goldenPath, yamlBuf.String(), string(want))
	}
	return frames
}

func TestGoldenCaptureReplication(t *testing.T) {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Latency: 16 * time.Millisecond, Seed: 7})
	clk := newStepClock()
	var capBuf bytes.Buffer

	srvRaw := hub.AddPeer(1)
	cliRaw := hub.AddPeer(2)
	srvT := netplay.NewCaptureTransport(srvRaw, &capBuf, netplay.CaptureOptions{Now: clk.Now})

	srv := netplay.NewServerWorld(srvT)
	srv.SetEpoch(1)
	cli := netplay.NewClientWorld(cliRaw, 1)

	e1 := srv.Spawn(netplay.EntityState{
		ComponentState: netplay.ComponentState{
			Kind: 1, X: 10, Y: 20, VX: 5, VY: 0, Rotation: 0.5, Flags: 1, Props: []byte("hero"),
		},
	})
	e2 := srv.Spawn(netplay.EntityState{
		ComponentState: netplay.ComponentState{
			Kind: 2, X: 100, Y: 50,
		},
	})

	// Tick 1: full snapshot -> client ack.
	if err := srv.Step(1); err != nil {
		t.Fatal(err)
	}
	clk.Advance(16 * time.Millisecond)
	hub.Advance(16 * time.Millisecond)
	cli.Poll()
	clk.Advance(16 * time.Millisecond)
	hub.Advance(16 * time.Millisecond)

	// Tick 2: move e1 -> delta snapshot -> client ack.
	srv.Update(e1, netplay.EntityState{
		ComponentState: netplay.ComponentState{
			Kind: 1, X: 15, Y: 20, VX: 5, VY: 0, Rotation: 0.5, Flags: 1, Props: []byte("hero"),
		},
	})
	if err := srv.Step(2); err != nil {
		t.Fatal(err)
	}
	clk.Advance(16 * time.Millisecond)
	hub.Advance(16 * time.Millisecond)
	cli.Poll()
	clk.Advance(16 * time.Millisecond)
	hub.Advance(16 * time.Millisecond)

	// Tick 3: despawn e2 -> delta snapshot -> client ack.
	srv.Despawn(e2)
	if err := srv.Step(3); err != nil {
		t.Fatal(err)
	}
	clk.Advance(16 * time.Millisecond)
	hub.Advance(16 * time.Millisecond)
	cli.Poll()
	clk.Advance(16 * time.Millisecond)
	hub.Advance(16 * time.Millisecond)
	// Step tick 4 so server drains tick 3's ack and sends a no-op delta.
	if err := srv.Step(4); err != nil {
		t.Fatal(err)
	}

	assertGoldenDecodedCapture(t, "capture_replication.golden.yaml", capBuf.Bytes(), wire.InspectOptions{ResolveDeltas: false})
	resolvedFrames := assertGoldenDecodedCapture(t, "capture_replication_resolved.golden.yaml", capBuf.Bytes(), wire.InspectOptions{ResolveDeltas: true})

	// Verify that resolved deltas actually populated full entity states.
	resolvedCount := 0
	for _, fr := range resolvedFrames {
		for _, l := range fr.Layers {
			if l.Message == "SnapshotDelta" && len(l.Resolved) > 0 {
				resolvedCount++
			}
		}
	}
	if resolvedCount != 3 {
		t.Fatalf("resolved SnapshotDelta count = %d, want 3", resolvedCount)
	}
}

type deterministicSim struct {
	sum int64
}

func (s *deterministicSim) Step(inputs []netplay.InputFrame) {
	for _, in := range inputs {
		s.sum += int64(in.Peer)*1000 + int64(in.Bits) + int64(in.Axes[0]) - int64(in.Axes[1])
	}
}

func (s *deterministicSim) Save() []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(s.sum))
	return b[:]
}

func (s *deterministicSim) Load(data []byte) error {
	s.sum = int64(binary.BigEndian.Uint64(data))
	return nil
}

func (s *deterministicSim) Checksum() uint64 {
	h := fnv.New64a()
	_, _ = h.Write(s.Save())
	return h.Sum64()
}

func TestGoldenCaptureLockstep(t *testing.T) {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Latency: 10 * time.Millisecond, Seed: 3})
	clk := newStepClock()
	var capBuf bytes.Buffer

	p1Raw := hub.AddPeer(1)
	p2Raw := hub.AddPeer(2)
	p1T := netplay.NewCaptureTransport(p1Raw, &capBuf, netplay.CaptureOptions{Now: clk.Now})

	sim1 := &deterministicSim{}
	sim2 := &deterministicSim{}

	ls1 := netplay.NewLockstepSession(netplay.LockstepConfig{
		Transport:        p1T,
		Sim:              sim1,
		Peers:            []netplay.PeerID{1, 2},
		ChecksumInterval: 2,
	})
	ls2 := netplay.NewLockstepSession(netplay.LockstepConfig{
		Transport:        p2Raw,
		Sim:              sim2,
		Peers:            []netplay.PeerID{1, 2},
		ChecksumInterval: 2,
	})

	for tick := 0; tick < 3; tick++ {
		_ = ls1.SubmitInput(netplay.InputFrame{Tick: uint32(tick), Peer: 1, Bits: uint64(1 << tick), Axes: [2]int16{int16(tick * 10), -5}})
		_ = ls2.SubmitInput(netplay.InputFrame{Tick: uint32(tick), Peer: 2, Bits: uint64(1 << (tick + 1)), Axes: [2]int16{int16(-tick * 5), 10}})
		clk.Advance(10 * time.Millisecond)
		hub.Advance(10 * time.Millisecond)
		ls1.Step()
		ls2.Step()
		clk.Advance(10 * time.Millisecond)
		hub.Advance(10 * time.Millisecond)
		ls1.Step()
		ls2.Step()
	}

	assertGoldenDecodedCapture(t, "capture_lockstep.golden.yaml", capBuf.Bytes(), wire.InspectOptions{})
}

type loopbackRelayClient struct {
	id    netplay.PeerID
	relay *loopbackRelayHub
	inbox []netplay.Packet
	seq   uint32
}

type loopbackRelayHub struct {
	clients map[netplay.PeerID]*loopbackRelayClient
}

func newLoopbackRelayHub(ids ...netplay.PeerID) *loopbackRelayHub {
	h := &loopbackRelayHub{clients: make(map[netplay.PeerID]*loopbackRelayClient)}
	for _, id := range ids {
		h.clients[id] = &loopbackRelayClient{id: id, relay: h}
	}
	return h
}

func (h *loopbackRelayHub) announce(epoch uint32, members ...netplay.PeerID) {
	u32 := make([]uint32, len(members))
	for i, m := range members {
		u32[i] = uint32(m)
	}
	body := wire.AppendRelayMembers(nil, &wire.RelayMembers{Epoch: epoch, Peers: u32})
	frame := wire.AppendRelayFrame(nil, &wire.RelayFrame{Peer: 0, Body: body})
	for _, id := range members {
		c := h.clients[id]
		c.seq++
		c.inbox = append(c.inbox, netplay.Packet{
			From:    1,
			Channel: netplay.ChannelUnreliable,
			Seq:     c.seq,
			Payload: frame,
		})
	}
}

func (c *loopbackRelayClient) Send(to netplay.PeerID, ch netplay.Channel, payload []byte) error {
	if to != netplay.PeerBroadcast || ch != netplay.ChannelUnreliable {
		return netplay.ErrUnsupportedChannel
	}
	var rf wire.RelayFrame
	if _, err := wire.DecodeRelayFrame(payload, &rf); err != nil {
		return netplay.ErrInvalidPacket
	}
	dst := c.relay.clients[netplay.PeerID(rf.Peer)]
	if dst == nil {
		return nil
	}
	fwd := wire.AppendRelayFrame(nil, &wire.RelayFrame{Peer: uint32(c.id), Body: rf.Body})
	dst.seq++
	dst.inbox = append(dst.inbox, netplay.Packet{
		From:    1,
		Channel: netplay.ChannelUnreliable,
		Seq:     dst.seq,
		Payload: fwd,
	})
	return nil
}

func (c *loopbackRelayClient) Recv() (netplay.Packet, bool) {
	if len(c.inbox) == 0 {
		return netplay.Packet{}, false
	}
	p := c.inbox[0]
	c.inbox = c.inbox[1:]
	return p, true
}

func (c *loopbackRelayClient) LocalID() netplay.PeerID { return c.id }
func (c *loopbackRelayClient) Peers() []netplay.PeerID { return []netplay.PeerID{1} }
func (c *loopbackRelayClient) Close() error            { return nil }

func TestGoldenCaptureRelay(t *testing.T) {
	clk := newStepClock()
	var capBuf bytes.Buffer

	rh := newLoopbackRelayHub(2, 3)
	// Wrap peer 2's inner relay transport so the capture contains RelayFrame + RelayMembers + RelayReliable/RelayUnreliable + RelayAck + inner replication/rollback layers.
	c2Cap := netplay.NewCaptureTransport(rh.clients[2], &capBuf, netplay.CaptureOptions{Now: clk.Now})

	rt2 := netplay.NewRelayTransport(c2Cap, netplay.RelayTransportConfig{Now: clk.Now})
	rt3 := netplay.NewRelayTransport(rh.clients[3], netplay.RelayTransportConfig{Now: clk.Now})

	// Relay announces membership [2, 3] at epoch 1.
	rh.announce(1, 2, 3)
	_, _ = rt2.Recv()
	_, _ = rt3.Recv()

	// Peer 2 sends a reliable lockstep SingleInput to Peer 3, and Peer 3 receives it and replies with RelayAck.
	clk.Advance(10 * time.Millisecond)
	singleInput := wire.AppendRollbackMessage(nil, &wire.RollbackMessage{
		Tag: wire.RollbackMessageTagSingleInput,
		SingleInput: wire.SingleInput{
			Input: wire.InputFrame{Tick: 1, Peer: 2, Bits: 0x05, Axes: [2]int16{100, -50}},
		},
	})
	if err := rt2.Send(3, netplay.ChannelReliableOrdered, singleInput); err != nil {
		t.Fatalf("rt2 reliable Send: %v", err)
	}
	clk.Advance(10 * time.Millisecond)
	if pkt, ok := rt3.Recv(); !ok || !bytes.Equal(pkt.Payload, singleInput) {
		t.Fatalf("rt3 Recv reliable = %+v ok=%v", pkt, ok)
	}
	// Peer 2 drains RelayAck sent by Peer 3.
	clk.Advance(10 * time.Millisecond)
	_, _ = rt2.Recv()

	// Peer 3 sends an unreliable ReplicationMessage SnapshotAck to Peer 2.
	clk.Advance(10 * time.Millisecond)
	replAck := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
		Tag: wire.ReplicationMessageTagAck,
		Ack: wire.SnapshotAck{Epoch: 1, Tick: 5},
	})
	if err := rt3.Send(2, netplay.ChannelUnreliable, replAck); err != nil {
		t.Fatalf("rt3 unreliable Send: %v", err)
	}
	clk.Advance(10 * time.Millisecond)
	if pkt, ok := rt2.Recv(); !ok || !bytes.Equal(pkt.Payload, replAck) {
		t.Fatalf("rt2 Recv unreliable = %+v ok=%v", pkt, ok)
	}

	assertGoldenDecodedCapture(t, "capture_relay.golden.yaml", capBuf.Bytes(), wire.InspectOptions{})
}

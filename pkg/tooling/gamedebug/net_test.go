//go:build !release

package gamedebug_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestNetMonitorZeroAllocsAndOverlayUpdate(t *testing.T) {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{
		Inbox: netplay.InboxLimits{Unreliable: 16},
	})
	p1Raw := hub.AddPeer(1)
	p2Raw := hub.AddPeer(2)

	mon := gamedebug.NewNetMonitor(8)
	p1 := mon.Wrap(p1Raw)
	_ = p1
	_ = p2Raw

	// Pre-encode representative payloads from every #1301 protocol so ObserveSend / ObserveRecv exercise every generated decoder path.
	deltaPayload := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
		Tag: wire.ReplicationMessageTagDelta,
		Delta: wire.SnapshotDelta{
			Epoch:        1,
			Tick:         4,
			BaselineTick: 3,
			Updates: []wire.EntityRecord{
				{
					ID:         1,
					FieldsMask: wire.EntityRecordFieldX | wire.EntityRecordFieldY | wire.EntityRecordFieldProps,
					X:          12.5,
					Y:          4.0,
					Props:      []byte{1, 2, 3, 4},
				},
			},
		},
	})
	inputPayload := wire.AppendRollbackMessage(nil, &wire.RollbackMessage{
		Tag: wire.RollbackMessageTagSingleInput,
		SingleInput: wire.SingleInput{
			Input: wire.InputFrame{Tick: 10, Peer: 2, Bits: 0x05, Axes: [2]int16{10, -20}},
		},
	})
	relayBody := wire.AppendRelayBody(nil, &wire.RelayBody{
		Tag:        wire.RelayBodyTagUnreliable,
		Unreliable: wire.RelayUnreliable{Seq: 7, Payload: deltaPayload},
	})
	relayPayload := wire.AppendRelayFrame(nil, &wire.RelayFrame{Peer: 2, Body: relayBody})

	recvPkt := netplay.Packet{
		From:    2,
		Channel: netplay.ChannelUnreliable,
		Seq:     1,
		Payload: relayPayload,
	}

	dbg := gamedebug.New(gamedebug.Options{
		Visible: true,
		Net:     mon.Snapshot,
	})

	// Warm up reused wire codec slices, peer table, recent buffer, and overlay's lastNet buffer.
	for range 4 {
		mon.ObserveSend(2, netplay.ChannelReliableOrdered, inputPayload)
		mon.ObserveRecv(recvPkt)
		mon.SampleFrame(16 * time.Millisecond)
		dbg.RecordFrame(16*time.Millisecond, 4*time.Millisecond, 3*time.Millisecond)
	}

	// Verify decoded types and non-zero rates after warmup.
	snap := mon.Snapshot()
	if len(snap.Channels) != 2 {
		t.Fatalf("Channels len = %d, want 2", len(snap.Channels))
	}
	if len(snap.Peers) != 1 || snap.Peers[0].Peer != 2 || snap.Peers[0].BytesPerSec <= 0 || snap.Peers[0].PacketsPerSec <= 0 {
		t.Fatalf("unexpected Peers stats: %+v", snap.Peers)
	}
	if len(snap.Recent) != 8 {
		t.Fatalf("Recent len = %d, want 8", len(snap.Recent))
	}
	last := snap.Recent[len(snap.Recent)-1]
	if last.Type != "relay.unreliable/replication.delta" {
		t.Fatalf("last recent type = %q, want relay.unreliable/replication.delta", last.Type)
	}
	prev := snap.Recent[len(snap.Recent)-2]
	if prev.Type != "rollback.single_input" {
		t.Fatalf("prev recent type = %q, want rollback.single_input", prev.Type)
	}

	// Acceptance criterion: debug.AssertZeroAllocs on the overlay's per-frame update (including decoding packets with #1301 codecs into reused buffers and refreshing the overlay).
	gamedebug.AssertZeroAllocs(t, 200, func() {
		mon.ObserveSend(2, netplay.ChannelReliableOrdered, inputPayload)
		mon.ObserveRecv(recvPkt)
		mon.SampleFrame(16 * time.Millisecond)
		dbg.RecordFrame(16*time.Millisecond, 4*time.Millisecond, 3*time.Millisecond)
	})
}

func TestOverlayNetPanelRendersAndUpdates(t *testing.T) {
	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{
		Inbox: netplay.InboxLimits{Unreliable: 1},
	})
	p1Raw := hub.AddPeer(1)
	p2Raw := hub.AddPeer(2)

	mon := gamedebug.NewNetMonitor(8)
	p1 := mon.Wrap(p1Raw)

	ackPayload := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
		Tag: wire.ReplicationMessageTagAck,
		Ack: wire.SnapshotAck{Epoch: 1, Tick: 5},
	})
	// Trigger an unreliable drop so InboxStats.UnreliableDropped == 1.
	_ = p2Raw.Send(1, netplay.ChannelUnreliable, ackPayload)
	_ = p2Raw.Send(1, netplay.ChannelUnreliable, ackPayload)
	hub.Advance(time.Millisecond)
	_, _ = p1.Recv()
	_ = p1.Send(2, netplay.ChannelReliableOrdered, ackPayload)
	mon.SampleFrame(16 * time.Millisecond)

	dbg := gamedebug.New(gamedebug.Options{
		Visible: true,
		Net:     mon.Snapshot,
	})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(400)},
			Children: []core.Node{dbg.View(cx)},
		}
	})

	if !app.Exists("debug-net-inspector") {
		t.Fatal("debug-net-inspector not mounted when Options.Net is set")
	}
	if !app.Exists("debug-net-recent") {
		t.Fatal("debug-net-recent not mounted when recent events exist")
	}

	texts := app.Texts()
	joined := strings.Join(texts, "\n")
	for _, want := range []string{
		"Net",
		"inbox: rel 0 / unrel 0 · drop 1 · ref 0 · warn 0",
		"reliable-ordered",
		"unreliable",
		"peer 2",
		"replication.ack",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overlay net texts missing %q:\n%s", want, joined)
		}
	}

	// Test pushed SetNet / ClearNet when Options.Net is nil.
	pushed := gamedebug.New(gamedebug.Options{Visible: true})
	app2 := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(400)},
			Children: []core.Node{pushed.View(cx)},
		}
	})
	if app2.Exists("debug-net-inspector") {
		t.Fatal("debug-net-inspector mounted before SetNet")
	}
	app2.Post(func() {
		pushed.SetNet(mon.Snapshot())
	})
	app2.Settle()
	if !app2.Exists("debug-net-inspector") {
		t.Fatal("debug-net-inspector not mounted after SetNet")
	}
	if !slices.Contains(app2.Texts(), "Net") {
		t.Fatalf("pushed net overlay texts %v missing Net", app2.Texts())
	}
	app2.Post(pushed.ClearNet)
	app2.Settle()
	if app2.Exists("debug-net-inspector") {
		t.Fatal("debug-net-inspector still mounted after ClearNet")
	}
}

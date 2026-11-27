//go:build !js && !wasip1

package netplay_test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
)

type tsE2EEntity struct {
	ID       uint32 `json:"id"`
	Kind     uint16 `json:"kind"`
	XBits    uint32 `json:"xBits"`
	YBits    uint32 `json:"yBits"`
	VXBits   uint32 `json:"vxBits"`
	VYBits   uint32 `json:"vyBits"`
	RotBits  uint32 `json:"rotBits"`
	Flags    uint32 `json:"flags"`
	PropsHex string `json:"propsHex"`
}

type tsE2EReply struct {
	Kind     string        `json:"kind"`
	Epoch    uint32        `json:"epoch"`
	Tick     uint32        `json:"tick"`
	ReplyHex string        `json:"replyHex"`
	Entities []tsE2EEntity `json:"entities"`
}

// TestServerWorldTypeScriptE2E drives a real Go ServerWorld across Full, Delta (triggered by a TypeScript-encoded SnapshotAck), epoch switch, and split FullPart snapshots, verifying the generated TypeScript decoder reproduces every entity state bit-for-bit.
func TestServerWorldTypeScriptE2E(t *testing.T) {
	bunPath, err := exec.LookPath("bun")
	if err != nil {
		if os.Getenv("REQUIRE_BUN_E2E") == "1" {
			t.Fatalf("REQUIRE_BUN_E2E=1 but bun not found in PATH: %v", err)
		}
		t.Skip("bun not found in PATH; skipping Go-to-TypeScript E2E test")
	}

	clientScript := filepath.Join("..", "..", "..", "examples", "ts-replication-client", "client.ts")
	cmd := exec.Command(bunPath, "run", clientScript, "--e2e")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start bun E2E bridge: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	sendToTS := func(pkt []byte) tsE2EReply {
		t.Helper()
		req := fmt.Sprintf("{\"hex\":%q}\n", hex.EncodeToString(pkt))
		if _, err := stdin.Write([]byte(req)); err != nil {
			t.Fatalf("write to bun bridge: %v (stderr: %s)", err, stderr.String())
		}
		if !scanner.Scan() {
			t.Fatalf("read from bun bridge failed: err=%v stderr=%s", scanner.Err(), stderr.String())
		}
		var rep tsE2EReply
		if err := json.Unmarshal(scanner.Bytes(), &rep); err != nil {
			t.Fatalf("parse bun reply %q: %v", scanner.Text(), err)
		}
		return rep
	}

	assertEntitiesMatch := func(label string, want map[netplay.EntityID]netplay.EntityState, got []tsE2EEntity) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: entity count = %d, want %d", label, len(got), len(want))
		}
		for _, g := range got {
			w, ok := want[netplay.EntityID(g.ID)]
			if !ok {
				t.Fatalf("%s: unexpected entity id %d", label, g.ID)
			}
			if g.Kind != w.Kind ||
				g.XBits != math.Float32bits(w.X) ||
				g.YBits != math.Float32bits(w.Y) ||
				g.VXBits != math.Float32bits(w.VX) ||
				g.VYBits != math.Float32bits(w.VY) ||
				g.RotBits != math.Float32bits(w.Rotation) ||
				g.Flags != w.Flags ||
				g.PropsHex != hex.EncodeToString(w.Props) {
				t.Fatalf("%s: entity %d mismatch:\nwant %+v (x=0x%08x y=0x%08x props=%x)\ngot  %+v",
					label, g.ID, w, math.Float32bits(w.X), math.Float32bits(w.Y), w.Props, g)
			}
		}
	}

	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{})
	serverTx := hub.AddPeer(1)
	clientTx := hub.AddPeer(2)
	server := netplay.NewServerWorld(serverTx)
	server.SetEpoch(0x11223344)

	negZero := float32(math.Copysign(0, -1))
	wantWorld := make(map[netplay.EntityID]netplay.EntityState)

	// Tick 1: spawn 2 entities (including -0.0 float bit pattern and custom Props) -> Full snapshot.
	e1 := netplay.EntityState{
		ComponentState: netplay.ComponentState{
			Kind: 1, X: 12.5, Y: negZero, VX: -3.25, VY: 4.5, Rotation: 1.5707964, Flags: 0x0f,
			Props: []byte{0xde, 0xad, 0xbe, 0xef},
		},
	}
	e1.ID = server.Spawn(e1)
	wantWorld[e1.ID] = e1.Clone()

	e2 := netplay.EntityState{
		ComponentState: netplay.ComponentState{
			Kind: 2, X: -100, Y: 250.75, Flags: 0x100,
		},
	}
	e2.ID = server.Spawn(e2)
	wantWorld[e2.ID] = e2.Clone()

	if err := server.Step(1); err != nil {
		t.Fatalf("Step(1): %v", err)
	}
	hub.Advance(time.Millisecond)

	pkt1, ok := clientTx.Recv()
	if !ok {
		t.Fatal("expected full snapshot packet on tick 1")
	}
	rep1 := sendToTS(pkt1.Payload)
	if rep1.Kind != "full" || rep1.Epoch != 0x11223344 || rep1.Tick != 1 {
		t.Fatalf("tick 1 reply = %+v, want kind=full epoch=0x11223344 tick=1", rep1)
	}
	assertEntitiesMatch("tick 1 full", wantWorld, rep1.Entities)

	// Send the TypeScript-encoded SnapshotAck back to Go ServerWorld so tick 2 emits a real SnapshotDelta!
	ackBytes, err := hex.DecodeString(rep1.ReplyHex)
	if err != nil || len(ackBytes) == 0 {
		t.Fatalf("invalid TS ack hex %q: %v", rep1.ReplyHex, err)
	}
	if err := clientTx.Send(1, netplay.ChannelUnreliable, ackBytes); err != nil {
		t.Fatalf("send TS ack: %v", err)
	}
	hub.Advance(time.Millisecond)

	// Tick 2: update e1 (including clearing Props to empty and changing X), despawn e2, spawn e3.
	e1.X = 99.125
	e1.Flags = 0x80000001
	e1.Props = nil
	server.Update(e1.ID, e1)
	wantWorld[e1.ID] = e1.Clone()

	server.Despawn(e2.ID)
	delete(wantWorld, e2.ID)

	e3 := netplay.EntityState{
		ComponentState: netplay.ComponentState{
			Kind: 77, X: 0.125, Y: -0.125, Rotation: -3.1415927, Flags: 42, Props: []byte{1, 2, 3, 4, 5},
		},
	}
	e3.ID = server.Spawn(e3)
	wantWorld[e3.ID] = e3.Clone()

	if err := server.Step(2); err != nil {
		t.Fatalf("Step(2): %v", err)
	}
	hub.Advance(time.Millisecond)

	if st := server.Stats(); st.DeltaSnapshotsSent != 1 {
		t.Fatalf("expected ServerWorld to accept TS SnapshotAck and send 1 delta snapshot, stats = %+v", st)
	}
	pkt2, ok := clientTx.Recv()
	if !ok {
		t.Fatal("expected delta snapshot packet on tick 2")
	}
	rep2 := sendToTS(pkt2.Payload)
	if rep2.Kind != "delta" || rep2.Epoch != 0x11223344 || rep2.Tick != 2 {
		t.Fatalf("tick 2 reply = %+v, want kind=delta epoch=0x11223344 tick=2", rep2)
	}
	assertEntitiesMatch("tick 2 delta", wantWorld, rep2.Entities)

	// Tick 3: switch epoch and force split SnapshotFullPart packets by lowering max snapshot size to 256 bytes.
	server.SetMaxSnapshotSize(256)
	server.SetEpoch(0x22334455)
	for i := 0; i < 12; i++ {
		e := netplay.EntityState{
			ComponentState: netplay.ComponentState{
				Kind: uint16(10 + i), X: float32(i) * 1.5, Y: float32(-i) * 2.25,
				Flags: uint32(i * 100), Props: bytes.Repeat([]byte{byte(i + 1)}, 20),
			},
		}
		e.ID = server.Spawn(e)
		wantWorld[e.ID] = e.Clone()
	}

	if err := server.Step(3); err != nil {
		t.Fatalf("Step(3): %v", err)
	}
	hub.Advance(time.Millisecond)

	var parts [][]byte
	for {
		pkt, ok := clientTx.Recv()
		if !ok {
			break
		}
		parts = append(parts, pkt.Payload)
	}
	if len(parts) < 2 {
		t.Fatalf("expected split full snapshot (>=2 parts), got %d packet(s)", len(parts))
	}

	var lastRep tsE2EReply
	for i, part := range parts {
		lastRep = sendToTS(part)
		if i < len(parts)-1 && lastRep.Kind != "full_part_pending" {
			t.Fatalf("part %d/%d: kind = %q, want full_part_pending", i+1, len(parts), lastRep.Kind)
		}
	}
	if lastRep.Kind != "full_part_complete" || lastRep.Epoch != 0x22334455 || lastRep.Tick != 3 {
		t.Fatalf("final split part reply = %+v, want full_part_complete epoch=0x22334455 tick=3", lastRep)
	}
	assertEntitiesMatch("tick 3 split full_part", wantWorld, lastRep.Entities)
}

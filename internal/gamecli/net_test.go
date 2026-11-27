package gamecli

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
	"go.yaml.in/yaml/v3"
)

func buildTestReplicationCapture(t *testing.T, withCustomProps bool) ([]byte, string) {
	t.Helper()
	var customPropsBytes []byte
	schemaDir := filepath.Join("..", "..", "pkg", "online", "netplay", "wire", "testdata")
	if withCustomProps {
		custom, err := wire.LoadSchemaPath(schemaDir)
		if err != nil {
			t.Fatalf("LoadSchemaPath: %v", err)
		}
		reg, err := wire.NewRegistry(custom...)
		if err != nil {
			t.Fatalf("NewRegistry: %v", err)
		}
		propsBytes, err := reg.EncodeMessage("FighterProps", wire.DecodedObject{
			{Name: "team", Value: uint64(2)},
			{Name: "combo", Value: uint64(5)},
			{Name: "aim_x", Value: float32(0.5)},
			{Name: "aim_y", Value: float32(0)},
			{Name: "stance", Value: wire.DecodedEnum{Name: "running", Value: 1}},
			{Name: "hp_delta", Value: int64(-10)},
			{Name: "stamina", Value: float64(50.5)},
			{Name: "state", Value: wire.DecodedMask{
				Mask: 0x02,
				Fields: wire.DecodedObject{
					{Name: "tag", Value: "dash"},
				},
			}},
		})
		if err != nil {
			t.Fatalf("EncodeMessage(FighterProps): %v", err)
		}
		customPropsBytes = propsBytes
	} else {
		customPropsBytes = []byte("hero")
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := func() time.Time { return now }

	hub := netplay.NewLoopbackHub(netplay.LoopbackConfig{Latency: 10 * time.Millisecond, Seed: 1})
	var buf bytes.Buffer
	srvT := netplay.NewCaptureTransport(hub.AddPeer(1), &buf, netplay.CaptureOptions{Now: clk})
	cliT := hub.AddPeer(2)

	srv := netplay.NewServerWorld(srvT)
	srv.SetEpoch(1)
	cli := netplay.NewClientWorld(cliT, 1)

	e1 := srv.Spawn(netplay.EntityState{
		ComponentState: netplay.ComponentState{
			Kind: 1, X: 10, Y: 20, Props: customPropsBytes,
		},
	})
	if err := srv.Step(1); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Millisecond)
	hub.Advance(10 * time.Millisecond)
	cli.Poll()
	now = now.Add(10 * time.Millisecond)
	hub.Advance(10 * time.Millisecond)

	srv.Update(e1, netplay.EntityState{
		ComponentState: netplay.ComponentState{
			Kind: 1, X: 25, Y: 20, Props: customPropsBytes,
		},
	})
	if err := srv.Step(2); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Millisecond)
	hub.Advance(10 * time.Millisecond)
	cli.Poll()
	now = now.Add(10 * time.Millisecond)
	hub.Advance(10 * time.Millisecond)
	if err := srv.Step(3); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), schemaDir
}

func TestNetDecodeYAMLJSONSchemaAndResolveDeltas(t *testing.T) {
	capBytes, schemaDir := buildTestReplicationCapture(t, true)
	capFile := filepath.Join(t.TempDir(), "session.capture.jsonl")
	if err := os.WriteFile(capFile, capBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// YAML decode with --schema and --resolve-deltas (flags after verb).
	a, stdout, stderr := newTestApp(t)
	code := run([]string{"net", "decode", "--schema", schemaDir, "--resolve-deltas", capFile}, a)
	if code != 0 {
		t.Fatalf("net decode yaml exit %d (stderr: %s)", code, stderr)
	}
	out := stdout.String()
	for _, want := range []string{
		"layer: transport",
		"layer: replication",
		"message: SnapshotFull",
		"message: SnapshotDelta",
		"message: FighterProps",
		"stance: running",
		"tag: dash",
		"resolved:",
		"x: 25",
		"y: 20",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("YAML output missing %q:\n%s", want, out)
		}
	}
	var parsedYAML []any
	if err := yaml.Unmarshal(stdout.Bytes(), &parsedYAML); err != nil {
		t.Fatalf("decoded YAML failed to parse: %v\n%s", err, out)
	}

	// JSON decode (flags before verb also work).
	a, stdout, stderr = newTestApp(t)
	code = run([]string{"net", "-format", "json", "-resolve-deltas", "decode", capFile}, a)
	if code != 0 {
		t.Fatalf("net decode json exit %d (stderr: %s)", code, stderr)
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("net decode json produced invalid JSON:\n%s", stdout)
	}
	var parsedJSON []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsedJSON); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(parsedJSON) == 0 {
		t.Fatal("expected decoded JSON frames")
	}
}

func TestNetDecodeStdinHexBase64AndJSONL(t *testing.T) {
	payload := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
		Tag: wire.ReplicationMessageTagAck,
		Ack: wire.SnapshotAck{Epoch: 3, Tick: 77},
	})
	tf := wire.AppendTransportFrame(nil, &wire.TransportFrame{
		Channel: wire.ChannelUnreliable,
		Seq:     9,
		Payload: payload,
	})

	// 1. Single hex frame on stdin.
	a, stdout, stderr := newTestApp(t)
	a.stdin = strings.NewReader(hex.EncodeToString(tf) + "\n")
	if code := run([]string{"net", "decode", "-"}, a); code != 0 {
		t.Fatalf("decode hex stdin exit %d (stderr: %s)", code, stderr)
	}
	for _, want := range []string{"layer: transport", "channel: unreliable", "seq: 9", "layer: replication", "case: ack", "tick: 77"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("hex stdin output missing %q:\n%s", want, stdout)
		}
	}

	// 2. Single base64 frame on stdin (JSON format).
	a, stdout, stderr = newTestApp(t)
	a.stdin = strings.NewReader(base64.StdEncoding.EncodeToString(payload) + "\n")
	if code := run([]string{"net", "decode", "--format", "json", "-"}, a); code != 0 {
		t.Fatalf("decode base64 stdin exit %d (stderr: %s)", code, stderr)
	}
	if !json.Valid(stdout.Bytes()) || !strings.Contains(stdout.String(), `"case": "ack"`) {
		t.Fatalf("unexpected base64 stdin output:\n%s", stdout)
	}

	// 3. JSONL capture piped on stdin.
	capBytes, _ := buildTestReplicationCapture(t, false)
	a, stdout, stderr = newTestApp(t)
	a.stdin = bytes.NewReader(capBytes)
	if code := run([]string{"net", "decode", "-"}, a); code != 0 {
		t.Fatalf("decode jsonl stdin exit %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout.String(), "message: SnapshotFull") {
		t.Fatalf("jsonl stdin output missing SnapshotFull:\n%s", stdout)
	}
}

func TestNetStatsTextYAMLAndJSON(t *testing.T) {
	capBytes, _ := buildTestReplicationCapture(t, false)
	capFile := filepath.Join(t.TempDir(), "stats.capture.jsonl")
	if err := os.WriteFile(capFile, capBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Default text summary.
	a, stdout, stderr := newTestApp(t)
	if code := run([]string{"net", "stats", capFile}, a); code != 0 {
		t.Fatalf("net stats text exit %d (stderr: %s)", code, stderr)
	}
	out := stdout.String()
	for _, want := range []string{
		"Packets:",
		"Mean delta size:",
		"By message type:",
		"replication.full",
		"replication.delta",
		"replication.ack",
		"By peer:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("net stats text missing %q:\n%s", want, out)
		}
	}

	// JSON stats.
	a, stdout, stderr = newTestApp(t)
	if code := run([]string{"net", "stats", "--format", "json", capFile}, a); code != 0 {
		t.Fatalf("net stats json exit %d (stderr: %s)", code, stderr)
	}
	var st wire.CaptureStats
	if err := json.Unmarshal(stdout.Bytes(), &st); err != nil {
		t.Fatalf("json.Unmarshal stats: %v\n%s", err, stdout)
	}
	if st.TotalPackets == 0 || st.DeltaCount < 2 || st.MeanDeltaBytes <= 0 {
		t.Fatalf("unexpected stats summary: %+v", st)
	}
}

func TestNetUsageErrors(t *testing.T) {
	cases := [][]string{
		{"net"},
		{"net", "unknown"},
		{"net", "decode"},
		{"net", "decode", "--format", "xml", "-"},
		{"net", "stats"},
		{"net", "stats", "--resolve-deltas", "-"},
		{"net", "stats", "--format", "xml", "-"},
	}
	for _, args := range cases {
		a, _, stderr := newTestApp(t)
		if code := run(args, a); code != 2 {
			t.Errorf("%v: exit %d, want 2 (stderr: %s)", args, code, stderr)
		}
	}
}

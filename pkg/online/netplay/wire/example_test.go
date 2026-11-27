package wire_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
)

// QuantizeFloat and DequantizeFloat map float32 values into compact integer bit widths using integer arithmetic only, producing bit-identical bytes on amd64, arm64, and wasm.
func ExampleQuantizeFloat() {
	q := wire.QuantizeFloat(0.25, 0, 1, 8)
	v := wire.DequantizeFloat(q, 0, 1, 8)
	fmt.Printf("q=%d v=%.4f\n", q, v)
	// Output:
	// q=64 v=0.2510
}

// Generated codecs encode into caller-owned slices with AppendX and decode into caller-owned structs with DecodeX, while DiffX and ApplyX compute and apply presence-bitmask deltas.
func ExampleDiffEntityRecord() {
	base := wire.EntityRecord{ID: 7, Kind: 1, X: 10, Y: 20}
	cur := base
	cur.X = 14.5
	cur.Props = []byte{0x01, 0x02}

	cur.FieldsMask = wire.DiffEntityRecord(&base, &cur)
	buf := wire.AppendEntityRecord(make([]byte, 0, 64), &cur)

	var decoded wire.EntityRecord
	n, err := wire.DecodeEntityRecord(buf, &decoded)
	if err != nil {
		panic(err)
	}
	wire.ApplyEntityRecord(&base, &decoded)
	fmt.Printf("bytes=%d mask=0x%02x x=%.1f y=%.1f props=%x\n", n, decoded.FieldsMask, base.X, base.Y, base.Props)
	// Output:
	// bytes=9 mask=0x82 x=14.5 y=20.0 props=0102
}

// Parse reads a *.wire.yaml schema strictly and GenerateGo writes deterministic Go codecs for it.
func ExampleGenerateGo() {
	const src = `version: 1
package: demo
messages:
  - name: Ping
    fields:
      - name: seq
        type: uvarint
        bits: 32
`
	s, err := wire.Parse([]byte(src))
	if err != nil {
		panic(err)
	}
	code, err := wire.GenerateGo(s, wire.GenerateOptions{SourceName: "ping.wire.yaml"})
	if err != nil {
		panic(err)
	}
	fmt.Println(len(code) > 0, s.Messages[0].Name)
	// Output:
	// true Ping
}

// NewInspector decodes a captured packet layer by layer using the built-in wire schemas and re-encodes it back to the exact original bytes.
func ExampleNewInspector() {
	ins, err := wire.NewInspector(nil, wire.InspectOptions{})
	if err != nil {
		panic(err)
	}
	payload := wire.AppendReplicationMessage(nil, &wire.ReplicationMessage{
		Tag: wire.ReplicationMessageTagAck,
		Ack: wire.SnapshotAck{Epoch: 1, Tick: 42},
	})
	frame := ins.InspectCaptureRecord(0, 0, "send", 1, uint8(wire.ChannelUnreliable), 1, payload)
	reencoded, err := ins.ReencodePayload(frame)
	if err != nil {
		panic(err)
	}
	fmt.Println(wire.FrameMessageType(frame), len(frame.Layers), len(reencoded) == len(payload))
	// Output:
	// replication.ack 2 true
}

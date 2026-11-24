package net

import (
	"encoding/binary"
	"testing"
)

func TestApplyFullRejectsOversizedEntityCount(t *testing.T) {
	// #546: the corrupt packet's count must be rejected before the map is made.
	var src []byte
	src = binary.AppendUvarint(src, 2)
	src = binary.AppendUvarint(src, MaxEntitiesPerSnapshot+1)
	c := &ClientWorld{}
	if c.applyFull(src) {
		t.Fatal("oversized snapshot count was accepted")
	}
}

// FuzzApplyFullPart feeds arbitrary snapshot parts to a client: none may panic, and an assembly never holds more than MaxEntitiesPerSnapshot entities or maxSnapshotParts parts.
func FuzzApplyFullPart(f *testing.F) {
	parts, err := encodeFullSnapshotParts(7, map[EntityID]EntityState{
		1: {ID: 1, ComponentState: ComponentState{X: 1, Props: []byte("a")}},
		2: {ID: 2, ComponentState: ComponentState{Y: 2, Props: make([]byte, 200)}},
		3: {ID: 3, ComponentState: ComponentState{Y: 3, Props: make([]byte, 200)}},
	}, minSnapshotSize)
	if err != nil {
		f.Fatal(err)
	}
	for _, p := range parts {
		f.Add(p[1:])
	}
	f.Add([]byte{7, 0, 1, 0})
	f.Fuzz(func(t *testing.T, src []byte) {
		c := NewClientWorld(nopTransport{}, 1)
		c.applyFullPart(src)
		c.applyFullPart(src)
		if a := c.assembly; a != nil && (len(a.entities) > MaxEntitiesPerSnapshot || len(a.got) > maxSnapshotParts) {
			t.Fatalf("assembly holds %d entities in %d parts", len(a.entities), len(a.got))
		}
	})
}

func TestSplitFullSnapshotPartsReassemble(t *testing.T) {
	entities := make(map[EntityID]EntityState)
	for i := range EntityID(40) {
		entities[i+1] = EntityState{ID: i + 1, ComponentState: ComponentState{X: float32(i), Props: make([]byte, 50)}}
	}
	parts, err := encodeFullSnapshotParts(9, entities, minSnapshotSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 {
		t.Fatalf("got %d parts, want a split", len(parts))
	}
	c := NewClientWorld(nopTransport{}, 1)
	// Deliver in reverse, with a duplicate: order does not matter and a duplicate does not count twice.
	for i := len(parts) - 1; i >= 0; i-- {
		if len(parts[i]) > minSnapshotSize {
			t.Fatalf("part %d is %d bytes, above %d", i, len(parts[i]), minSnapshotSize)
		}
		if i == len(parts)-1 && c.applyFullPart(parts[i][1:]) {
			t.Fatal("a duplicate first part completed the snapshot")
		}
		done := c.applyFullPart(parts[i][1:])
		if done != (i == 0) {
			t.Fatalf("part %d completed = %v", i, done)
		}
	}
	for id, want := range entities {
		got, ok := c.Entity(id)
		if !ok || got.X != want.X || len(got.Props) != 50 {
			t.Fatalf("entity %d = %+v, %v", id, got, ok)
		}
	}
}

// nopTransport accepts and discards everything, for driving a ClientWorld directly.
type nopTransport struct{}

func (nopTransport) Send(PeerID, Channel, []byte) error { return nil }
func (nopTransport) Recv() (Packet, bool)               { return Packet{}, false }
func (nopTransport) LocalID() PeerID                    { return 2 }
func (nopTransport) Peers() []PeerID                    { return nil }
func (nopTransport) Close() error                       { return nil }

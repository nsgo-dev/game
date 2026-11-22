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

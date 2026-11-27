package ktx2

import (
	"fmt"
	"testing"
)

// bc7Layout gives the absolute bit offset of every field of a BC7 mode, as bc7enc's reference decoder reads them from the block's two 64-bit halves, so these tests do not share the decoder's sequential reading. A negative offset is a field the mode lacks.
type bc7Layout struct {
	part, rot, isb     int
	color, colorBits   int
	alpha, alphaBits   int
	pbit               int
	shared             bool
	index, indexBits   int
	index2, index2Bits int
}

var bc7Layouts = [8]bc7Layout{
	{part: 1, rot: -1, isb: -1, color: 5, colorBits: 4, alpha: -1, pbit: 77, index: 83, indexBits: 3, index2: -1},
	{part: 2, rot: -1, isb: -1, color: 8, colorBits: 6, alpha: -1, pbit: 80, shared: true, index: 82, indexBits: 3, index2: -1},
	{part: 3, rot: -1, isb: -1, color: 9, colorBits: 5, alpha: -1, pbit: -1, index: 99, indexBits: 2, index2: -1},
	{part: 4, rot: -1, isb: -1, color: 10, colorBits: 7, alpha: -1, pbit: 94, index: 98, indexBits: 2, index2: -1},
	{part: -1, rot: 5, isb: 7, color: 8, colorBits: 5, alpha: 38, alphaBits: 6, pbit: -1, index: 50, indexBits: 2, index2: 81, index2Bits: 3},
	{part: -1, rot: 6, isb: -1, color: 8, colorBits: 7, alpha: 50, alphaBits: 8, pbit: -1, index: 66, indexBits: 2, index2: 97, index2Bits: 2},
	{part: -1, rot: -1, isb: -1, color: 7, colorBits: 7, alpha: 49, alphaBits: 7, pbit: 63, index: 65, indexBits: 4, index2: -1},
	{part: 8, rot: -1, isb: -1, color: 14, colorBits: 5, alpha: 74, alphaBits: 5, pbit: 94, index: 98, indexBits: 2, index2: -1},
}

func bc7Put(b *[16]byte, pos, n int, v uint32) {
	for i := range n {
		if v>>i&1 != 0 {
			b[(pos+i)>>3] |= 1 << ((pos + i) & 7)
		}
	}
}

func bc7TestExpand(v uint32, bits int) int {
	v <<= 8 - bits
	return int(v | v>>bits)
}

// TestDecodeBC7AllModes builds a block of every mode, partition count, rotation and index selection with each subset's second endpoint at full intensity in its own channel, and checks every texel against the BC7 arithmetic.
func TestDecodeBC7AllModes(t *testing.T) {
	weights := map[int][]int{2: {0, 21, 43, 64}, 3: {0, 9, 18, 27, 37, 46, 55, 64}, 4: {0, 4, 9, 13, 17, 21, 26, 30, 34, 38, 43, 47, 51, 55, 60, 64}}
	for mode, l := range bc7Layouts {
		subsets := bc7Modes[mode].subsets
		for _, variant := range []struct{ partition, rot, isb int }{{0, 0, 0}, {13, 1, 1}, {63, 3, 0}} {
			partition := variant.partition
			if l.part < 0 {
				partition = 0
			} else if mode == 0 {
				partition %= 16
			}
			rot, isb := variant.rot, variant.isb
			if l.rot < 0 {
				rot = 0
			}
			if l.isb < 0 {
				isb = 0
			}
			t.Run(fmt.Sprintf("mode%d/p%d/r%d/i%d", mode, partition, rot, isb), func(t *testing.T) {
				var b [16]byte
				b[0] = 1 << mode
				if l.part >= 0 {
					bc7Put(&b, l.part, bc7Modes[mode].partitionBits, uint32(partition))
				}
				if l.rot >= 0 {
					bc7Put(&b, l.rot, 2, uint32(rot))
				}
				if l.isb >= 0 {
					bc7Put(&b, l.isb, 1, uint32(isb))
				}
				nEnd := subsets * 2
				// Endpoint 2s+1 is full in channel s and in alpha; every other endpoint is zero.
				raw := func(e, ch, bits int) uint32 {
					if e%2 == 1 && (ch == e/2 || ch == 3) {
						return 1<<bits - 1
					}
					return 0
				}
				for ch := range 3 {
					for e := range nEnd {
						bc7Put(&b, l.color+(ch*nEnd+e)*l.colorBits, l.colorBits, raw(e, ch, l.colorBits))
					}
				}
				if l.alpha >= 0 {
					for e := range nEnd {
						bc7Put(&b, l.alpha+e*l.alphaBits, l.alphaBits, raw(e, 3, l.alphaBits))
					}
				}
				pbit := func(e int) uint32 { return uint32(e % 2) }
				if l.pbit >= 0 {
					if l.shared {
						for s := range subsets {
							bc7Put(&b, l.pbit+s, 1, uint32(s%2))
						}
						pbit = func(e int) uint32 { return uint32((e / 2) % 2) }
					} else {
						for e := range nEnd {
							bc7Put(&b, l.pbit+e, 1, pbit(e))
						}
					}
				}
				subsetOf := [16]uint8{}
				anchor := map[int]bool{0: true}
				switch subsets {
				case 2:
					subsetOf = bc7Partition2[partition]
					anchor[int(bc7Anchor2Second[partition])] = true
				case 3:
					subsetOf = bc7Partition3[partition]
					anchor[int(bc7Anchor3Second[partition])] = true
					anchor[int(bc7Anchor3Third[partition])] = true
				}
				var idx, idx2 [16]int
				pos := l.index
				for i := range 16 {
					n := l.indexBits
					if anchor[i] {
						n--
					}
					idx[i] = (i*5 + 3) % (1 << n)
					bc7Put(&b, pos, n, uint32(idx[i]))
					pos += n
				}
				if l.index2 >= 0 {
					pos = l.index2
					for i := range 16 {
						n := l.index2Bits
						if i == 0 {
							n--
						}
						idx2[i] = (i*3 + 1) % (1 << n)
						bc7Put(&b, pos, n, uint32(idx2[i]))
						pos += n
					}
				}
				endpoint := func(e, ch int) int {
					bits := l.colorBits
					if ch == 3 {
						if l.alpha < 0 {
							return 255
						}
						bits = l.alphaBits
					}
					v := raw(e, ch, bits)
					if l.pbit >= 0 && (ch < 3 || l.alpha >= 0) {
						return bc7TestExpand(v<<1|pbit(e), bits+1)
					}
					return bc7TestExpand(v, bits)
				}
				var got [16][4]uint8
				decodeBC7Block(b[:], &got)
				for i := range 16 {
					s := int(subsetOf[i])
					cIdx, cBits, aIdx, aBits := idx[i], l.indexBits, idx[i], l.indexBits
					if l.index2 >= 0 {
						aIdx, aBits = idx2[i], l.index2Bits
						if isb == 1 {
							cIdx, cBits, aIdx, aBits = aIdx, aBits, cIdx, cBits
						}
					}
					var want [4]uint8
					for ch := range 4 {
						w := weights[cBits][cIdx]
						if ch == 3 {
							w = weights[aBits][aIdx]
						}
						want[ch] = uint8(((64-w)*endpoint(2*s, ch) + w*endpoint(2*s+1, ch) + 32) >> 6)
					}
					if rot > 0 {
						want[3], want[rot-1] = want[rot-1], want[3]
					}
					if got[i] != want {
						t.Fatalf("texel %d (subset %d) = %v, want %v", i, s, got[i], want)
					}
				}
			})
		}
	}
}

// TestDecodeBC7Reserved checks that a block with no mode bit decodes to transparent black, as the specification requires.
func TestDecodeBC7Reserved(t *testing.T) {
	b := [16]byte{0, 0xFF, 0xFF, 0xFF}
	var got [16][4]uint8
	decodeBC7Block(b[:], &got)
	if got != [16][4]uint8{} {
		t.Fatalf("reserved block = %v, want transparent black", got)
	}
}

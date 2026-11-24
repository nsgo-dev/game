package ktx2

import (
	"image"
	"math"
)

// Pure-Go Khronos UASTC LDR 4x4 (19-mode) decoder and encoder, implementing the
// Khronos Data Format Specification §22 ("Universal ASTC Compressed Texture Images").

type uastcModeInfo struct {
	huffCode    uint8
	huffLen     uint8
	subsets     uint8
	planes      uint8
	comps       uint8 // 2 = LA, 3 = RGB, 4 = RGBA
	ebits       uint8
	wbits       uint8
	tritOrQuint uint8 // 0 = bits only, 3 = trits, 5 = quints
	hasBC1H1    bool
	hasETCBI    bool
	hasETC2     bool
	patBits     uint8
	hasCSEL     bool
}

var uastcModes = [20]uastcModeInfo{
	0:  {huffCode: 0x01, huffLen: 4, subsets: 1, planes: 1, comps: 3, ebits: 6, wbits: 4, tritOrQuint: 3, hasBC1H1: true, hasETCBI: true},
	1:  {huffCode: 0x35, huffLen: 6, subsets: 1, planes: 1, comps: 3, ebits: 8, wbits: 2, hasBC1H1: true, hasETCBI: true},
	2:  {huffCode: 0x1D, huffLen: 5, subsets: 2, planes: 1, comps: 3, ebits: 4, wbits: 3, hasBC1H1: true, hasETCBI: true, patBits: 5},
	3:  {huffCode: 0x03, huffLen: 5, subsets: 3, planes: 1, comps: 3, ebits: 2, wbits: 2, tritOrQuint: 3, hasBC1H1: true, hasETCBI: true, patBits: 4},
	4:  {huffCode: 0x13, huffLen: 5, subsets: 2, planes: 1, comps: 3, ebits: 3, wbits: 2, tritOrQuint: 5, hasBC1H1: true, hasETCBI: true, patBits: 5},
	5:  {huffCode: 0x0B, huffLen: 5, subsets: 1, planes: 1, comps: 3, ebits: 8, wbits: 3, hasBC1H1: true, hasETCBI: true},
	6:  {huffCode: 0x1B, huffLen: 5, subsets: 1, planes: 2, comps: 3, ebits: 5, wbits: 2, tritOrQuint: 5, hasBC1H1: true, hasETCBI: true, hasCSEL: true},
	7:  {huffCode: 0x07, huffLen: 5, subsets: 2, planes: 1, comps: 3, ebits: 3, wbits: 2, tritOrQuint: 5, hasBC1H1: true, hasETCBI: true, patBits: 5},
	8:  {huffCode: 0x17, huffLen: 5, subsets: 0, planes: 0, comps: 4}, // Void-Extent solid color
	9:  {huffCode: 0x0F, huffLen: 5, subsets: 2, planes: 1, comps: 4, ebits: 4, wbits: 2, hasBC1H1: true, hasETCBI: true, hasETC2: true, patBits: 5},
	10: {huffCode: 0x02, huffLen: 3, subsets: 1, planes: 1, comps: 4, ebits: 4, wbits: 4, tritOrQuint: 3, hasETC2: true},
	11: {huffCode: 0x00, huffLen: 2, subsets: 1, planes: 2, comps: 4, ebits: 4, wbits: 2, tritOrQuint: 3, hasETC2: true, hasCSEL: true},
	12: {huffCode: 0x06, huffLen: 3, subsets: 1, planes: 1, comps: 4, ebits: 6, wbits: 3, tritOrQuint: 3, hasETC2: true},
	13: {huffCode: 0x1F, huffLen: 5, subsets: 1, planes: 2, comps: 4, ebits: 8, wbits: 1, hasBC1H1: true, hasETCBI: true, hasETC2: true, hasCSEL: true},
	14: {huffCode: 0x0D, huffLen: 5, subsets: 1, planes: 1, comps: 4, ebits: 8, wbits: 2, hasBC1H1: true, hasETCBI: true, hasETC2: true},
	15: {huffCode: 0x05, huffLen: 7, subsets: 1, planes: 1, comps: 2, ebits: 8, wbits: 4, hasBC1H1: true, hasETCBI: true, hasETC2: true},
	16: {huffCode: 0x15, huffLen: 6, subsets: 2, planes: 1, comps: 2, ebits: 8, wbits: 2, hasBC1H1: true, hasETCBI: true, hasETC2: true, patBits: 5},
	17: {huffCode: 0x25, huffLen: 6, subsets: 1, planes: 2, comps: 2, ebits: 8, wbits: 2, hasBC1H1: true, hasETCBI: true, hasETC2: true},
	18: {huffCode: 0x09, huffLen: 4, subsets: 1, planes: 1, comps: 3, ebits: 5, wbits: 5, hasBC1H1: true, hasETCBI: true},
	19: {huffCode: 0x45, huffLen: 7}, // reserved future expansion -> error color
}

var uastcHuffTable [128]uint8

func init() {
	for i := range uastcHuffTable {
		uastcHuffTable[i] = 19 // invalid/reserved fallback
	}
	for mode, info := range uastcModes {
		step := 1 << info.huffLen
		for idx := int(info.huffCode); idx < 128; idx += step {
			uastcHuffTable[idx] = uint8(mode)
		}
	}
}

// Endpoint unquantization tables from Khronos Data Format Spec §22 ("Unquantizing endpoints").
var uastcUnquant3 = [12]uint8{
	0, 255, 69, 186, 23, 232, 92, 163, 46, 209, 116, 139,
}

var uastcUnquant47 = [40]uint8{
	0, 255, 32, 223, 65, 190, 97, 158, 6, 249, 39, 216, 71, 184, 104, 151, 13, 242, 45, 210,
	78, 177, 110, 145, 19, 236, 52, 203, 84, 171, 117, 138, 26, 229, 58, 197, 91, 164, 123, 132,
}

var uastcUnquant1011 = [48]uint8{
	0, 255, 16, 239, 32, 223, 48, 207, 65, 190, 81, 174, 97, 158, 113, 142,
	5, 250, 21, 234, 38, 217, 54, 201, 70, 185, 86, 169, 103, 152, 119, 136,
	11, 244, 27, 228, 43, 212, 59, 196, 76, 179, 92, 163, 108, 147, 124, 131,
}

var uastcUnquant6 = [160]uint8{
	0, 255, 8, 247, 16, 239, 24, 231, 32, 223, 40, 215, 48, 207, 56, 199,
	64, 191, 72, 183, 80, 175, 88, 167, 96, 159, 104, 151, 112, 143, 120, 135,
	1, 254, 9, 246, 17, 238, 25, 230, 33, 222, 41, 214, 49, 206, 57, 198,
	65, 190, 73, 182, 81, 174, 89, 166, 97, 158, 105, 150, 113, 142, 121, 134,
	3, 252, 11, 244, 19, 236, 27, 228, 35, 220, 43, 212, 51, 204, 59, 196,
	67, 188, 75, 180, 83, 172, 91, 164, 99, 156, 107, 148, 115, 140, 123, 132,
	4, 251, 12, 243, 20, 235, 28, 227, 36, 219, 44, 211, 52, 203, 60, 195,
	68, 187, 76, 179, 84, 171, 92, 163, 100, 155, 108, 147, 116, 139, 124, 131,
	6, 249, 14, 241, 22, 233, 30, 225, 38, 217, 46, 209, 54, 201, 62, 193,
	70, 185, 78, 177, 86, 169, 94, 161, 102, 153, 110, 145, 118, 137, 126, 129,
}

var uastcUnquant012 = [192]uint8{
	0, 255, 4, 251, 8, 247, 12, 243, 16, 239, 20, 235, 24, 231, 28, 227,
	32, 223, 36, 219, 40, 215, 44, 211, 48, 207, 52, 203, 56, 199, 60, 195,
	64, 191, 68, 187, 72, 183, 76, 179, 80, 175, 84, 171, 88, 167, 92, 163,
	96, 159, 100, 155, 104, 151, 108, 147, 112, 143, 116, 139, 120, 135, 124, 131,
	1, 254, 5, 250, 9, 246, 13, 242, 17, 238, 21, 234, 25, 230, 29, 226,
	33, 222, 37, 218, 41, 214, 45, 210, 49, 206, 53, 202, 57, 198, 61, 194,
	65, 190, 69, 186, 73, 182, 77, 178, 81, 174, 85, 170, 89, 166, 93, 162,
	97, 158, 101, 154, 105, 150, 109, 146, 113, 142, 117, 138, 121, 134, 125, 130,
	2, 253, 6, 249, 10, 245, 14, 241, 18, 237, 22, 233, 26, 229, 30, 225,
	34, 221, 38, 217, 42, 213, 46, 209, 50, 205, 54, 201, 58, 197, 62, 193,
	66, 189, 70, 185, 74, 181, 78, 177, 82, 173, 86, 169, 90, 165, 94, 161,
	98, 157, 102, 153, 106, 149, 110, 145, 114, 141, 118, 137, 122, 133, 126, 129,
}

// Weight unquantization tables (1..5 bits -> [0..64]).
var uastcWeights1 = [2]uint32{0, 64}
var uastcWeights2 = [4]uint32{0, 21, 43, 64}
var uastcWeights3 = [8]uint32{0, 9, 18, 27, 37, 46, 55, 64}
var uastcWeights4 = [16]uint32{0, 4, 8, 12, 17, 21, 25, 29, 35, 39, 43, 47, 52, 56, 60, 64}
var uastcWeights5 = [32]uint32{
	0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28, 30,
	34, 36, 38, 40, 42, 44, 46, 48, 50, 52, 54, 56, 58, 60, 62, 64,
}

// 2-subset partition table for modes 2, 4, 9, 16 (30 patterns, raster order 4x4).
var uastcPat24916 = [30][16]uint8{
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1}, // 0
	{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1}, // 1
	{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}, // 2
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 1, 1, 1}, // 3
	{1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 0, 0}, // 4
	{0, 0, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1}, // 5
	{1, 1, 1, 0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}, // 6
	{1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 0, 0, 1, 0, 0, 0}, // 7
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1}, // 8
	{1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // 9
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 1}, // 10
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0, 0}, // 11
	{1, 1, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, // 12
	{1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0}, // 13
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, // 14
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0}, // 15
	{1, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1}, // 16
	{1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 1, 0, 0, 0, 1}, // 17
	{0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0}, // 18
	{0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}, // 19
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0}, // 20
	{1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 1, 0, 0, 1, 1}, // 21
	{1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0}, // 22
	{0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0}, // 23
	{1, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 0, 1, 1}, // 24
	{0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0}, // 25
	{1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1}, // 26
	{1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0}, // 27
	{1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0}, // 28
	{1, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 0, 1, 1, 0, 0}, // 29
}

var uastcAnchors24916 = [30][2]uint8{
	{0, 2}, {0, 3}, {1, 0}, {0, 3}, {7, 0}, {0, 2}, {3, 0}, {7, 0}, {0, 11}, {2, 0},
	{0, 7}, {11, 0}, {3, 0}, {8, 0}, {0, 4}, {12, 0}, {1, 0}, {8, 0}, {0, 1}, {0, 2},
	{0, 4}, {8, 0}, {1, 0}, {0, 2}, {4, 0}, {0, 1}, {4, 0}, {1, 0}, {4, 0}, {1, 0},
}

// 2-subset partition table for mode 7 (19 patterns, raster order 4x4).
var uastcPat7 = [19][16]uint8{
	{0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0}, // 0
	{0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0}, // 1
	{1, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}, // 2
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 1}, // 3
	{1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1}, // 4
	{0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0}, // 5
	{0, 0, 0, 1, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, // 6
	{0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1}, // 7
	{1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0}, // 8
	{0, 1, 1, 1, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0}, // 9
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1, 0}, // 10
	{1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 0}, // 11
	{0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0}, // 12
	{0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1}, // 13
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 1, 0}, // 14
	{1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0}, // 15
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 1, 0, 0, 0}, // 16
	{0, 0, 1, 1, 0, 1, 1, 0, 1, 1, 0, 0, 1, 0, 0, 0}, // 17
	{1, 1, 1, 1, 0, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0}, // 18
}

var uastcAnchors7 = [19][2]uint8{
	{0, 4}, {0, 2}, {2, 0}, {0, 7}, {8, 0}, {0, 1}, {0, 3}, {0, 1}, {2, 0}, {0, 1},
	{0, 8}, {2, 0}, {0, 1}, {0, 7}, {12, 0}, {2, 0}, {9, 0}, {0, 2}, {4, 0},
}

// 3-subset partition table for mode 3 (11 patterns, raster order 4x4).
var uastcPat3 = [11][16]uint8{
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2}, // 0
	{1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 2, 2, 2, 2}, // 1
	{1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 2, 2, 2, 2}, // 2
	{1, 1, 1, 1, 2, 2, 2, 2, 0, 0, 0, 0, 0, 0, 0, 0}, // 3
	{1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2, 0}, // 4
	{0, 1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2}, // 5
	{0, 2, 1, 1, 0, 2, 1, 1, 0, 2, 1, 1, 0, 2, 1, 1}, // 6
	{2, 0, 0, 0, 2, 0, 0, 0, 2, 1, 1, 1, 2, 1, 1, 1}, // 7
	{2, 0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2}, // 8
	{1, 1, 1, 1, 0, 0, 0, 0, 2, 2, 2, 2, 1, 1, 1, 1}, // 9
	{0, 0, 2, 2, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 2, 2}, // 10
}

var uastcAnchors3 = [11][3]uint8{
	{0, 8, 10}, {8, 0, 12}, {4, 0, 12}, {8, 0, 4}, {3, 0, 2},
	{0, 1, 3}, {0, 2, 1}, {1, 9, 0}, {1, 2, 0}, {4, 0, 8}, {0, 6, 2},
}

func readBitsLE128(block []byte, bitPos, count int) uint32 {
	var v uint32
	for i := range count {
		bp := bitPos + i
		if (block[bp>>3]>>(bp&7))&1 != 0 {
			v |= 1 << i
		}
	}
	return v
}

func writeBitsLE128(block []byte, bitPos, count int, v uint32) {
	for i := range count {
		if (v>>i)&1 != 0 {
			bp := bitPos + i
			block[bp>>3] |= 1 << (bp & 7)
		}
	}
}

func replicateTo8(v uint32, bits uint8) uint8 {
	switch bits {
	case 8:
		return uint8(v)
	case 5:
		return uint8((v << 3) | (v >> 2))
	case 4:
		return uint8((v << 4) | v)
	case 3:
		return uint8((v << 5) | (v << 2) | (v >> 1))
	case 2:
		return uint8((v << 6) | (v << 4) | (v << 2) | v)
	case 1:
		if v != 0 {
			return 255
		}
		return 0
	default:
		return uint8(v)
	}
}

func uastcUnquantWeight(w uint32, wbits uint8) uint32 {
	switch wbits {
	case 1:
		return uastcWeights1[w&1]
	case 2:
		return uastcWeights2[w&3]
	case 3:
		return uastcWeights3[w&7]
	case 4:
		return uastcWeights4[w&15]
	case 5:
		return uastcWeights5[w&31]
	default:
		return 0
	}
}

func uastcInterpolate(l, h uint8, w uint32, srgb bool) uint8 {
	var cl, ch uint32
	if srgb {
		cl = (uint32(l) << 8) | 0x80
		ch = (uint32(h) << 8) | 0x80
	} else {
		cl = (uint32(l) << 8) | uint32(l)
		ch = (uint32(h) << 8) | uint32(h)
	}
	return uint8(((cl*(64-w) + ch*w + 32) >> 6) >> 8)
}

func fillUASTCError(out *[16][4]uint8) {
	for i := range 16 {
		out[i] = [4]uint8{255, 0, 255, 255}
	}
}

// decodeUASTCBlock decodes one 16-byte Khronos UASTC 4x4 block into 16 RGBA pixels (raster order).
func decodeUASTCBlock(block []byte, srgb bool, out *[16][4]uint8) {
	if len(block) < 16 {
		fillUASTCError(out)
		return
	}
	mode := uastcHuffTable[block[0]&0x7F]
	if mode >= 19 {
		fillUASTCError(out)
		return
	}

	if mode == 8 {
		r := uint8(readBitsLE128(block, 5, 8))
		g := uint8(readBitsLE128(block, 13, 8))
		b := uint8(readBitsLE128(block, 21, 8))
		a := uint8(readBitsLE128(block, 29, 8))
		for i := range 16 {
			out[i] = [4]uint8{r, g, b, a}
		}
		return
	}

	info := uastcModes[mode]
	pos := int(info.huffLen)

	// Skip transcoding hints: BC1H0(1), [BC1H1(1)], ETC1F(1), ETC1D(1), ETCI0(3), ETCI1(3), [ETCBI(5)], [ETC2T(4), ETC2M(4)]
	pos += 1 // BC1H0
	if info.hasBC1H1 {
		pos += 1
	}
	pos += 1 + 1 + 3 + 3 // ETC1F, ETC1D, ETCI0, ETCI1
	if info.hasETCBI {
		pos += 5
	}
	if info.hasETC2 {
		pos += 8
	}

	var pat uint32
	if info.patBits > 0 {
		pat = readBitsLE128(block, pos, int(info.patBits))
		pos += int(info.patBits)
	}

	var csel uint32
	if info.hasCSEL {
		csel = readBitsLE128(block, pos, 2)
		pos += 2
	} else if mode == 17 {
		csel = 3 // Mode 17 always uses plane 1 for Alpha
	}

	// Validate partition index and resolve per-pixel subset + anchor mask.
	var subsetMap [16]uint8
	var isAnchor [16]bool
	switch info.subsets {
	case 1:
		isAnchor[0] = true
	case 2:
		if mode == 7 {
			if pat >= uint32(len(uastcPat7)) {
				fillUASTCError(out)
				return
			}
			subsetMap = uastcPat7[pat]
			isAnchor[uastcAnchors7[pat][0]] = true
			isAnchor[uastcAnchors7[pat][1]] = true
		} else {
			if pat >= uint32(len(uastcPat24916)) {
				fillUASTCError(out)
				return
			}
			subsetMap = uastcPat24916[pat]
			isAnchor[uastcAnchors24916[pat][0]] = true
			isAnchor[uastcAnchors24916[pat][1]] = true
		}
	case 3:
		if pat >= uint32(len(uastcPat3)) {
			fillUASTCError(out)
			return
		}
		subsetMap = uastcPat3[pat]
		isAnchor[uastcAnchors3[pat][0]] = true
		isAnchor[uastcAnchors3[pat][1]] = true
		isAnchor[uastcAnchors3[pat][2]] = true
	}

	numEndpoints := int(info.subsets) * int(info.comps) * 2
	var tq [18]uint32

	switch info.tritOrQuint {
	case 3:
		// Unpack trits (5 per 8-bit ET, trailing 1 -> 2 bits or 3 -> 5 bits).
		idx := 0
		for idx < numEndpoints {
			rem := numEndpoints - idx
			var bits int
			var count int
			switch {
			case rem >= 5:
				bits, count = 8, 5
			case rem == 3:
				bits, count = 5, 3
			case rem == 1:
				bits, count = 2, 1
			default:
				bits, count = 8, rem
			}
			et := readBitsLE128(block, pos, bits)
			pos += bits
			if et >= 243 {
				et -= 243
			}
			for range count {
				tq[idx] = et % 3
				et /= 3
				idx++
			}
		}
	case 5:
		// Unpack quints (3 per 7-bit EQ).
		idx := 0
		for idx < numEndpoints {
			eq := readBitsLE128(block, pos, 7)
			pos += 7
			if eq >= 125 {
				eq -= 125
			}
			for range 3 {
				if idx < numEndpoints {
					tq[idx] = eq % 5
					eq /= 5
					idx++
				}
			}
		}
	}

	// Read and unquantize endpoint values.
	var epVal [18]uint8
	for i := range numEndpoints {
		eb := readBitsLE128(block, pos, int(info.ebits))
		pos += int(info.ebits)
		if info.tritOrQuint == 0 {
			epVal[i] = replicateTo8(eb, info.ebits)
		} else {
			key := (tq[i] << info.ebits) | eb
			switch mode {
			case 3:
				epVal[i] = uastcUnquant3[key]
			case 4, 7:
				epVal[i] = uastcUnquant47[key]
			case 10, 11:
				epVal[i] = uastcUnquant1011[key]
			case 6:
				epVal[i] = uastcUnquant6[key]
			case 0, 12:
				epVal[i] = uastcUnquant012[key]
			}
		}
	}

	// Expand endpoints into [subset][low/high][RGBA].
	var ep [3][2][4]uint8
	eIdx := 0
	for s := range int(info.subsets) {
		switch info.comps {
		case 3: // RGB, A = 255
			ep[s][0] = [4]uint8{epVal[eIdx+0], epVal[eIdx+2], epVal[eIdx+4], 255}
			ep[s][1] = [4]uint8{epVal[eIdx+1], epVal[eIdx+3], epVal[eIdx+5], 255}
			eIdx += 6
		case 4: // RGBA
			ep[s][0] = [4]uint8{epVal[eIdx+0], epVal[eIdx+2], epVal[eIdx+4], epVal[eIdx+6]}
			ep[s][1] = [4]uint8{epVal[eIdx+1], epVal[eIdx+3], epVal[eIdx+5], epVal[eIdx+7]}
			eIdx += 8
		case 2: // LA -> R=G=B=L, A=A
			l0, l1 := epVal[eIdx+0], epVal[eIdx+1]
			a0, a1 := epVal[eIdx+2], epVal[eIdx+3]
			ep[s][0] = [4]uint8{l0, l0, l0, a0}
			ep[s][1] = [4]uint8{l1, l1, l1, a1}
			eIdx += 4
		}
	}

	// Read and unquantize weights.
	var w0 [16]uint32
	var w1 [16]uint32
	for i := range 16 {
		wb := int(info.wbits)
		if isAnchor[i] {
			wb--
		}
		rawW0 := uint32(0)
		if wb > 0 {
			rawW0 = readBitsLE128(block, pos, wb)
			pos += wb
		}
		w0[i] = uastcUnquantWeight(rawW0, info.wbits)
		if info.planes == 2 {
			rawW1 := uint32(0)
			if wb > 0 {
				rawW1 = readBitsLE128(block, pos, wb)
				pos += wb
			}
			w1[i] = uastcUnquantWeight(rawW1, info.wbits)
		}
	}

	// Interpolate pixels.
	for i := range 16 {
		s := subsetMap[i]
		lo := ep[s][0]
		hi := ep[s][1]
		for ch := range 4 {
			w := w0[i]
			if info.planes == 2 && uint32(ch) == csel {
				w = w1[i]
			}
			out[i][ch] = uastcInterpolate(lo[ch], hi[ch], w, srgb)
		}
	}
}

func decodeUASTC(raw []byte, width, height int, srgb bool) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	bw := (width + 3) / 4
	bh := (height + 3) / 4
	var blockPix [16][4]uint8
	for by := range bh {
		for bx := range bw {
			off := (by*bw + bx) * 16
			if off+16 > len(raw) {
				break
			}
			decodeUASTCBlock(raw[off:off+16], srgb, &blockPix)
			for dy := range 4 {
				py := by*4 + dy
				if py >= height {
					continue
				}
				rowOff := py * dst.Stride
				for dx := range 4 {
					px := bx*4 + dx
					if px >= width {
						continue
					}
					c := blockPix[dy*4+dx]
					di := rowOff + px*4
					dst.Pix[di+0] = c[0]
					dst.Pix[di+1] = c[1]
					dst.Pix[di+2] = c[2]
					dst.Pix[di+3] = c[3]
				}
			}
		}
	}
	return dst
}

// encodeUASTC compresses img into Khronos-spec UASTC 4x4 blocks (16 bytes per 4x4 block).
// Solid blocks use Mode 8 (lossless 8-bit RGBA); opaque blocks select the lowest-error
// encoding between Mode 5 (8-bit endpoints, 3-bit weights) and Mode 18 (5-bit endpoints,
// 5-bit weights); blocks with non-opaque alpha use Mode 14 (8-bit RGBA endpoints, 2-bit
// weights) with endpoint refinement.
func encodeUASTC(img *image.NRGBA, srgb bool) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	out := make([]byte, bw*bh*16)
	for by := range bh {
		for bx := range bw {
			pix := gatherBlock4x4(img, bx, by)
			off := (by*bw + bx) * 16
			encodeUASTCBlock(out[off:off+16], &pix, srgb)
		}
	}
	return out
}

func encodeUASTCBlock(dst []byte, pix *[16][4]uint8, srgb bool) {
	// Check solid block -> Mode 8 (exact lossless 8-bit RGBA).
	solid := true
	opaque := true
	for i := range 16 {
		if pix[i] != pix[0] {
			solid = false
		}
		if pix[i][3] != 255 {
			opaque = false
		}
	}
	if solid {
		clear(dst[:16])
		writeBitsLE128(dst, 0, 5, uint32(uastcModes[8].huffCode))
		writeBitsLE128(dst, 5, 8, uint32(pix[0][0]))
		writeBitsLE128(dst, 13, 8, uint32(pix[0][1]))
		writeBitsLE128(dst, 21, 8, uint32(pix[0][2]))
		writeBitsLE128(dst, 29, 8, uint32(pix[0][3]))
		return
	}

	if opaque {
		var blk5, blk18 [16]byte
		err5 := encodeUASTCMode5(&blk5, pix, srgb)
		err18 := encodeUASTCMode18(&blk18, pix, srgb)
		if err5 <= err18 {
			copy(dst[:16], blk5[:])
		} else {
			copy(dst[:16], blk18[:])
		}
		return
	}

	var blk14 [16]byte
	encodeUASTCMode14(&blk14, pix, srgb)
	copy(dst[:16], blk14[:])
}

// fitBlockEndpoints4D finds initial RGBA endpoints along the principal axis of a 4x4 block.
func fitBlockEndpoints4D(pix *[16][4]uint8, includeAlpha bool) (lo, hi [4]uint8) {
	var mean [4]float64
	var minV = [4]float64{255, 255, 255, 255}
	var maxV = [4]float64{0, 0, 0, 0}
	dims := 3
	if includeAlpha {
		dims = 4
	}
	for i := range 16 {
		for c := range dims {
			v := float64(pix[i][c])
			mean[c] += v
			if v < minV[c] {
				minV[c] = v
			}
			if v > maxV[c] {
				maxV[c] = v
			}
		}
	}
	for c := range dims {
		mean[c] /= 16.0
	}

	var cov [4][4]float64
	for i := range 16 {
		var d [4]float64
		for c := range dims {
			d[c] = float64(pix[i][c]) - mean[c]
		}
		for r := range dims {
			for c := range dims {
				cov[r][c] += d[r] * d[c]
			}
		}
	}

	var axis [4]float64
	for c := range dims {
		axis[c] = maxV[c] - minV[c]
	}
	for range 4 {
		var next [4]float64
		var mag2 float64
		for r := range dims {
			var s float64
			for c := range dims {
				s += cov[r][c] * axis[c]
			}
			next[r] = s
			mag2 += s * s
		}
		if mag2 < 1e-12 {
			break
		}
		inv := 1.0 / math.Sqrt(mag2)
		for c := range dims {
			axis[c] = next[c] * inv
		}
	}

	minP, maxP := math.Inf(1), math.Inf(-1)
	minIdx, maxIdx := 0, 0
	for i := range 16 {
		var p float64
		for c := range dims {
			p += (float64(pix[i][c]) - mean[c]) * axis[c]
		}
		if p < minP {
			minP = p
			minIdx = i
		}
		if p > maxP {
			maxP = p
			maxIdx = i
		}
	}
	lo = pix[minIdx]
	hi = pix[maxIdx]
	if !includeAlpha {
		lo[3] = 255
		hi[3] = 255
	}
	return lo, hi
}

func refineEndpointsLinear(pix *[16][4]uint8, weights []uint32, indices *[16]uint8, includeAlpha bool) (lo, hi [4]uint8, ok bool) {
	dims := 3
	if includeAlpha {
		dims = 4
	}
	var a00, a01, a11 float64
	var b0, b1 [4]float64
	for i := range 16 {
		w := float64(weights[indices[i]]) / 64.0
		u := 1.0 - w
		a00 += u * u
		a01 += u * w
		a11 += w * w
		for c := range dims {
			val := float64(pix[i][c])
			b0[c] += u * val
			b1[c] += w * val
		}
	}
	det := a00*a11 - a01*a01
	if math.Abs(det) < 1e-6 {
		return lo, hi, false
	}
	inv := 1.0 / det
	lo[3], hi[3] = 255, 255
	for c := range dims {
		v0 := (a11*b0[c] - a01*b1[c]) * inv
		v1 := (a00*b1[c] - a01*b0[c]) * inv
		lo[c] = clampByte(int(math.Round(v0)))
		hi[c] = clampByte(int(math.Round(v1)))
	}
	return lo, hi, true
}

func matchWeightsUASTC(pix *[16][4]uint8, lo, hi [4]uint8, weights []uint32, includeAlpha, srgb bool) (indices [16]uint8, totalErr int) {
	dims := 3
	if includeAlpha {
		dims = 4
	}
	var pal [32][4]uint8
	for k, w := range weights {
		for c := range dims {
			pal[k][c] = uastcInterpolate(lo[c], hi[c], w, srgb)
		}
	}
	for i := range 16 {
		bestK := uint8(0)
		bestD := math.MaxInt
		for k := range weights {
			d := 0
			for c := range dims {
				diff := int(pix[i][c]) - int(pal[k][c])
				d += diff * diff
			}
			if d < bestD {
				bestD = d
				bestK = uint8(k)
			}
		}
		indices[i] = bestK
		totalErr += bestD
	}
	return indices, totalErr
}

func encodeUASTCMode5(dst *[16]byte, pix *[16][4]uint8, srgb bool) int {
	lo, hi := fitBlockEndpoints4D(pix, false)
	weights := uastcWeights3[:]
	indices, bestErr := matchWeightsUASTC(pix, lo, hi, weights, false, srgb)
	for range 2 {
		nlo, nhi, ok := refineEndpointsLinear(pix, weights, &indices, false)
		if !ok {
			break
		}
		nIdx, nErr := matchWeightsUASTC(pix, nlo, nhi, weights, false, srgb)
		if nErr < bestErr {
			lo, hi, indices, bestErr = nlo, nhi, nIdx, nErr
		} else {
			break
		}
	}
	// Ensure anchor texel 0 has MSB == 0 (for 3-bit weights, indices[0] < 4).
	if indices[0] >= 4 {
		lo, hi = hi, lo
		for i := range 16 {
			indices[i] = 7 - indices[i]
		}
	}
	clear(dst[:])
	// Mode 5: huff 0x0B (5b), config 15b (pos=20), 6x8b RGB endpoints (pos=68), weights (2b + 15*3b = 47b)
	writeBitsLE128(dst[:], 0, 5, uint32(uastcModes[5].huffCode))
	pos := 20
	for _, v := range [6]uint8{lo[0], hi[0], lo[1], hi[1], lo[2], hi[2]} {
		writeBitsLE128(dst[:], pos, 8, uint32(v))
		pos += 8
	}
	writeBitsLE128(dst[:], pos, 2, uint32(indices[0]))
	pos += 2
	for i := 1; i < 16; i++ {
		writeBitsLE128(dst[:], pos, 3, uint32(indices[i]))
		pos += 3
	}
	return bestErr
}

func encodeUASTCMode18(dst *[16]byte, pix *[16][4]uint8, srgb bool) int {
	lo, hi := fitBlockEndpoints4D(pix, false)
	quant5Ep := func(c [4]uint8) (q [3]uint32, exp [4]uint8) {
		for ch := range 3 {
			qv := uint32((int(c[ch])*31 + 127) / 255)
			q[ch] = qv
			exp[ch] = replicateTo8(qv, 5)
		}
		exp[3] = 255
		return q, exp
	}
	qlo, elo := quant5Ep(lo)
	qhi, ehi := quant5Ep(hi)
	weights := uastcWeights5[:]
	indices, bestErr := matchWeightsUASTC(pix, elo, ehi, weights, false, srgb)
	for range 2 {
		nlo, nhi, ok := refineEndpointsLinear(pix, weights, &indices, false)
		if !ok {
			break
		}
		nqlo, nelo := quant5Ep(nlo)
		nqhi, nehi := quant5Ep(nhi)
		nIdx, nErr := matchWeightsUASTC(pix, nelo, nehi, weights, false, srgb)
		if nErr < bestErr {
			qlo, qhi, indices, bestErr = nqlo, nqhi, nIdx, nErr
		} else {
			break
		}
	}
	// Ensure anchor texel 0 has MSB == 0 (for 5-bit weights, indices[0] < 16).
	if indices[0] >= 16 {
		qlo, qhi = qhi, qlo
		for i := range 16 {
			indices[i] = 31 - indices[i]
		}
	}
	clear(dst[:])
	// Mode 18: huff 0x09 (4b), config 15b (pos=19), 6x5b RGB endpoints (pos=49), weights (4b + 15*5b = 79b)
	writeBitsLE128(dst[:], 0, 4, uint32(uastcModes[18].huffCode))
	pos := 19
	for _, v := range [6]uint32{qlo[0], qhi[0], qlo[1], qhi[1], qlo[2], qhi[2]} {
		writeBitsLE128(dst[:], pos, 5, v)
		pos += 5
	}
	writeBitsLE128(dst[:], pos, 4, uint32(indices[0]))
	pos += 4
	for i := 1; i < 16; i++ {
		writeBitsLE128(dst[:], pos, 5, uint32(indices[i]))
		pos += 5
	}
	return bestErr
}

func encodeUASTCMode14(dst *[16]byte, pix *[16][4]uint8, srgb bool) int {
	lo, hi := fitBlockEndpoints4D(pix, true)
	weights := uastcWeights2[:]
	indices, bestErr := matchWeightsUASTC(pix, lo, hi, weights, true, srgb)
	for range 2 {
		nlo, nhi, ok := refineEndpointsLinear(pix, weights, &indices, true)
		if !ok {
			break
		}
		nIdx, nErr := matchWeightsUASTC(pix, nlo, nhi, weights, true, srgb)
		if nErr < bestErr {
			lo, hi, indices, bestErr = nlo, nhi, nIdx, nErr
		} else {
			break
		}
	}
	// Ensure anchor texel 0 has MSB == 0 (for 2-bit weights, indices[0] < 2).
	if indices[0] >= 2 {
		lo, hi = hi, lo
		for i := range 16 {
			indices[i] = 3 - indices[i]
		}
	}
	clear(dst[:])
	// Mode 14: huff 0x0D (5b), config 23b (pos=28), 8x8b RGBA endpoints (pos=92), weights (1b + 15*2b = 31b)
	writeBitsLE128(dst[:], 0, 5, uint32(uastcModes[14].huffCode))
	pos := 28
	for _, v := range [8]uint8{lo[0], hi[0], lo[1], hi[1], lo[2], hi[2], lo[3], hi[3]} {
		writeBitsLE128(dst[:], pos, 8, uint32(v))
		pos += 8
	}
	writeBitsLE128(dst[:], pos, 1, uint32(indices[0]))
	pos += 1
	for i := 1; i < 16; i++ {
		writeBitsLE128(dst[:], pos, 2, uint32(indices[i]))
		pos += 2
	}
	return bestErr
}

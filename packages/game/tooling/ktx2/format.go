// Package ktx2 provides a pure-Go KTX2 v2.0 texture container encoder, decoder,
// mipmap chain generator, and block texture compressor (BC1, BC3, ETC2 RGB8, RGBA8).
package ktx2

import (
	"fmt"
	"strings"
)

// Identifier is the 12-byte KTX2 v2.0 magic file signature: "\xABKTX 20\xBB\r\n\x1A\n".
var Identifier = [12]byte{0xAB, 0x4B, 0x54, 0x58, 0x20, 0x32, 0x30, 0xBB, 0x0D, 0x0A, 0x1A, 0x0A}

// HeaderSize is the fixed size in bytes of the KTX2 identifier + header + index section.
const HeaderSize = 80

// LevelIndexEntrySize is the byte size of one entry in the KTX2 level index array.
const LevelIndexEntrySize = 24

// Vulkan VkFormat constants used by this package.
const (
	VkFormatR8G8B8A8Unorm      uint32 = 37
	VkFormatR8G8B8A8SRGB       uint32 = 43
	VkFormatBC1RGBAUnormBlock  uint32 = 133
	VkFormatBC1RGBASRGBBlock   uint32 = 134
	VkFormatBC3UnormBlock      uint32 = 137
	VkFormatBC3SRGBBlock       uint32 = 138
	VkFormatETC2R8G8B8UnormBlk uint32 = 147
	VkFormatETC2R8G8B8SRGBBlk  uint32 = 148
)

// Khronos Data Format Descriptor (DFD) v1.3 constants.
const (
	DFDVendorKhronos          uint32 = 0
	DFDDescriptorBasicFormat  uint32 = 0
	DFDVersionNumber          uint32 = 2
	DFDModelRGBSDA            uint8  = 1
	DFDModelBC1A              uint8  = 128
	DFDModelBC3               uint8  = 130
	DFDModelETC2              uint8  = 161
	DFDPrimariesBT709         uint8  = 1
	DFDTransferLinear         uint8  = 1
	DFDTransferSRGB           uint8  = 2
	DFDChannelRGBSDARed       uint8  = 0
	DFDChannelRGBSDAGreen     uint8  = 1
	DFDChannelRGBSDABlue      uint8  = 2
	DFDChannelRGBSDAAlpha     uint8  = 15
	DFDChannelBC1AColor       uint8  = 0
	DFDChannelBC3Color        uint8  = 0
	DFDChannelBC3Alpha        uint8  = 15
	DFDChannelETC2Color       uint8  = 0
	SupercompressionNone      uint32 = 0
	SupercompressionBasisLZ   uint32 = 1
	SupercompressionZstandard uint32 = 2
)

// Format identifies a supported KTX2 pixel or block-compressed texture format.
type Format string

const (
	FormatRGBA8 Format = "rgba8"
	FormatBC1   Format = "bc1"
	FormatBC3   Format = "bc3"
	FormatETC2  Format = "etc2"
)

// ParseFormat parses a CLI or configuration format name (rgba8, bc1/dxt1, bc3/dxt5, etc2).
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "rgba8", "rgba":
		return FormatRGBA8, nil
	case "bc1", "dxt1":
		return FormatBC1, nil
	case "bc3", "dxt5":
		return FormatBC3, nil
	case "etc2", "etc2_rgb8", "etc2-rgb8":
		return FormatETC2, nil
	default:
		return "", fmt.Errorf("ktx2: unsupported format %q (want rgba8, bc1, bc3, or etc2)", s)
	}
}

// String returns the canonical format name.
func (f Format) String() string {
	if f == "" {
		return string(FormatRGBA8)
	}
	return string(f)
}

// VkFormat returns the Vulkan VkFormat ID for f and the requested sRGB color space.
func (f Format) VkFormat(srgb bool) (uint32, error) {
	switch f {
	case "", FormatRGBA8:
		if srgb {
			return VkFormatR8G8B8A8SRGB, nil
		}
		return VkFormatR8G8B8A8Unorm, nil
	case FormatBC1:
		if srgb {
			return VkFormatBC1RGBASRGBBlock, nil
		}
		return VkFormatBC1RGBAUnormBlock, nil
	case FormatBC3:
		if srgb {
			return VkFormatBC3SRGBBlock, nil
		}
		return VkFormatBC3UnormBlock, nil
	case FormatETC2:
		if srgb {
			return VkFormatETC2R8G8B8SRGBBlk, nil
		}
		return VkFormatETC2R8G8B8UnormBlk, nil
	default:
		return 0, fmt.Errorf("ktx2: unknown format %q", f)
	}
}

// FormatFromVk maps a supported Vulkan VkFormat ID back to (Format, srgb, ok).
func FormatFromVk(vk uint32) (Format, bool, bool) {
	switch vk {
	case VkFormatR8G8B8A8Unorm:
		return FormatRGBA8, false, true
	case VkFormatR8G8B8A8SRGB:
		return FormatRGBA8, true, true
	case VkFormatBC1RGBAUnormBlock:
		return FormatBC1, false, true
	case VkFormatBC1RGBASRGBBlock:
		return FormatBC1, true, true
	case VkFormatBC3UnormBlock:
		return FormatBC3, false, true
	case VkFormatBC3SRGBBlock:
		return FormatBC3, true, true
	case VkFormatETC2R8G8B8UnormBlk:
		return FormatETC2, false, true
	case VkFormatETC2R8G8B8SRGBBlk:
		return FormatETC2, true, true
	default:
		return "", false, false
	}
}

func (f Format) blockDim() int {
	switch f {
	case FormatBC1, FormatBC3, FormatETC2:
		return 4
	default:
		return 1
	}
}

func (f Format) blockBytes() int {
	switch f {
	case FormatBC1, FormatETC2:
		return 8
	case FormatBC3:
		return 16
	default:
		return 4
	}
}

// levelAlignment returns lcm(texelBlockSize, 4) as required by KTX2 spec §3.6.
func (f Format) levelAlignment() uint64 {
	b := uint64(f.blockBytes())
	return lcm(b, 4)
}

func (f Format) levelByteSize(w, h int) int {
	bd := f.blockDim()
	bw := (w + bd - 1) / bd
	bh := (h + bd - 1) / bd
	return bw * bh * f.blockBytes()
}

func gcd(a, b uint64) uint64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func lcm(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	return (a / gcd(a, b)) * b
}

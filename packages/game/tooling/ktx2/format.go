// Package ktx2 provides a pure-Go KTX2 v2.0 texture container encoder, decoder,
// supercompression decoder (Zstandard, ZLIB), mipmap chain generator, Basis
// Universal UASTC 4x4 codec, and block texture transcoder (BC1, BC3, BC7, ETC2
// RGB8/RGBA8, ASTC 4x4, RGBA8).
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
	VkFormatUndefined            uint32 = 0
	VkFormatR8G8B8A8Unorm        uint32 = 37
	VkFormatR8G8B8A8SRGB         uint32 = 43
	VkFormatBC1RGBUnormBlock     uint32 = 131
	VkFormatBC1RGBSRGBBlock      uint32 = 132
	VkFormatBC1RGBAUnormBlock    uint32 = 133
	VkFormatBC1RGBASRGBBlock     uint32 = 134
	VkFormatBC3UnormBlock        uint32 = 137
	VkFormatBC3SRGBBlock         uint32 = 138
	VkFormatBC7UnormBlock        uint32 = 145
	VkFormatBC7SRGBBlock         uint32 = 146
	VkFormatETC2R8G8B8UnormBlk   uint32 = 147
	VkFormatETC2R8G8B8SRGBBlk    uint32 = 148
	VkFormatETC2R8G8B8A8UnormBlk uint32 = 151
	VkFormatETC2R8G8B8A8SRGBBlk  uint32 = 152
	VkFormatASTC4x4UnormBlock    uint32 = 157
	VkFormatASTC4x4SRGBBlock     uint32 = 158
)

// Khronos Data Format Descriptor (DFD) v1.3 constants.
const (
	DFDVendorKhronos          uint32 = 0
	DFDDescriptorBasicFormat  uint32 = 0
	DFDVersionNumber          uint32 = 2
	DFDModelRGBSDA            uint8  = 1
	DFDModelBC1A              uint8  = 128
	DFDModelBC3               uint8  = 130
	DFDModelBC7               uint8  = 134
	DFDModelETC2              uint8  = 161
	DFDModelASTC              uint8  = 162
	DFDModelETC1S             uint8  = 163
	DFDModelUASTC             uint8  = 166
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
	DFDChannelBC7Color        uint8  = 0
	DFDChannelETC2Color       uint8  = 0
	DFDChannelETC2Alpha       uint8  = 15
	DFDChannelASTCData        uint8  = 0
	DFDChannelETC1SRGB        uint8  = 0
	DFDChannelETC1SRRR        uint8  = 3
	DFDChannelETC1SGGG        uint8  = 4
	DFDChannelETC1SAAA        uint8  = 15
	DFDChannelUASTCRGB        uint8  = 0
	DFDChannelUASTCRGBA       uint8  = 3
	DFDChannelUASTCRRR        uint8  = 4
	DFDChannelUASTCRRRG       uint8  = 5
	SupercompressionNone      uint32 = 0
	SupercompressionBasisLZ   uint32 = 1
	SupercompressionZstandard uint32 = 2
	SupercompressionZLIB      uint32 = 3
)

// Format identifies a supported KTX2 pixel, universal, or block-compressed texture format.
type Format string

const (
	FormatRGBA8    Format = "rgba8"
	FormatBC1      Format = "bc1"
	FormatBC3      Format = "bc3"
	FormatBC7      Format = "bc7"
	FormatETC2     Format = "etc2"
	FormatETC2RGBA Format = "etc2-rgba"
	FormatASTC4x4  Format = "astc4x4"
	FormatUASTC    Format = "uastc"
	FormatETC1S    Format = "etc1s"
)

// ParseFormat parses a CLI or configuration format name (rgba8, bc1/dxt1, bc3/dxt5,
// bc7, etc2, etc2-rgba, astc4x4, or uastc).
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "rgba8", "rgba":
		return FormatRGBA8, nil
	case "bc1", "dxt1":
		return FormatBC1, nil
	case "bc3", "dxt5":
		return FormatBC3, nil
	case "bc7":
		return FormatBC7, nil
	case "etc2", "etc2_rgb8", "etc2-rgb8":
		return FormatETC2, nil
	case "etc2-rgba", "etc2_rgba", "etc2_rgba8", "etc2-rgba8":
		return FormatETC2RGBA, nil
	case "astc4x4", "astc-4x4", "astc_4x4":
		return FormatASTC4x4, nil
	case "uastc":
		return FormatUASTC, nil
	default:
		return "", fmt.Errorf("ktx2: unsupported format %q (want rgba8, bc1, bc3, bc7, etc2, etc2-rgba, astc4x4, or uastc)", s)
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
// Universal formats (FormatUASTC, FormatETC1S) use VkFormatUndefined (0) per KTX2 v2.0,
// with the pixel representation described in the DFD.
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
	case FormatBC7:
		if srgb {
			return VkFormatBC7SRGBBlock, nil
		}
		return VkFormatBC7UnormBlock, nil
	case FormatETC2:
		if srgb {
			return VkFormatETC2R8G8B8SRGBBlk, nil
		}
		return VkFormatETC2R8G8B8UnormBlk, nil
	case FormatETC2RGBA:
		if srgb {
			return VkFormatETC2R8G8B8A8SRGBBlk, nil
		}
		return VkFormatETC2R8G8B8A8UnormBlk, nil
	case FormatASTC4x4:
		if srgb {
			return VkFormatASTC4x4SRGBBlock, nil
		}
		return VkFormatASTC4x4UnormBlock, nil
	case FormatUASTC, FormatETC1S:
		return VkFormatUndefined, nil
	default:
		return 0, fmt.Errorf("ktx2: unknown format %q", f)
	}
}

// FormatFromVk maps a supported Vulkan VkFormat ID back to (Format, srgb, ok).
// For VkFormatUndefined (0), use [FormatFromHeaderDFD] to resolve Basis Universal
// UASTC or ETC1S from the Data Format Descriptor.
func FormatFromVk(vk uint32) (Format, bool, bool) {
	switch vk {
	case VkFormatR8G8B8A8Unorm:
		return FormatRGBA8, false, true
	case VkFormatR8G8B8A8SRGB:
		return FormatRGBA8, true, true
	case VkFormatBC1RGBUnormBlock, VkFormatBC1RGBAUnormBlock:
		return FormatBC1, false, true
	case VkFormatBC1RGBSRGBBlock, VkFormatBC1RGBASRGBBlock:
		return FormatBC1, true, true
	case VkFormatBC3UnormBlock:
		return FormatBC3, false, true
	case VkFormatBC3SRGBBlock:
		return FormatBC3, true, true
	case VkFormatBC7UnormBlock:
		return FormatBC7, false, true
	case VkFormatBC7SRGBBlock:
		return FormatBC7, true, true
	case VkFormatETC2R8G8B8UnormBlk:
		return FormatETC2, false, true
	case VkFormatETC2R8G8B8SRGBBlk:
		return FormatETC2, true, true
	case VkFormatETC2R8G8B8A8UnormBlk:
		return FormatETC2RGBA, false, true
	case VkFormatETC2R8G8B8A8SRGBBlk:
		return FormatETC2RGBA, true, true
	case VkFormatASTC4x4UnormBlock:
		return FormatASTC4x4, false, true
	case VkFormatASTC4x4SRGBBlock:
		return FormatASTC4x4, true, true
	default:
		return "", false, false
	}
}

// FormatFromHeaderDFD resolves the texture format, sRGB transfer flag, and alpha presence
// from a KTX2 header and parsed Data Format Descriptor, including Basis Universal UASTC
// and ETC1S containers that set Header.VkFormat == 0.
func FormatFromHeaderDFD(h Header, dfd DFD) (format Format, srgb bool, hasAlpha bool, ok bool) {
	if h.VkFormat != VkFormatUndefined {
		f, isSRGB, known := FormatFromVk(h.VkFormat)
		if !known {
			return "", false, false, false
		}
		alpha := f.HasAlpha()
		if h.VkFormat == VkFormatBC1RGBUnormBlock || h.VkFormat == VkFormatBC1RGBSRGBBlock {
			alpha = false
		}
		return f, isSRGB, alpha, true
	}

	isSRGB := dfd.TransferFunction == DFDTransferSRGB
	switch dfd.ColorModel {
	case DFDModelUASTC:
		alpha := true
		if len(dfd.Samples) > 0 {
			ch := dfd.Samples[0].ChannelType & 0x0F
			if ch == DFDChannelUASTCRGB || ch == DFDChannelUASTCRRR {
				alpha = false
			}
		}
		return FormatUASTC, isSRGB, alpha, true
	case DFDModelETC1S:
		alpha := len(dfd.Samples) > 1
		for _, s := range dfd.Samples {
			if (s.ChannelType & 0x0F) == DFDChannelETC1SAAA {
				alpha = true
			}
		}
		return FormatETC1S, isSRGB, alpha, true
	default:
		return "", false, false, false
	}
}

// IsBlockCompressed reports whether f stores 4x4 texel blocks rather than uncompressed pixels.
func (f Format) IsBlockCompressed() bool {
	return f.BlockDim() > 1
}

// HasAlpha reports whether f carries an alpha channel.
func (f Format) HasAlpha() bool {
	switch f {
	case FormatETC2:
		return false
	default:
		return true
	}
}

// BlockDim returns the square texel block dimension (4 for block formats, 1 for RGBA8).
func (f Format) BlockDim() int {
	switch f {
	case FormatBC1, FormatBC3, FormatBC7, FormatETC2, FormatETC2RGBA, FormatASTC4x4, FormatUASTC, FormatETC1S:
		return 4
	default:
		return 1
	}
}

// BlockBytes returns the byte size of one texel block (4 for RGBA8, 8 for BC1/ETC2, 16 for BC3/BC7/ETC2RGBA/ASTC4x4/UASTC).
func (f Format) BlockBytes() int {
	switch f {
	case FormatBC1, FormatETC2:
		return 8
	case FormatBC3, FormatBC7, FormatETC2RGBA, FormatASTC4x4, FormatUASTC:
		return 16
	default:
		return 4
	}
}

func (f Format) blockDim() int   { return f.BlockDim() }
func (f Format) blockBytes() int { return f.BlockBytes() }

// levelAlignment returns lcm(texelBlockSize, 4) as required by KTX2 spec §3.6.
func (f Format) levelAlignment() uint64 {
	b := uint64(f.BlockBytes())
	return lcm(b, 4)
}

// LevelByteSize returns the exact uncompressed byte size of a w x h mip level encoded in f.
func (f Format) LevelByteSize(w, h int) int {
	if w <= 0 || h <= 0 {
		return 0
	}
	bd := f.BlockDim()
	bw := (w + bd - 1) / bd
	bh := (h + bd - 1) / bd
	return bw * bh * f.BlockBytes()
}

func (f Format) levelByteSize(w, h int) int { return f.LevelByteSize(w, h) }

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

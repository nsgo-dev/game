package ktx2

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

// Safety bounds enforced when decoding untrusted KTX2 containers. maxUncompressedLevelBytes bounds every decoded level, both its de-supercompressed payload and the RGBA image DecodeLevelRGBA builds from it, so a few kilobytes of Zstandard RLE blocks cannot make the decoder allocate gigabytes.
const (
	maxTextureDimension       = 16384
	maxLevelCount             = 16
	maxUncompressedLevelBytes = 256 * 1024 * 1024
)

// kvdBinaryKeys are the keys the KTX2 specification (§3.11) defines with binary values, which carry no NUL terminator: buildKVD writes them and DecodeKVD reads them back byte for byte, while every other value is a string whose one terminating NUL is added on write and dropped on read.
var kvdBinaryKeys = map[string]bool{
	"KTXcubemapIncomplete": true,
	"KTXglFormat":          true,
	"KTXdxgiFormat__":      true,
	"KTXmetalPixelFormat":  true,
	"KTXanimData":          true,
}

// Options configures KTX2 texture encoding.
type Options struct {
	// Format selects pixel, universal, or block compression (default FormatRGBA8).
	Format Format
	// Mipmaps generates a complete mipmap chain down to 1x1 when true.
	Mipmaps bool
	// SRGB encodes using the corresponding Vulkan sRGB format, sRGB DFD transfer function, and sRGB-aware linear-light mipmap filtering.
	SRGB bool
	// Supercompression selects an optional KTX2 supercompression scheme (SupercompressionNone default, or SupercompressionZLIB).
	Supercompression uint32
	// KVD adds optional key/value metadata pairs to the KTX2 Key/Value Data section. Standard keys KTXorientation and KTXwriter are populated automatically if absent.
	KVD map[string]string
}

// Header represents the 80-byte KTX2 v2.0 file header (including index offsets).
type Header struct {
	VkFormat               uint32
	TypeSize               uint32
	PixelWidth             uint32
	PixelHeight            uint32
	PixelDepth             uint32
	LayerCount             uint32
	FaceCount              uint32
	LevelCount             uint32
	SupercompressionScheme uint32
	DFDByteOffset          uint32
	DFDByteLength          uint32
	KVDByteOffset          uint32
	KVDByteLength          uint32
	SGDByteOffset          uint64
	SGDByteLength          uint64
}

// LevelIndex describes one mip level entry in the KTX2 level index array.
type LevelIndex struct {
	ByteOffset             uint64
	ByteLength             uint64
	UncompressedByteLength uint64
}

// DFDSample describes one sample block inside a KTX2 Basic Data Format Descriptor.
type DFDSample struct {
	BitOffset      uint16
	BitLength      uint8 // stored as bit_length - 1 per Khronos DFD v1.3
	ChannelType    uint8
	SamplePosition [4]uint8
	SampleLower    uint32
	SampleUpper    uint32
}

// DFD represents a parsed KTX2 Basic Data Format Descriptor block.
type DFD struct {
	TotalSize            uint32
	VendorID             uint32
	DescriptorType       uint32
	VersionNumber        uint32
	DescriptorBlockSize  uint32
	ColorModel           uint8
	ColorPrimaries       uint8
	TransferFunction     uint8
	Flags                uint8
	TexelBlockDimensions [4]uint8 // stored as dim - 1 per Khronos DFD v1.3
	BytesPlane           [8]uint8
	Samples              []DFDSample
}

// Level holds one mip level's dimensions and de-supercompressed payload bytes.
type Level struct {
	Width  int
	Height int
	Data   []byte
}

// Texture holds a fully parsed and validated 2D KTX2 v2.0 container.
type Texture struct {
	Header   Header
	DFD      DFD
	KVD      map[string][]byte
	Format   Format
	SRGB     bool
	HasAlpha bool
	Width    int
	Height   int
	Levels   []Level
	SGD      []byte
	etc1s    *basisLZGlobalData
}

// Encode encodes img into a spec-compliant KTX2 v2.0 byte slice.
func Encode(img image.Image, opts Options) ([]byte, error) {
	if img == nil {
		return nil, errors.New("ktx2: nil image")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("ktx2: invalid image dimensions %dx%d", w, h)
	}

	format := opts.Format
	if format == "" {
		format = FormatRGBA8
	}
	if format == FormatETC1S {
		return nil, errors.New("ktx2: encoding FormatETC1S is not supported (use FormatUASTC)")
	}
	vkFmt, err := format.VkFormat(opts.SRGB)
	if err != nil {
		return nil, err
	}
	if opts.Supercompression != SupercompressionNone && opts.Supercompression != SupercompressionZLIB {
		return nil, fmt.Errorf("ktx2: unsupported encode supercompression scheme %d", opts.Supercompression)
	}

	var mips []*image.NRGBA
	if opts.Mipmaps {
		mips = GenerateMipmaps(img, opts.SRGB)
	} else {
		mips = []*image.NRGBA{ToNRGBA(img)}
	}
	levelCount := len(mips)

	hasAlpha := imageHasAlpha(mips[0])
	encodedLevels := make([][]byte, levelCount)
	uncompressedLens := make([]uint64, levelCount)
	for i, mip := range mips {
		raw := encodeLevel(mip, format, opts.SRGB)
		uncompressedLens[i] = uint64(len(raw))
		if opts.Supercompression == SupercompressionZLIB {
			var zbuf bytes.Buffer
			zw := zlib.NewWriter(&zbuf)
			if _, err := zw.Write(raw); err != nil {
				return nil, fmt.Errorf("ktx2: zlib compress level %d: %w", i, err)
			}
			if err := zw.Close(); err != nil {
				return nil, fmt.Errorf("ktx2: zlib close level %d: %w", i, err)
			}
			encodedLevels[i] = zbuf.Bytes()
		} else {
			encodedLevels[i] = raw
		}
	}

	dfd := buildDFD(format, opts.SRGB, hasAlpha)
	kvd := buildKVD(opts.KVD)

	levelIndexOffset := uint64(HeaderSize)
	dfdOffset := levelIndexOffset + uint64(levelCount)*LevelIndexEntrySize
	kvdOffset := dfdOffset + uint64(len(dfd))
	if len(kvd) == 0 {
		kvdOffset = 0
	}

	dataCursor := dfdOffset + uint64(len(dfd)) + uint64(len(kvd))
	align := format.levelAlignment()
	if opts.Supercompression != SupercompressionNone {
		// Per KTX2 spec §3.6, supercompressed levels require no padding (alignment 1).
		align = 1
	}

	levels := make([]LevelIndex, levelCount)
	// KTX2 spec §3.6 stores mip levels from smallest (levelCount-1) to largest (0).
	for lvl := levelCount - 1; lvl >= 0; lvl-- {
		dataCursor = alignUp(dataCursor, align)
		n := uint64(len(encodedLevels[lvl]))
		levels[lvl] = LevelIndex{
			ByteOffset:             dataCursor,
			ByteLength:             n,
			UncompressedByteLength: uncompressedLens[lvl],
		}
		dataCursor += n
	}

	out := make([]byte, dataCursor)

	// 1. Magic identifier (12 bytes)
	copy(out[0:12], Identifier[:])

	// 2. Header + Index (68 bytes, offsets 12..79)
	binary.LittleEndian.PutUint32(out[12:16], vkFmt)
	binary.LittleEndian.PutUint32(out[16:20], 1) // typeSize = 1 for RGBA8, UASTC, and all _BLOCK formats
	binary.LittleEndian.PutUint32(out[20:24], uint32(w))
	binary.LittleEndian.PutUint32(out[24:28], uint32(h))
	binary.LittleEndian.PutUint32(out[28:32], 0) // pixelDepth = 0 (2D)
	binary.LittleEndian.PutUint32(out[32:36], 0) // layerCount = 0 (non-array)
	binary.LittleEndian.PutUint32(out[36:40], 1) // faceCount = 1 (2D)
	binary.LittleEndian.PutUint32(out[40:44], uint32(levelCount))
	binary.LittleEndian.PutUint32(out[44:48], opts.Supercompression)
	binary.LittleEndian.PutUint32(out[48:52], uint32(dfdOffset))
	binary.LittleEndian.PutUint32(out[52:56], uint32(len(dfd)))
	binary.LittleEndian.PutUint32(out[56:60], uint32(kvdOffset))
	binary.LittleEndian.PutUint32(out[60:64], uint32(len(kvd)))
	binary.LittleEndian.PutUint64(out[64:72], 0) // sgdByteOffset
	binary.LittleEndian.PutUint64(out[72:80], 0) // sgdByteLength

	// 3. Level Index array
	for lvl, entry := range levels {
		off := int(levelIndexOffset) + lvl*LevelIndexEntrySize
		binary.LittleEndian.PutUint64(out[off+0:off+8], entry.ByteOffset)
		binary.LittleEndian.PutUint64(out[off+8:off+16], entry.ByteLength)
		binary.LittleEndian.PutUint64(out[off+16:off+24], entry.UncompressedByteLength)
	}

	// 4. DFD
	copy(out[dfdOffset:dfdOffset+uint64(len(dfd))], dfd)

	// 5. KVD
	if len(kvd) > 0 {
		copy(out[kvdOffset:kvdOffset+uint64(len(kvd))], kvd)
	}

	// 6. Level payloads (written at their precomputed aligned offsets)
	for lvl := levelCount - 1; lvl >= 0; lvl-- {
		entry := levels[lvl]
		copy(out[entry.ByteOffset:entry.ByteOffset+entry.ByteLength], encodedLevels[lvl])
	}

	return out, nil
}

func imageHasAlpha(img *image.NRGBA) bool {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	for y := range h {
		row := y * img.Stride
		for x := range w {
			if img.Pix[row+x*4+3] != 255 {
				return true
			}
		}
	}
	return false
}

// EncodeWriter encodes img as KTX2 and writes it to w.
func EncodeWriter(w io.Writer, img image.Image, opts Options) error {
	data, err := Encode(img, opts)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// EncodeFile reads a PNG or JPEG image from inPath and writes a KTX2 texture to outPath.
func EncodeFile(inPath, outPath string, opts Options) error {
	f, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("ktx2: open %q: %w", inPath, err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("ktx2: decode image %q: %w", inPath, err)
	}

	data, err := Encode(img, opts)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(outPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("ktx2: mkdir %q: %w", dir, err)
		}
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("ktx2: write %q: %w", outPath, err)
	}
	return nil
}

func encodeLevel(mip *image.NRGBA, format Format, srgb bool) []byte {
	switch format {
	case FormatBC1:
		return encodeBC1(mip)
	case FormatBC3:
		return encodeBC3(mip)
	case FormatBC7:
		return encodeBC7(mip)
	case FormatETC2:
		return encodeETC2RGB8(mip)
	case FormatETC2RGBA:
		return encodeETC2RGBA8(mip)
	case FormatASTC4x4:
		return encodeASTC4x4(mip)
	case FormatUASTC:
		return encodeUASTC(mip, srgb)
	default:
		w, h := mip.Bounds().Dx(), mip.Bounds().Dy()
		out := make([]byte, w*h*4)
		for y := range h {
			copy(out[y*w*4:(y+1)*w*4], mip.Pix[y*mip.Stride:y*mip.Stride+w*4])
		}
		return out
	}
}

func buildDFD(format Format, srgb, hasAlpha bool) []byte {
	transfer := DFDTransferLinear
	if srgb {
		transfer = DFDTransferSRGB
	}

	var (
		colorModel uint8
		blockDim   [4]uint8
		bytesPlane [8]uint8
		samples    []DFDSample
	)

	switch format {
	case FormatBC1:
		colorModel = DFDModelBC1A
		blockDim = [4]uint8{3, 3, 0, 0}
		bytesPlane[0] = 8
		samples = []DFDSample{
			{BitOffset: 0, BitLength: 63, ChannelType: DFDChannelBC1AColor, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
		}
	case FormatBC3:
		colorModel = DFDModelBC3
		blockDim = [4]uint8{3, 3, 0, 0}
		bytesPlane[0] = 16
		samples = []DFDSample{
			{BitOffset: 0, BitLength: 63, ChannelType: DFDChannelBC3Alpha, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
			{BitOffset: 64, BitLength: 63, ChannelType: DFDChannelBC3Color, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
		}
	case FormatBC7:
		colorModel = DFDModelBC7
		blockDim = [4]uint8{3, 3, 0, 0}
		bytesPlane[0] = 16
		samples = []DFDSample{
			{BitOffset: 0, BitLength: 127, ChannelType: DFDChannelBC7Color, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
		}
	case FormatETC2:
		colorModel = DFDModelETC2
		blockDim = [4]uint8{3, 3, 0, 0}
		bytesPlane[0] = 8
		samples = []DFDSample{
			{BitOffset: 0, BitLength: 63, ChannelType: DFDChannelETC2Color, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
		}
	case FormatETC2RGBA:
		colorModel = DFDModelETC2
		blockDim = [4]uint8{3, 3, 0, 0}
		bytesPlane[0] = 16
		samples = []DFDSample{
			{BitOffset: 0, BitLength: 63, ChannelType: DFDChannelETC2Alpha, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
			{BitOffset: 64, BitLength: 63, ChannelType: DFDChannelETC2Color, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
		}
	case FormatASTC4x4:
		colorModel = DFDModelASTC
		blockDim = [4]uint8{3, 3, 0, 0}
		bytesPlane[0] = 16
		samples = []DFDSample{
			{BitOffset: 0, BitLength: 127, ChannelType: DFDChannelASTCData, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
		}
	case FormatUASTC:
		colorModel = DFDModelUASTC
		blockDim = [4]uint8{3, 3, 0, 0}
		bytesPlane[0] = 16
		ch := DFDChannelUASTCRGB
		if hasAlpha {
			ch = DFDChannelUASTCRGBA
		}
		samples = []DFDSample{
			{BitOffset: 0, BitLength: 127, ChannelType: ch, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
		}
	default: // FormatRGBA8
		colorModel = DFDModelRGBSDA
		blockDim = [4]uint8{0, 0, 0, 0}
		bytesPlane[0] = 4
		samples = []DFDSample{
			{BitOffset: 0, BitLength: 7, ChannelType: DFDChannelRGBSDARed, SampleLower: 0, SampleUpper: 255},
			{BitOffset: 8, BitLength: 7, ChannelType: DFDChannelRGBSDAGreen, SampleLower: 0, SampleUpper: 255},
			{BitOffset: 16, BitLength: 7, ChannelType: DFDChannelRGBSDABlue, SampleLower: 0, SampleUpper: 255},
			{BitOffset: 24, BitLength: 7, ChannelType: DFDChannelRGBSDAAlpha, SampleLower: 0, SampleUpper: 255},
		}
	}

	blockSize := uint32(24 + 16*len(samples))
	totalSize := 4 + blockSize
	buf := make([]byte, totalSize)

	binary.LittleEndian.PutUint32(buf[0:4], totalSize)
	// Word 0: vendorId (bits 0..16) | descriptorType (bits 17..31)
	binary.LittleEndian.PutUint32(buf[4:8], (DFDVendorKhronos&0x1FFFF)|((DFDDescriptorBasicFormat&0x7FFF)<<17))
	// Word 1: versionNumber (bits 0..15) | descriptorBlockSize (bits 16..31)
	binary.LittleEndian.PutUint32(buf[8:12], (DFDVersionNumber&0xFFFF)|(blockSize<<16))
	// Word 2: colorModel, colorPrimaries, transferFunction, flags
	buf[12] = colorModel
	buf[13] = DFDPrimariesBT709
	buf[14] = transfer
	buf[15] = 0
	// Word 3: texelBlockDimension0..3
	copy(buf[16:20], blockDim[:])
	// Words 4..5: bytesPlane0..7
	copy(buf[20:28], bytesPlane[:])

	for i, s := range samples {
		off := 28 + i*16
		binary.LittleEndian.PutUint16(buf[off+0:off+2], s.BitOffset)
		buf[off+2] = s.BitLength
		buf[off+3] = s.ChannelType
		copy(buf[off+4:off+8], s.SamplePosition[:])
		binary.LittleEndian.PutUint32(buf[off+8:off+12], s.SampleLower)
		binary.LittleEndian.PutUint32(buf[off+12:off+16], s.SampleUpper)
	}
	return buf
}

func buildKVD(extra map[string]string) []byte {
	kv := make(map[string]string, len(extra)+2)
	kv["KTXorientation"] = "rd"
	kv["KTXwriter"] = "nsgo game tex"
	for k, v := range extra {
		if k != "" {
			kv[k] = v
		}
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	// KTX2 spec §3.4 requires KVD entries to be sorted lexicographically by UTF-8 key bytes.
	sort.Strings(keys)

	var buf bytes.Buffer
	var lenBuf [4]byte
	for _, k := range keys {
		val := kv[k]
		term := 1 // string values end in one NUL, binary values in none
		if kvdBinaryKeys[k] {
			term = 0
		}
		entryLen := uint32(len(k) + 1 + len(val) + term)
		binary.LittleEndian.PutUint32(lenBuf[:], entryLen)
		buf.Write(lenBuf[:])
		buf.WriteString(k)
		buf.WriteByte(0)
		buf.WriteString(val)
		if term == 1 {
			buf.WriteByte(0)
		}
		if rem := int(entryLen % 4); rem != 0 {
			for range 4 - rem {
				buf.WriteByte(0)
			}
		}
	}
	return buf.Bytes()
}

func rangeInBounds(off, length uint64, total int) bool {
	uTotal := uint64(total)
	return off <= uTotal && length <= uTotal-off
}

// DecodeHeader parses the KTX2 identifier, header, and level index array.
func DecodeHeader(data []byte) (Header, []LevelIndex, error) {
	if len(data) < HeaderSize {
		return Header{}, nil, fmt.Errorf("ktx2: truncated file (%d bytes, need at least %d)", len(data), HeaderSize)
	}
	if !bytes.Equal(data[0:12], Identifier[:]) {
		return Header{}, nil, errors.New("ktx2: invalid KTX2 magic identifier")
	}

	h := Header{
		VkFormat:               binary.LittleEndian.Uint32(data[12:16]),
		TypeSize:               binary.LittleEndian.Uint32(data[16:20]),
		PixelWidth:             binary.LittleEndian.Uint32(data[20:24]),
		PixelHeight:            binary.LittleEndian.Uint32(data[24:28]),
		PixelDepth:             binary.LittleEndian.Uint32(data[28:32]),
		LayerCount:             binary.LittleEndian.Uint32(data[32:36]),
		FaceCount:              binary.LittleEndian.Uint32(data[36:40]),
		LevelCount:             binary.LittleEndian.Uint32(data[40:44]),
		SupercompressionScheme: binary.LittleEndian.Uint32(data[44:48]),
		DFDByteOffset:          binary.LittleEndian.Uint32(data[48:52]),
		DFDByteLength:          binary.LittleEndian.Uint32(data[52:56]),
		KVDByteOffset:          binary.LittleEndian.Uint32(data[56:60]),
		KVDByteLength:          binary.LittleEndian.Uint32(data[60:64]),
		SGDByteOffset:          binary.LittleEndian.Uint64(data[64:72]),
		SGDByteLength:          binary.LittleEndian.Uint64(data[72:80]),
	}

	lvlCount := int(h.LevelCount)
	if lvlCount == 0 {
		lvlCount = 1
	}
	if lvlCount > maxLevelCount {
		return Header{}, nil, fmt.Errorf("ktx2: levelCount %d exceeds maximum %d", h.LevelCount, maxLevelCount)
	}
	indexEnd := uint64(HeaderSize) + uint64(lvlCount)*LevelIndexEntrySize
	if uint64(len(data)) < indexEnd {
		return Header{}, nil, fmt.Errorf("ktx2: truncated level index (file %d bytes, need %d)", len(data), indexEnd)
	}

	levels := make([]LevelIndex, lvlCount)
	for i := range lvlCount {
		off := HeaderSize + i*LevelIndexEntrySize
		levels[i] = LevelIndex{
			ByteOffset:             binary.LittleEndian.Uint64(data[off+0 : off+8]),
			ByteLength:             binary.LittleEndian.Uint64(data[off+8 : off+16]),
			UncompressedByteLength: binary.LittleEndian.Uint64(data[off+16 : off+24]),
		}
	}
	return h, levels, nil
}

// ParseDFD decodes the Basic Data Format Descriptor block from raw DFD bytes.
func ParseDFD(dfdBytes []byte) (DFD, error) {
	if len(dfdBytes) < 28 {
		return DFD{}, fmt.Errorf("ktx2: truncated DFD (%d bytes)", len(dfdBytes))
	}
	totalSize := binary.LittleEndian.Uint32(dfdBytes[0:4])
	if uint64(totalSize) != uint64(len(dfdBytes)) {
		return DFD{}, fmt.Errorf("ktx2: DFD totalSize %d does not match byte length %d", totalSize, len(dfdBytes))
	}
	w0 := binary.LittleEndian.Uint32(dfdBytes[4:8])
	w1 := binary.LittleEndian.Uint32(dfdBytes[8:12])
	blockSize := w1 >> 16
	if blockSize < 24 || 4+uint64(blockSize) > uint64(totalSize) || (blockSize-24)%16 != 0 {
		return DFD{}, fmt.Errorf("ktx2: invalid DFD descriptorBlockSize %d", blockSize)
	}

	numSamples := int((blockSize - 24) / 16)
	samples := make([]DFDSample, numSamples)
	for i := range numSamples {
		off := 28 + i*16
		var pos [4]uint8
		copy(pos[:], dfdBytes[off+4:off+8])
		samples[i] = DFDSample{
			BitOffset:      binary.LittleEndian.Uint16(dfdBytes[off+0 : off+2]),
			BitLength:      dfdBytes[off+2],
			ChannelType:    dfdBytes[off+3],
			SamplePosition: pos,
			SampleLower:    binary.LittleEndian.Uint32(dfdBytes[off+8 : off+12]),
			SampleUpper:    binary.LittleEndian.Uint32(dfdBytes[off+12 : off+16]),
		}
	}

	var dim [4]uint8
	copy(dim[:], dfdBytes[16:20])
	var bp [8]uint8
	copy(bp[:], dfdBytes[20:28])

	return DFD{
		TotalSize:            totalSize,
		VendorID:             w0 & 0x1FFFF,
		DescriptorType:       (w0 >> 17) & 0x7FFF,
		VersionNumber:        w1 & 0xFFFF,
		DescriptorBlockSize:  blockSize,
		ColorModel:           dfdBytes[12],
		ColorPrimaries:       dfdBytes[13],
		TransferFunction:     dfdBytes[14],
		Flags:                dfdBytes[15],
		TexelBlockDimensions: dim,
		BytesPlane:           bp,
		Samples:              samples,
	}, nil
}

// DecodeKVD parses the Key/Value Data section of a KTX2 file and verifies lexicographical key order.
func DecodeKVD(kvdBytes []byte) (map[string][]byte, error) {
	out := make(map[string][]byte)
	pos := 0
	var prevKey string
	for pos < len(kvdBytes) {
		if pos+4 > len(kvdBytes) {
			return nil, errors.New("ktx2: truncated KVD entry length")
		}
		n64 := uint64(binary.LittleEndian.Uint32(kvdBytes[pos : pos+4]))
		pos += 4
		if n64 == 0 || n64 > uint64(len(kvdBytes)-pos) {
			return nil, fmt.Errorf("ktx2: invalid KVD entry length %d", n64)
		}
		n := int(n64)
		entry := kvdBytes[pos : pos+n]
		nul := bytes.IndexByte(entry, 0)
		if nul <= 0 {
			return nil, errors.New("ktx2: KVD entry missing NUL key terminator")
		}
		key := string(entry[:nul])
		if prevKey != "" && key <= prevKey {
			return nil, fmt.Errorf("ktx2: KVD keys not strictly sorted (%q after %q)", key, prevKey)
		}
		prevKey = key
		val := make([]byte, len(entry)-(nul+1))
		copy(val, entry[nul+1:])
		// A string value ends in one NUL terminator, which is dropped; any zero bytes before it are part of the value. A binary value the specification defines has no terminator and is kept byte for byte.
		if !kvdBinaryKeys[key] && len(val) > 0 && val[len(val)-1] == 0 {
			val = val[:len(val)-1]
		}
		out[key] = val

		pos += n
		if rem := n % 4; rem != 0 {
			pad := 4 - rem
			if pos+pad > len(kvdBytes) {
				return nil, errors.New("ktx2: truncated KVD padding")
			}
			pos += pad
		}
	}
	return out, nil
}

// Validate checks a KTX2 buffer for structural compliance (magic, header, DFD, KVD, SGD bounds, level index bounds, and mip level byte sizes).
func Validate(data []byte) error {
	h, levels, err := DecodeHeader(data)
	if err != nil {
		return err
	}
	if h.PixelWidth == 0 || h.PixelHeight == 0 {
		return fmt.Errorf("ktx2: invalid texture dimensions %dx%d", h.PixelWidth, h.PixelHeight)
	}
	if h.PixelWidth > maxTextureDimension || h.PixelHeight > maxTextureDimension {
		return fmt.Errorf("ktx2: texture dimensions %dx%d exceed maximum %d", h.PixelWidth, h.PixelHeight, maxTextureDimension)
	}
	if h.FaceCount == 0 {
		return errors.New("ktx2: faceCount must be >= 1")
	}
	if maxMips := MipLevelCount(int(h.PixelWidth), int(h.PixelHeight)); len(levels) > maxMips {
		return fmt.Errorf("ktx2: levelCount %d exceeds max %d for %dx%d", len(levels), maxMips, h.PixelWidth, h.PixelHeight)
	}
	if h.DFDByteOffset == 0 || h.DFDByteLength == 0 {
		return errors.New("ktx2: missing DFD section")
	}
	if !rangeInBounds(uint64(h.DFDByteOffset), uint64(h.DFDByteLength), len(data)) {
		return errors.New("ktx2: DFD section out of bounds")
	}
	dfd, err := ParseDFD(data[h.DFDByteOffset : h.DFDByteOffset+h.DFDByteLength])
	if err != nil {
		return err
	}
	if dfd.VersionNumber != DFDVersionNumber {
		return fmt.Errorf("ktx2: unsupported DFD version %d", dfd.VersionNumber)
	}

	if h.KVDByteLength > 0 {
		if !rangeInBounds(uint64(h.KVDByteOffset), uint64(h.KVDByteLength), len(data)) {
			return errors.New("ktx2: KVD section out of bounds")
		}
		if _, err := DecodeKVD(data[h.KVDByteOffset : h.KVDByteOffset+h.KVDByteLength]); err != nil {
			return err
		}
	}
	if h.SGDByteLength > 0 {
		if !rangeInBounds(h.SGDByteOffset, h.SGDByteLength, len(data)) {
			return errors.New("ktx2: SGD section out of bounds")
		}
	}

	format, srgb, _, known := FormatFromHeaderDFD(h, dfd)
	if known && h.VkFormat != VkFormatUndefined {
		wantTransfer := DFDTransferLinear
		if srgb {
			wantTransfer = DFDTransferSRGB
		}
		if dfd.TransferFunction != wantTransfer {
			return fmt.Errorf("ktx2: DFD transfer function %d does not match vkFormat %d", dfd.TransferFunction, h.VkFormat)
		}
	}

	for lvl, entry := range levels {
		if !rangeInBounds(entry.ByteOffset, entry.ByteLength, len(data)) {
			return fmt.Errorf("ktx2: level %d payload out of bounds", lvl)
		}
		if known && format != FormatETC1S {
			w, ht := MipDimensions(int(h.PixelWidth), int(h.PixelHeight), lvl)
			wantBytes := uint64(format.LevelByteSize(w, ht))
			switch h.SupercompressionScheme {
			case SupercompressionNone:
				if entry.ByteLength != wantBytes {
					return fmt.Errorf("ktx2: level %d byteLength=%d, want %d for %dx%d %s", lvl, entry.ByteLength, wantBytes, w, ht, format)
				}
				if align := format.levelAlignment(); entry.ByteOffset%align != 0 {
					return fmt.Errorf("ktx2: level %d byteOffset=%d is not aligned to %d", lvl, entry.ByteOffset, align)
				}
			case SupercompressionZstandard, SupercompressionZLIB:
				if entry.UncompressedByteLength != wantBytes {
					return fmt.Errorf("ktx2: level %d uncompressedByteLength=%d, want %d for %dx%d %s", lvl, entry.UncompressedByteLength, wantBytes, w, ht, format)
				}
			}
		}
	}
	return nil
}

// Decode parses, validates, and de-supercompresses a 2D KTX2 v2.0 container. Supports SupercompressionNone (0), SupercompressionBasisLZ (1), SupercompressionZstandard (2), and SupercompressionZLIB (3) across RGBA8, BC1, BC3, BC7, ETC2 RGB8/RGBA8, ASTC 4x4, Basis Universal UASTC 4x4, and BasisLZ/ETC1S.
func Decode(data []byte) (*Texture, error) {
	if err := Validate(data); err != nil {
		return nil, err
	}
	h, levelEntries, err := DecodeHeader(data)
	if err != nil {
		return nil, err
	}
	if h.PixelDepth > 1 {
		return nil, fmt.Errorf("ktx2: 3D textures (pixelDepth=%d) are not supported", h.PixelDepth)
	}
	if h.LayerCount > 1 {
		return nil, fmt.Errorf("ktx2: texture arrays (layerCount=%d) are not supported", h.LayerCount)
	}
	if h.FaceCount != 1 {
		return nil, fmt.Errorf("ktx2: cubemap/multi-face textures (faceCount=%d) are not supported", h.FaceCount)
	}

	dfd, err := ParseDFD(data[h.DFDByteOffset : h.DFDByteOffset+h.DFDByteLength])
	if err != nil {
		return nil, err
	}
	var kvd map[string][]byte
	if h.KVDByteLength > 0 {
		kvd, err = DecodeKVD(data[h.KVDByteOffset : h.KVDByteOffset+h.KVDByteLength])
		if err != nil {
			return nil, err
		}
	}
	var sgd []byte
	if h.SGDByteLength > 0 {
		sgd = slices.Clone(data[h.SGDByteOffset : h.SGDByteOffset+h.SGDByteLength])
	}

	format, srgb, hasAlpha, ok := FormatFromHeaderDFD(h, dfd)
	if !ok {
		return nil, fmt.Errorf("ktx2: unsupported texture format (vkFormat=%d, dfdColorModel=%d)", h.VkFormat, dfd.ColorModel)
	}

	levels := make([]Level, len(levelEntries))
	for lvl, entry := range levelEntries {
		w, ht := MipDimensions(int(h.PixelWidth), int(h.PixelHeight), lvl)
		rawSlice := data[entry.ByteOffset : entry.ByteOffset+entry.ByteLength]

		var payload []byte
		switch h.SupercompressionScheme {
		case SupercompressionNone:
			payload = slices.Clone(rawSlice)
		case SupercompressionBasisLZ:
			if format != FormatETC1S {
				return nil, fmt.Errorf("ktx2: BasisLZ supercompression requires ETC1S DFD model, got %s", format)
			}
			payload = slices.Clone(rawSlice)
		case SupercompressionZstandard:
			want := uint64(format.LevelByteSize(w, ht))
			if want == 0 || want > maxUncompressedLevelBytes {
				return nil, fmt.Errorf("ktx2: level %d uncompressed size %d out of range", lvl, want)
			}
			dec, err := decompressZstd(rawSlice, want)
			if err != nil {
				return nil, fmt.Errorf("ktx2: level %d zstd: %w", lvl, err)
			}
			if uint64(len(dec)) != want {
				return nil, fmt.Errorf("ktx2: level %d zstd decoded %d bytes, want %d", lvl, len(dec), want)
			}
			payload = dec
		case SupercompressionZLIB:
			want := uint64(format.LevelByteSize(w, ht))
			if want == 0 || want > maxUncompressedLevelBytes {
				return nil, fmt.Errorf("ktx2: level %d uncompressed size %d out of range", lvl, want)
			}
			dec, err := decompressZLIB(rawSlice, want)
			if err != nil {
				return nil, fmt.Errorf("ktx2: level %d zlib: %w", lvl, err)
			}
			if uint64(len(dec)) != want {
				return nil, fmt.Errorf("ktx2: level %d zlib decoded %d bytes, want %d", lvl, len(dec), want)
			}
			payload = dec
		default:
			return nil, fmt.Errorf("ktx2: unsupported supercompression scheme %d", h.SupercompressionScheme)
		}

		levels[lvl] = Level{
			Width:  w,
			Height: ht,
			Data:   payload,
		}
	}

	var etc1sGD *basisLZGlobalData
	if format == FormatETC1S {
		if h.SupercompressionScheme != SupercompressionBasisLZ {
			return nil, fmt.Errorf("ktx2: ETC1S requires BasisLZ supercompression, got %d", h.SupercompressionScheme)
		}
		etc1sGD, err = parseBasisLZSGD(sgd, len(levels), hasAlpha)
		if err != nil {
			return nil, err
		}
	}

	return &Texture{
		Header:   h,
		DFD:      dfd,
		KVD:      kvd,
		Format:   format,
		SRGB:     srgb,
		HasAlpha: hasAlpha,
		Width:    int(h.PixelWidth),
		Height:   int(h.PixelHeight),
		Levels:   levels,
		SGD:      sgd,
		etc1s:    etc1sGD,
	}, nil
}

// ReadLevel returns the raw (on-disk) bytes and pixel dimensions for mip level (0 = base level).
func ReadLevel(data []byte, level int) (raw []byte, width, height int, err error) {
	h, levels, err := DecodeHeader(data)
	if err != nil {
		return nil, 0, 0, err
	}
	if h.PixelWidth == 0 || h.PixelHeight == 0 {
		return nil, 0, 0, fmt.Errorf("ktx2: invalid texture dimensions %dx%d", h.PixelWidth, h.PixelHeight)
	}
	if level < 0 || level >= len(levels) {
		return nil, 0, 0, fmt.Errorf("ktx2: level %d out of range [0, %d)", level, len(levels))
	}
	entry := levels[level]
	if !rangeInBounds(entry.ByteOffset, entry.ByteLength, len(data)) {
		return nil, 0, 0, fmt.Errorf("ktx2: level %d out of bounds", level)
	}
	w, ht := MipDimensions(int(h.PixelWidth), int(h.PixelHeight), level)
	out := make([]byte, entry.ByteLength)
	copy(out, data[entry.ByteOffset:entry.ByteOffset+entry.ByteLength])
	return out, w, ht, nil
}

// DecodeLevelRGBA decodes mip level (0 = base) of t into *image.NRGBA.
func (t *Texture) DecodeLevelRGBA(level int) (*image.NRGBA, error) {
	if t == nil {
		return nil, errors.New("ktx2: nil texture")
	}
	if level < 0 || level >= len(t.Levels) {
		return nil, fmt.Errorf("ktx2: level %d out of range [0, %d)", level, len(t.Levels))
	}
	lvl := t.Levels[level]
	if err := checkRGBALevelSize(lvl.Width, lvl.Height); err != nil {
		return nil, err
	}
	if t.Format == FormatETC1S {
		return t.decodeETC1SLevelRGBA(level)
	}
	return decodeLevelToRGBA(t.Format, lvl.Data, lvl.Width, lvl.Height, t.SRGB)
}

// Transcode converts every mip level of t to target format. When t.Format already equals target (and target is a concrete GPU/pixel format), level payloads are cloned directly without pixel decoding or re-encoding.
func (t *Texture) Transcode(target Format) ([]Level, error) {
	if t == nil {
		return nil, errors.New("ktx2: nil texture")
	}
	if target == "" {
		target = FormatRGBA8
	}
	if target == FormatETC1S {
		return nil, errors.New("ktx2: transcoding to FormatETC1S is not supported")
	}
	if t.Format == target {
		out := make([]Level, len(t.Levels))
		for i, lvl := range t.Levels {
			out[i] = Level{
				Width:  lvl.Width,
				Height: lvl.Height,
				Data:   slices.Clone(lvl.Data),
			}
		}
		return out, nil
	}

	out := make([]Level, len(t.Levels))
	for i, lvl := range t.Levels {
		rgba, err := t.DecodeLevelRGBA(i)
		if err != nil {
			return nil, fmt.Errorf("ktx2: transcode level %d: %w", i, err)
		}
		out[i] = Level{
			Width:  lvl.Width,
			Height: lvl.Height,
			Data:   encodeLevel(rgba, target, t.SRGB),
		}
	}
	return out, nil
}

func decodeLevelToRGBA(format Format, raw []byte, w, ht int, srgb bool) (*image.NRGBA, error) {
	if format == FormatETC1S {
		return nil, errors.New("ktx2: BasisLZ/ETC1S decoding is not supported (use UASTC, BC1/BC3/BC7, ETC2, ASTC 4x4, or RGBA8)")
	}
	wantLen := format.LevelByteSize(w, ht)
	if len(raw) < wantLen {
		return nil, fmt.Errorf("ktx2: level truncated (%d bytes, need %d for %dx%d %s)", len(raw), wantLen, w, ht, format)
	}
	switch format {
	case FormatRGBA8:
		img := image.NewNRGBA(image.Rect(0, 0, w, ht))
		copy(img.Pix, raw[:w*ht*4])
		return img, nil
	case FormatBC1:
		return decodeBC1(raw, w, ht), nil
	case FormatBC3:
		return decodeBC3(raw, w, ht), nil
	case FormatBC7:
		return decodeBC7(raw, w, ht), nil
	case FormatETC2:
		return decodeETC2RGB8(raw, w, ht), nil
	case FormatETC2RGBA:
		return decodeETC2RGBA8(raw, w, ht), nil
	case FormatASTC4x4:
		return decodeASTC4x4(raw, w, ht, srgb)
	case FormatUASTC:
		return decodeUASTC(raw, w, ht, srgb), nil
	default:
		return nil, fmt.Errorf("ktx2: unsupported format %q", format)
	}
}

// DecodeRGBA decodes the specified mip level (0 = base) of a KTX2 texture into *image.NRGBA. Supports RGBA8, BC1, BC3, BC7, ETC2 RGB8/RGBA8, ASTC 4x4, Basis Universal UASTC 4x4, and BasisLZ/ETC1S with uncompressed, BasisLZ, Zstandard, or ZLIB supercompression.
func DecodeRGBA(data []byte, level int) (*image.NRGBA, error) {
	// Refuse an oversized level from the header alone, before Decode decompresses anything.
	if h, levels, err := DecodeHeader(data); err == nil && level >= 0 && level < len(levels) {
		w, ht := MipDimensions(int(h.PixelWidth), int(h.PixelHeight), level)
		if err := checkRGBALevelSize(w, ht); err != nil {
			return nil, err
		}
	}
	tex, err := Decode(data)
	if err != nil {
		return nil, err
	}
	return tex.DecodeLevelRGBA(level)
}

// checkRGBALevelSize refuses a w x ht level whose RGBA image would exceed maxUncompressedLevelBytes, that is one larger than 8192x8192.
func checkRGBALevelSize(w, ht int) error {
	if w < 0 || ht < 0 {
		return fmt.Errorf("ktx2: invalid level dimensions %dx%d", w, ht)
	}
	if n := uint64(w) * uint64(ht) * 4; n > maxUncompressedLevelBytes {
		return fmt.Errorf("ktx2: %dx%d level decodes to %d RGBA bytes, over the %d-byte limit", w, ht, n, maxUncompressedLevelBytes)
	}
	return nil
}

func alignUp(v, align uint64) uint64 {
	if align <= 1 {
		return v
	}
	rem := v % align
	if rem == 0 {
		return v
	}
	return v + (align - rem)
}

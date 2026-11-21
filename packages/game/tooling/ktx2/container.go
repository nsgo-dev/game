package ktx2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Options configures KTX2 texture encoding.
type Options struct {
	// Format selects pixel/block compression (default FormatRGBA8).
	Format Format
	// Mipmaps generates a complete mipmap chain down to 1x1 when true.
	Mipmaps bool
	// SRGB encodes using the corresponding Vulkan sRGB format, sRGB DFD transfer
	// function, and sRGB-aware linear-light mipmap filtering.
	SRGB bool
	// KVD adds optional key/value metadata pairs to the KTX2 Key/Value Data section.
	// Standard keys KTXorientation and KTXwriter are populated automatically if absent.
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
	vkFmt, err := format.VkFormat(opts.SRGB)
	if err != nil {
		return nil, err
	}

	var mips []*image.NRGBA
	if opts.Mipmaps {
		mips = GenerateMipmaps(img, opts.SRGB)
	} else {
		mips = []*image.NRGBA{ToNRGBA(img)}
	}
	levelCount := len(mips)

	encodedLevels := make([][]byte, levelCount)
	for i, mip := range mips {
		encodedLevels[i] = encodeLevel(mip, format)
	}

	dfd := buildDFD(format, opts.SRGB)
	kvd := buildKVD(opts.KVD)

	levelIndexOffset := uint64(HeaderSize)
	dfdOffset := levelIndexOffset + uint64(levelCount)*LevelIndexEntrySize
	kvdOffset := dfdOffset + uint64(len(dfd))
	if len(kvd) == 0 {
		kvdOffset = 0
	}

	dataCursor := dfdOffset + uint64(len(dfd)) + uint64(len(kvd))
	align := format.levelAlignment()

	levels := make([]LevelIndex, levelCount)
	// KTX2 spec §3.6 stores mip levels from smallest (levelCount-1) to largest (0).
	for lvl := levelCount - 1; lvl >= 0; lvl-- {
		dataCursor = alignUp(dataCursor, align)
		n := uint64(len(encodedLevels[lvl]))
		levels[lvl] = LevelIndex{
			ByteOffset:             dataCursor,
			ByteLength:             n,
			UncompressedByteLength: n,
		}
		dataCursor += n
	}

	out := make([]byte, dataCursor)

	// 1. Magic identifier (12 bytes)
	copy(out[0:12], Identifier[:])

	// 2. Header + Index (68 bytes, offsets 12..79)
	binary.LittleEndian.PutUint32(out[12:16], vkFmt)
	binary.LittleEndian.PutUint32(out[16:20], 1) // typeSize = 1 for RGBA8 and all _BLOCK formats
	binary.LittleEndian.PutUint32(out[20:24], uint32(w))
	binary.LittleEndian.PutUint32(out[24:28], uint32(h))
	binary.LittleEndian.PutUint32(out[28:32], 0) // pixelDepth = 0 (2D)
	binary.LittleEndian.PutUint32(out[32:36], 0) // layerCount = 0 (non-array)
	binary.LittleEndian.PutUint32(out[36:40], 1) // faceCount = 1 (2D)
	binary.LittleEndian.PutUint32(out[40:44], uint32(levelCount))
	binary.LittleEndian.PutUint32(out[44:48], SupercompressionNone)
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

func encodeLevel(mip *image.NRGBA, format Format) []byte {
	switch format {
	case FormatBC1:
		return encodeBC1(mip)
	case FormatBC3:
		return encodeBC3(mip)
	case FormatETC2:
		return encodeETC2RGB8(mip)
	default:
		w, h := mip.Bounds().Dx(), mip.Bounds().Dy()
		out := make([]byte, w*h*4)
		for y := range h {
			copy(out[y*w*4:(y+1)*w*4], mip.Pix[y*mip.Stride:y*mip.Stride+w*4])
		}
		return out
	}
}

func buildDFD(format Format, srgb bool) []byte {
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
	case FormatETC2:
		colorModel = DFDModelETC2
		blockDim = [4]uint8{3, 3, 0, 0}
		bytesPlane[0] = 8
		samples = []DFDSample{
			{BitOffset: 0, BitLength: 63, ChannelType: DFDChannelETC2Color, SampleLower: 0, SampleUpper: 0xFFFFFFFF},
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
	kv["KTXwriter"] = "nsgo-gametex"
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
		entryLen := uint32(len(k) + 1 + len(val) + 1) // key + NUL + string value + NUL
		binary.LittleEndian.PutUint32(lenBuf[:], entryLen)
		buf.Write(lenBuf[:])
		buf.WriteString(k)
		buf.WriteByte(0)
		buf.WriteString(val)
		buf.WriteByte(0)
		if rem := int(entryLen % 4); rem != 0 {
			for range 4 - rem {
				buf.WriteByte(0)
			}
		}
	}
	return buf.Bytes()
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
	if int(totalSize) != len(dfdBytes) {
		return DFD{}, fmt.Errorf("ktx2: DFD totalSize %d does not match byte length %d", totalSize, len(dfdBytes))
	}
	w0 := binary.LittleEndian.Uint32(dfdBytes[4:8])
	w1 := binary.LittleEndian.Uint32(dfdBytes[8:12])
	blockSize := w1 >> 16
	if blockSize < 24 || 4+blockSize > totalSize || (blockSize-24)%16 != 0 {
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
		n := int(binary.LittleEndian.Uint32(kvdBytes[pos : pos+4]))
		pos += 4
		if n <= 0 || pos+n > len(kvdBytes) {
			return nil, fmt.Errorf("ktx2: invalid KVD entry length %d", n)
		}
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

// Validate checks a KTX2 buffer for structural compliance (magic, header, DFD, KVD,
// level index bounds, and mip level byte sizes).
func Validate(data []byte) error {
	h, levels, err := DecodeHeader(data)
	if err != nil {
		return err
	}
	if h.PixelWidth == 0 || h.PixelHeight == 0 {
		return fmt.Errorf("ktx2: invalid texture dimensions %dx%d", h.PixelWidth, h.PixelHeight)
	}
	if h.FaceCount == 0 {
		return errors.New("ktx2: faceCount must be >= 1")
	}
	if h.DFDByteOffset == 0 || h.DFDByteLength == 0 {
		return errors.New("ktx2: missing DFD section")
	}
	if uint64(h.DFDByteOffset)+uint64(h.DFDByteLength) > uint64(len(data)) {
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
		if uint64(h.KVDByteOffset)+uint64(h.KVDByteLength) > uint64(len(data)) {
			return errors.New("ktx2: KVD section out of bounds")
		}
		if _, err := DecodeKVD(data[h.KVDByteOffset : h.KVDByteOffset+h.KVDByteLength]); err != nil {
			return err
		}
	}

	format, srgb, known := FormatFromVk(h.VkFormat)
	if known {
		wantTransfer := DFDTransferLinear
		if srgb {
			wantTransfer = DFDTransferSRGB
		}
		if dfd.TransferFunction != wantTransfer {
			return fmt.Errorf("ktx2: DFD transfer function %d does not match vkFormat %d", dfd.TransferFunction, h.VkFormat)
		}
	}

	for lvl, entry := range levels {
		if entry.ByteOffset+entry.ByteLength > uint64(len(data)) {
			return fmt.Errorf("ktx2: level %d payload out of bounds", lvl)
		}
		if known && h.SupercompressionScheme == SupercompressionNone {
			w, ht := MipDimensions(int(h.PixelWidth), int(h.PixelHeight), lvl)
			wantBytes := uint64(format.levelByteSize(w, ht))
			if entry.ByteLength != wantBytes {
				return fmt.Errorf("ktx2: level %d byteLength=%d, want %d for %dx%d %s", lvl, entry.ByteLength, wantBytes, w, ht, format)
			}
			if align := format.levelAlignment(); entry.ByteOffset%align != 0 {
				return fmt.Errorf("ktx2: level %d byteOffset=%d is not aligned to %d", lvl, entry.ByteOffset, align)
			}
		}
	}
	return nil
}

// ReadLevel returns the raw bytes and pixel dimensions for mip level (0 = base level).
func ReadLevel(data []byte, level int) (raw []byte, width, height int, err error) {
	h, levels, err := DecodeHeader(data)
	if err != nil {
		return nil, 0, 0, err
	}
	if level < 0 || level >= len(levels) {
		return nil, 0, 0, fmt.Errorf("ktx2: level %d out of range [0, %d)", level, len(levels))
	}
	entry := levels[level]
	if entry.ByteOffset+entry.ByteLength > uint64(len(data)) {
		return nil, 0, 0, fmt.Errorf("ktx2: level %d out of bounds", level)
	}
	w, ht := MipDimensions(int(h.PixelWidth), int(h.PixelHeight), level)
	out := make([]byte, entry.ByteLength)
	copy(out, data[entry.ByteOffset:entry.ByteOffset+entry.ByteLength])
	return out, w, ht, nil
}

// DecodeRGBA decodes the specified mip level (0 = base) of a KTX2 texture into *image.NRGBA.
// Supports RGBA8, BC1, BC3, and ETC2 RGB8 (uncompressed supercompressionScheme == 0).
func DecodeRGBA(data []byte, level int) (*image.NRGBA, error) {
	h, _, err := DecodeHeader(data)
	if err != nil {
		return nil, err
	}
	if h.SupercompressionScheme != SupercompressionNone {
		return nil, fmt.Errorf("ktx2: unsupported supercompression scheme %d", h.SupercompressionScheme)
	}
	format, _, ok := FormatFromVk(h.VkFormat)
	if !ok {
		return nil, fmt.Errorf("ktx2: unsupported vkFormat %d for DecodeRGBA", h.VkFormat)
	}
	raw, w, ht, err := ReadLevel(data, level)
	if err != nil {
		return nil, err
	}
	wantLen := format.levelByteSize(w, ht)
	if len(raw) < wantLen {
		return nil, fmt.Errorf("ktx2: level %d truncated (%d bytes, need %d)", level, len(raw), wantLen)
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
	case FormatETC2:
		return decodeETC2RGB8(raw, w, ht), nil
	default:
		return nil, fmt.Errorf("ktx2: unsupported format %q", format)
	}
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

package audioconv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// maxWAVChunkSize caps a single WAVE chunk the decoder will buffer, so a malformed
// or hostile header cannot exhaust memory before the stream is known to be short.
const maxWAVChunkSize = 256 << 20

const (
	waveFormatPCM        uint16 = 1
	waveFormatIEEEFloat  uint16 = 3
	waveFormatExtensible uint16 = 0xFFFE
)

// KSDATAFORMAT_SUBTYPE_PCM and KSDATAFORMAT_SUBTYPE_IEEE_FLOAT GUIDs (little-endian).
var (
	guidSubformatPCM = [16]byte{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00,
		0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71,
	}
	guidSubformatIEEEFloat = [16]byte{
		0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00,
		0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71,
	}
)

// ReadWAVFile reads and decodes a WAV file from disk.
func ReadWAVFile(path string) (*Buffer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return DecodeWAV(f)
}

// WriteWAVFile encodes buf as a 16-bit PCM WAV file at path.
func WriteWAVFile(path string, buf *Buffer) error {
	var out bytes.Buffer
	if err := EncodeWAV(&out, buf); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// DecodeWAV reads a RIFF/WAVE stream containing 8-bit unsigned, 16-bit signed, 24-bit signed, 32-bit signed, or 32-bit/64-bit IEEE float PCM audio (mono or stereo). The first forward loop of a smpl chunk becomes the Buffer's Loop when it lies inside the audio.
func DecodeWAV(r io.Reader) (*Buffer, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("audioconv: wav header: %w", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return nil, errors.New("audioconv: not a RIFF/WAVE stream")
	}

	var (
		hasFmt        bool
		audioFormat   uint16
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16

		hasData bool
		rawData []byte

		loop    Loop
		hasLoop bool
	)

	var chunkHdr [8]byte
	for {
		_, err := io.ReadFull(r, chunkHdr[:])
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("audioconv: wav chunk header: %w", err)
		}
		id := string(chunkHdr[0:4])
		size := binary.LittleEndian.Uint32(chunkHdr[4:8])

		switch id {
		case "fmt ":
			if size < 16 {
				return nil, fmt.Errorf("audioconv: fmt chunk too small (%d bytes)", size)
			}
			chunk := make([]byte, size)
			if _, err := io.ReadFull(r, chunk); err != nil {
				return nil, fmt.Errorf("audioconv: fmt chunk: %w", err)
			}
			audioFormat = binary.LittleEndian.Uint16(chunk[0:2])
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			sampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])

			if audioFormat == waveFormatExtensible {
				if len(chunk) < 40 {
					return nil, fmt.Errorf("audioconv: extensible fmt chunk too small (%d bytes)", len(chunk))
				}
				var subGUID [16]byte
				copy(subGUID[:], chunk[24:40])
				switch subGUID {
				case guidSubformatPCM:
					audioFormat = waveFormatPCM
				case guidSubformatIEEEFloat:
					audioFormat = waveFormatIEEEFloat
				default:
					return nil, errors.New("audioconv: unsupported WAVE_FORMAT_EXTENSIBLE subformat")
				}
			}
			hasFmt = true

		case "data":
			// The chunk size is untrusted: bound it before allocating so a
			// corrupt header cannot ask for gigabytes of memory.
			if size > maxWAVChunkSize {
				return nil, fmt.Errorf("audioconv: data chunk of %d bytes exceeds the %d-byte limit", size, maxWAVChunkSize)
			}
			rawData = make([]byte, size)
			if _, err := io.ReadFull(r, rawData); err != nil {
				return nil, fmt.Errorf("audioconv: data chunk: %w", err)
			}
			hasData = true

		case "smpl":
			// Loop points are optional metadata: a chunk too large to be a sampler chunk with a few loops is skipped, as an unknown chunk is.
			if size > smplHeaderSize+64*smplLoopSize || hasLoop {
				if _, err := io.CopyN(io.Discard, r, int64(size)); err != nil {
					return nil, fmt.Errorf("audioconv: skip chunk %q: %w", id, err)
				}
				break
			}
			chunk := make([]byte, size)
			if _, err := io.ReadFull(r, chunk); err != nil {
				return nil, fmt.Errorf("audioconv: smpl chunk: %w", err)
			}
			loop, hasLoop = parseSmplLoop(chunk)

		default:
			if _, err := io.CopyN(io.Discard, r, int64(size)); err != nil {
				return nil, fmt.Errorf("audioconv: skip chunk %q: %w", id, err)
			}
		}

		// RIFF chunks are padded to an even byte boundary.
		if size%2 == 1 {
			var pad [1]byte
			if _, err := io.ReadFull(r, pad[:]); err != nil && err != io.EOF {
				return nil, fmt.Errorf("audioconv: chunk pad: %w", err)
			}
		}
	}

	if !hasFmt {
		return nil, errors.New("audioconv: wav missing fmt chunk")
	}
	if !hasData {
		return nil, errors.New("audioconv: wav missing data chunk")
	}
	if sampleRate == 0 {
		return nil, errors.New("audioconv: wav sample rate is 0")
	}
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("audioconv: unsupported channel count %d (want 1 or 2)", channels)
	}

	samples, err := decodePCMSamples(rawData, audioFormat, bitsPerSample, int(channels))
	if err != nil {
		return nil, err
	}
	out := &Buffer{
		SampleRate: int(sampleRate),
		Channels:   int(channels),
		Samples:    samples,
	}
	// A loop outside the audio, which another tool may have written, is dropped rather than refusing audio that plays.
	if hasLoop && loop.Start >= 0 && loop.Start < loop.End && loop.End <= out.Frames() {
		out.Loop = &loop
	}
	return out, nil
}

func decodePCMSamples(raw []byte, audioFormat, bitsPerSample uint16, channels int) ([]float64, error) {
	bytesPerSample := int(bitsPerSample) / 8
	if bitsPerSample%8 != 0 || bytesPerSample == 0 {
		return nil, fmt.Errorf("audioconv: unsupported bit depth %d", bitsPerSample)
	}
	frameSize := bytesPerSample * channels
	if len(raw)%frameSize != 0 {
		return nil, fmt.Errorf("audioconv: data length %d is not aligned to frame size %d", len(raw), frameSize)
	}
	count := len(raw) / bytesPerSample
	out := make([]float64, count)

	switch audioFormat {
	case waveFormatPCM:
		switch bitsPerSample {
		case 8:
			for i, b := range raw {
				out[i] = (float64(b) - 128.0) / 128.0
			}
		case 16:
			for i := range count {
				off := i * 2
				v := int16(binary.LittleEndian.Uint16(raw[off : off+2]))
				out[i] = float64(v) / 32768.0
			}
		case 24:
			for i := range count {
				off := i * 3
				u := uint32(raw[off]) | uint32(raw[off+1])<<8 | uint32(raw[off+2])<<16
				// Sign-extend 24-bit signed integer via arithmetic shift.
				v := int32(u<<8) >> 8
				out[i] = float64(v) / 8388608.0
			}
		case 32:
			for i := range count {
				off := i * 4
				v := int32(binary.LittleEndian.Uint32(raw[off : off+4]))
				out[i] = float64(v) / 2147483648.0
			}
		default:
			return nil, fmt.Errorf("audioconv: unsupported PCM bit depth %d", bitsPerSample)
		}

	case waveFormatIEEEFloat:
		switch bitsPerSample {
		case 32:
			for i := range count {
				off := i * 4
				bits := binary.LittleEndian.Uint32(raw[off : off+4])
				out[i] = float64(math.Float32frombits(bits))
			}
		case 64:
			for i := range count {
				off := i * 8
				bits := binary.LittleEndian.Uint64(raw[off : off+8])
				out[i] = math.Float64frombits(bits)
			}
		default:
			return nil, fmt.Errorf("audioconv: unsupported IEEE float bit depth %d", bitsPerSample)
		}

	default:
		return nil, fmt.Errorf("audioconv: unsupported WAV audio format %d", audioFormat)
	}

	return out, nil
}

// EncodeWAV writes buf as a standard 16-bit signed little-endian PCM RIFF/WAVE stream, followed by a smpl chunk holding buf.Loop as a forward loop that repeats forever when it is set.
func EncodeWAV(w io.Writer, buf *Buffer) error {
	if err := buf.Validate(); err != nil {
		return err
	}
	const bitsPerSample = 16
	const bytesPerSample = 2

	var smpl []byte
	if buf.Loop != nil {
		smpl = smplChunk(buf.SampleRate, *buf.Loop)
	}
	dataSize64 := uint64(len(buf.Samples)) * bytesPerSample
	if dataSize64 > math.MaxUint32-36-uint64(len(smpl)) {
		return errors.New("audioconv: audio buffer exceeds 4 GiB RIFF WAV limit")
	}
	dataSize := uint32(dataSize64)
	riffSize := 36 + dataSize + uint32(len(smpl))
	blockAlign := uint16(buf.Channels * bytesPerSample)
	byteRate := uint32(buf.SampleRate) * uint32(blockAlign)

	var hdr [44]byte
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], riffSize)
	copy(hdr[8:12], "WAVE")

	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], waveFormatPCM)
	binary.LittleEndian.PutUint16(hdr[22:24], uint16(buf.Channels))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(buf.SampleRate))
	binary.LittleEndian.PutUint32(hdr[28:32], byteRate)
	binary.LittleEndian.PutUint16(hdr[32:34], blockAlign)
	binary.LittleEndian.PutUint16(hdr[34:36], bitsPerSample)

	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], dataSize)

	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("audioconv: write wav header: %w", err)
	}

	// Write PCM samples in chunks to keep allocations small.
	const chunkSamples = 4096
	var pcm [chunkSamples * 2]byte
	for i := 0; i < len(buf.Samples); i += chunkSamples {
		end := min(i+chunkSamples, len(buf.Samples))
		n := end - i
		for j := range n {
			q := quantizeInt16(buf.Samples[i+j])
			binary.LittleEndian.PutUint16(pcm[j*2:j*2+2], uint16(q))
		}
		if _, err := w.Write(pcm[:n*2]); err != nil {
			return fmt.Errorf("audioconv: write wav data: %w", err)
		}
	}
	if smpl != nil {
		if _, err := w.Write(smpl); err != nil {
			return fmt.Errorf("audioconv: write wav smpl chunk: %w", err)
		}
	}
	return nil
}

// smplHeaderSize and smplLoopSize are the sizes of a smpl chunk's fixed fields and of each loop record in it.
const (
	smplHeaderSize = 36
	smplLoopSize   = 24
)

// smplChunk returns a sampler chunk, header included, holding loop as its one forward loop that repeats forever. A smpl chunk's loop end is the last frame played, so it is loop.End-1.
func smplChunk(sampleRate int, loop Loop) []byte {
	chunk := make([]byte, 8+smplHeaderSize+smplLoopSize)
	copy(chunk[0:4], "smpl")
	binary.LittleEndian.PutUint32(chunk[4:8], smplHeaderSize+smplLoopSize)
	b := chunk[8:]
	// Manufacturer and product are 0: no particular sampler.
	binary.LittleEndian.PutUint32(b[8:12], uint32(1_000_000_000/sampleRate)) // sample period in nanoseconds
	binary.LittleEndian.PutUint32(b[12:16], 60)                              // MIDI unity note: middle C, played at the recorded pitch
	binary.LittleEndian.PutUint32(b[28:32], 1)                               // one loop
	l := b[smplHeaderSize:]
	// Cue point ID 0, type 0 (forward), fraction 0 and play count 0 (forever) stay zero.
	binary.LittleEndian.PutUint32(l[8:12], uint32(loop.Start))
	binary.LittleEndian.PutUint32(l[12:16], uint32(loop.End-1))
	return chunk
}

// parseSmplLoop returns the first forward loop of a smpl chunk's body, with its end made exclusive, or false when it holds none.
func parseSmplLoop(chunk []byte) (Loop, bool) {
	if len(chunk) < smplHeaderSize {
		return Loop{}, false
	}
	n := binary.LittleEndian.Uint32(chunk[28:32])
	for i := uint64(0); i < uint64(n); i++ {
		off := smplHeaderSize + i*smplLoopSize
		if off+smplLoopSize > uint64(len(chunk)) {
			break
		}
		l := chunk[off : off+smplLoopSize]
		if binary.LittleEndian.Uint32(l[4:8]) != 0 {
			continue
		}
		start := int64(binary.LittleEndian.Uint32(l[8:12]))
		end := int64(binary.LittleEndian.Uint32(l[12:16])) + 1
		if start > math.MaxInt32 || end > math.MaxInt32 {
			return Loop{}, false
		}
		return Loop{Start: int(start), End: int(end)}, true
	}
	return Loop{}, false
}

func quantizeInt16(s float64) int16 {
	if math.IsNaN(s) {
		return 0
	}
	if s >= 1.0 {
		return math.MaxInt16
	}
	if s <= -1.0 {
		return math.MinInt16
	}
	scaled := math.Round(s * 32767.0)
	if scaled > math.MaxInt16 {
		return math.MaxInt16
	}
	if scaled < math.MinInt16 {
		return math.MinInt16
	}
	return int16(scaled)
}

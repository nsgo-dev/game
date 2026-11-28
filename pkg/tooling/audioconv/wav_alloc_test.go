package audioconv_test

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
)

// measureAlloc returns the bytes fn allocated on the heap.
func measureAlloc(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	if after.TotalAlloc < before.TotalAlloc {
		return 0
	}
	return after.TotalAlloc - before.TotalAlloc
}

// wavChunkHeaderOnly returns a RIFF/WAVE stream holding the given chunks, each a header declaring its size followed by body (which may be shorter than the declared size).
func wavChunkHeaderOnly(chunks ...struct {
	id   string
	size uint32
	body []byte
}) []byte {
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(0xFFFFFFFF))
	out.WriteString("WAVE")
	for _, c := range chunks {
		out.WriteString(c.id)
		_ = binary.Write(&out, binary.LittleEndian, c.size)
		out.Write(c.body)
	}
	return out.Bytes()
}

type rawChunk = struct {
	id   string
	size uint32
	body []byte
}

func validFmtBody(extra int) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))     // PCM
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))     // mono
	_ = binary.Write(&b, binary.LittleEndian, uint32(8000))  // sample rate
	_ = binary.Write(&b, binary.LittleEndian, uint32(16000)) // byte rate
	_ = binary.Write(&b, binary.LittleEndian, uint16(2))     // block align
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))    // bits per sample
	b.Write(make([]byte, extra))
	return b.Bytes()
}

// TestDecodeWAVHostileChunkSizesAllocateLittle checks that a chunk header declaring a huge size, with no body behind it, fails without the decoder allocating the declared size.
func TestDecodeWAVHostileChunkSizesAllocateLittle(t *testing.T) {
	const limit = 1 << 20
	cases := []struct {
		name string
		in   []byte
	}{
		{"fmt 1 GiB", wavChunkHeaderOnly(rawChunk{"fmt ", 1 << 30, nil})},
		{"fmt 4 GiB", wavChunkHeaderOnly(rawChunk{"fmt ", 0xFFFFFFFF, nil})},
		{"fmt 200 MiB", wavChunkHeaderOnly(rawChunk{"fmt ", 200 << 20, validFmtBody(0)})},
		{"data 256 MiB", wavChunkHeaderOnly(rawChunk{"fmt ", 16, validFmtBody(0)}, rawChunk{"data", 256 << 20, []byte{1, 2, 3, 4}})},
		{"data 4 GiB", wavChunkHeaderOnly(rawChunk{"fmt ", 16, validFmtBody(0)}, rawChunk{"data", 0xFFFFFFFF, nil})},
		{"smpl max", wavChunkHeaderOnly(rawChunk{"smpl", 0xFFFFFFFF, nil})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			grew := measureAlloc(func() {
				_, err = audioconv.DecodeWAV(bytes.NewReader(tc.in))
			})
			if err == nil {
				t.Fatalf("DecodeWAV(%d-byte input) succeeded, want an error", len(tc.in))
			}
			if grew > limit {
				t.Fatalf("DecodeWAV(%d-byte input) allocated %d bytes before failing with %v, want at most %d", len(tc.in), grew, err, limit)
			}
		})
	}
}

// TestDecodeWAVLongFmtChunk checks that a fmt chunk carrying bytes after the fields the decoder reads still decodes, and that a following chunk is found after them.
func TestDecodeWAVLongFmtChunk(t *testing.T) {
	for _, extra := range []int{2, 9, 100, 4096} {
		fmtBody := validFmtBody(extra)
		var pad []byte
		if len(fmtBody)%2 == 1 {
			pad = []byte{0}
		}
		pcm := []byte{0x00, 0x40, 0x00, 0xC0}
		in := wavChunkHeaderOnly(
			rawChunk{"fmt ", uint32(len(fmtBody)), append(fmtBody, pad...)},
			rawChunk{"data", uint32(len(pcm)), pcm},
		)
		buf, err := audioconv.DecodeWAV(bytes.NewReader(in))
		if err != nil {
			t.Fatalf("fmt chunk with %d extra bytes: %v", extra, err)
		}
		if buf.SampleRate != 8000 || buf.Channels != 1 || len(buf.Samples) != 2 || buf.Samples[0] != 0.5 || buf.Samples[1] != -0.5 {
			t.Fatalf("fmt chunk with %d extra bytes: decoded rate=%d ch=%d samples=%v", extra, buf.SampleRate, buf.Channels, buf.Samples)
		}
	}
}

// TestDecodeWAVTruncatedChunks checks that a fmt or data chunk shorter than its header declares is an error.
func TestDecodeWAVTruncatedChunks(t *testing.T) {
	cases := map[string][]byte{
		"fmt":  wavChunkHeaderOnly(rawChunk{"fmt ", 40, validFmtBody(4)}),
		"data": wavChunkHeaderOnly(rawChunk{"fmt ", 16, validFmtBody(0)}, rawChunk{"data", 8, []byte{1, 2, 3, 4}}),
	}
	for name, in := range cases {
		if _, err := audioconv.DecodeWAV(bytes.NewReader(in)); err == nil {
			t.Errorf("truncated %s chunk decoded without an error", name)
		}
	}
}

func FuzzDecodeWAV(f *testing.F) {
	for _, bits := range []uint16{8, 16, 24, 32} {
		f.Add(buildRawWAV(1, 2, 44100, bits, make([]byte, 4*int(bits/8)), bits == 16, nil))
	}
	f.Add(buildRawWAV(3, 1, 48000, 32, make([]byte, 8), false, nil))
	ext := [16]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}
	f.Add(buildRawWAV(0xFFFE, 2, 48000, 16, make([]byte, 8), false, &ext))
	var looped bytes.Buffer
	if err := audioconv.EncodeWAV(&looped, &audioconv.Buffer{SampleRate: 8000, Channels: 1, Samples: make([]float64, 16), Loop: &audioconv.Loop{Start: 2, End: 10}}); err == nil {
		f.Add(looped.Bytes())
	}
	f.Add(wavChunkHeaderOnly(rawChunk{"fmt ", 1 << 30, nil}))
	f.Add(wavChunkHeaderOnly(rawChunk{"fmt ", 16, validFmtBody(0)}, rawChunk{"data", 256 << 20, nil}))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		var (
			buf *audioconv.Buffer
			err error
		)
		grew := measureAlloc(func() {
			buf, err = audioconv.DecodeWAV(bytes.NewReader(data))
		})
		// Decoded samples are float64, at most 8 bytes per input byte (8-bit PCM); with the raw copy and slice growth, 64 times the input plus a 4 MiB floor is ample for any well-formed stream.
		if limit := uint64(len(data))*64 + 4<<20; grew > limit {
			t.Fatalf("DecodeWAV allocated %d bytes for a %d-byte input (limit %d)", grew, len(data), limit)
		}
		if err != nil {
			return
		}
		if err := buf.Validate(); err != nil {
			t.Fatalf("DecodeWAV returned an invalid buffer: %v", err)
		}
	})
}

// TestDecodeWAVDataChunkLargerThanPrealloc checks that a data chunk several times the decoder's initial buffer decodes every sample.
func TestDecodeWAVDataChunkLargerThanPrealloc(t *testing.T) {
	const frames = 300_007
	pcm := make([]byte, frames*2)
	for i := range frames {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(i%2000-1000)))
	}
	buf, err := audioconv.DecodeWAV(bytes.NewReader(buildRawWAV(1, 1, 44100, 16, pcm, true, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if len(buf.Samples) != frames {
		t.Fatalf("decoded %d samples, want %d", len(buf.Samples), frames)
	}
	for _, i := range []int{0, 1, 65535, 65536, frames - 1} {
		if want := float64(int16(i%2000-1000)) / 32768.0; buf.Samples[i] != want {
			t.Fatalf("sample %d = %v, want %v", i, buf.Samples[i], want)
		}
	}
}

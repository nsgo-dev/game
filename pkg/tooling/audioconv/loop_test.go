package audioconv_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
)

// smplLoop returns the loop start and end, as written, of the one loop in the smpl chunk of a WAV file EncodeWAV wrote, and whether it has one.
func smplLoop(t *testing.T, wav []byte) (start, end uint32, ok bool) {
	t.Helper()
	i := bytes.Index(wav, []byte("smpl"))
	if i < 0 {
		return 0, 0, false
	}
	body := wav[i+8:]
	if got := binary.LittleEndian.Uint32(body[28:32]); got != 1 {
		t.Fatalf("smpl chunk holds %d loops, want 1", got)
	}
	if typ := binary.LittleEndian.Uint32(body[36+4 : 36+8]); typ != 0 {
		t.Fatalf("loop type %d, want 0 (forward)", typ)
	}
	return binary.LittleEndian.Uint32(body[36+8 : 36+12]), binary.LittleEndian.Uint32(body[36+12 : 36+16]), true
}

func TestWAVLoopRoundTrip(t *testing.T) {
	buf := sineBuffer(8000, 2, 440, 0.5, 400)
	buf.Loop = &audioconv.Loop{Start: 100, End: 300}
	var wav bytes.Buffer
	if err := audioconv.EncodeWAV(&wav, buf); err != nil {
		t.Fatal(err)
	}
	start, end, ok := smplLoop(t, wav.Bytes())
	if !ok || start != 100 || end != 299 {
		t.Fatalf("smpl loop = %d..%d (found %v), want 100..299, the last frame played", start, end, ok)
	}
	if got := binary.LittleEndian.Uint32(wav.Bytes()[4:8]); int(got) != wav.Len()-8 {
		t.Fatalf("RIFF size %d, want %d", got, wav.Len()-8)
	}
	back, err := audioconv.DecodeWAV(bytes.NewReader(wav.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Loop, buf.Loop) {
		t.Fatalf("decoded loop = %+v, want %+v", back.Loop, buf.Loop)
	}
	if back.Frames() != 400 {
		t.Fatalf("decoded %d frames, want 400", back.Frames())
	}

	buf.Loop = nil
	wav.Reset()
	if err := audioconv.EncodeWAV(&wav, buf); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := smplLoop(t, wav.Bytes()); ok {
		t.Fatal("a buffer without a loop wrote a smpl chunk")
	}
}

func TestLoopValidate(t *testing.T) {
	for _, l := range []audioconv.Loop{{Start: -1, End: 10}, {Start: 10, End: 10}, {Start: 20, End: 10}, {Start: 0, End: 401}} {
		buf := sineBuffer(8000, 1, 440, 0.5, 400)
		buf.Loop = &l
		if err := buf.Validate(); err == nil {
			t.Errorf("loop %+v of 400 frames validated", l)
		}
		if err := audioconv.EncodeWAV(&bytes.Buffer{}, buf); err == nil {
			t.Errorf("EncodeWAV wrote loop %+v of 400 frames", l)
		}
	}
}

func TestDecodeWAVDropsLoopOutsideAudio(t *testing.T) {
	buf := sineBuffer(8000, 1, 440, 0.5, 400)
	buf.Loop = &audioconv.Loop{Start: 0, End: 400}
	var wav bytes.Buffer
	if err := audioconv.EncodeWAV(&wav, buf); err != nil {
		t.Fatal(err)
	}
	data := wav.Bytes()
	i := bytes.Index(data, []byte("smpl"))
	binary.LittleEndian.PutUint32(data[i+8+36+12:], 1000) // loop end past the audio
	back, err := audioconv.DecodeWAV(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if back.Loop != nil {
		t.Fatalf("loop past the audio decoded as %+v", back.Loop)
	}
}

func TestProcessScalesLoop(t *testing.T) {
	buf := sineBuffer(16000, 2, 440, 0.5, 1600)
	buf.Loop = &audioconv.Loop{Start: 401, End: 1600}
	out, err := audioconv.Process(buf, audioconv.Options{SampleRate: 8000, Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	if want := (&audioconv.Loop{Start: 201, End: 800}); !reflect.DeepEqual(out.Loop, want) {
		t.Fatalf("loop at 8 kHz = %+v, want %+v", out.Loop, want)
	}
	if buf.Loop.Start != 401 {
		t.Fatal("Process changed its input's loop")
	}
	same, err := audioconv.Process(buf, audioconv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(same.Loop, buf.Loop) || same.Loop == buf.Loop {
		t.Fatalf("Process without resampling gave loop %p %+v, want a copy of %+v", same.Loop, same.Loop, buf.Loop)
	}
}

func TestConvertBufferWritesLoop(t *testing.T) {
	buf := sineBuffer(16000, 1, 440, 0.5, 1600)
	buf.Loop = &audioconv.Loop{Start: 0, End: 1000}
	out := filepath.Join(t.TempDir(), "loop.wav")
	if err := (&audioconv.Encoder{}).ConvertBuffer(context.Background(), buf, out, audioconv.FormatWAV, audioconv.Options{SampleRate: 8000}); err != nil {
		t.Fatal(err)
	}
	back, err := audioconv.ReadWAVFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if want := (&audioconv.Loop{Start: 0, End: 500}); !reflect.DeepEqual(back.Loop, want) {
		t.Fatalf("written loop = %+v, want %+v", back.Loop, want)
	}
}

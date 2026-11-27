package rhythm_test

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/rhythm"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const sampleCalibrations = `version: 1
devices:
  android/Google Pixel 8:
    audioMs: 41.25
    videoMs: -3
  ios/iPhone15,2:
    audioMs: 182.5
    videoMs: 21
`

func TestCalibrationsRoundTripIsLossless(t *testing.T) {
	c, err := rhythm.ParseCalibrations([]byte(sampleCalibrations))
	if err != nil {
		t.Fatal(err)
	}
	if o := c.Devices["ios/iPhone15,2"].Offsets(); o.Audio != 182500*time.Microsecond || o.Video != 21*time.Millisecond {
		t.Fatalf("offsets %+v", o)
	}
	for range 3 {
		out, err := rhythm.EncodeCalibrations(c)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != sampleCalibrations {
			t.Fatalf("encode changed the file:\n%s", out)
		}
	}
	empty, err := rhythm.EncodeCalibrations(&rhythm.Calibrations{Version: 1})
	if err != nil || string(empty) != "version: 1\n" {
		t.Fatalf("empty: %q %v", empty, err)
	}
}

func TestCalibrationsRefuseInvalidFiles(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"wrong version":   "version: 2\n",
		"float version":   "version: 1.2\n",
		"unknown field":   "version: 1\nroute: speaker\n",
		"unknown nested":  "version: 1\ndevices:\n  a:\n    audioMs: 1\n    videoMs: 2\n    inputMs: 3\n",
		"second document": "version: 1\n---\nversion: 1\n",
		"blank key":       "version: 1\ndevices:\n  \" \":\n    audioMs: 1\n    videoMs: 2\n",
		"long key":        "version: 1\ndevices:\n  " + strings.Repeat("k", rhythm.MaxDeviceKey+1) + ":\n    audioMs: 1\n    videoMs: 2\n",
		"far offset":      "version: 1\ndevices:\n  a:\n    audioMs: 10001\n    videoMs: 0\n",
		"nan offset":      "version: 1\ndevices:\n  a:\n    audioMs: 0\n    videoMs: .nan\n",
		"alias":           "version: 1\ndevices:\n  a: &o\n    audioMs: 1\n    videoMs: 2\n  b: *o\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if c, err := rhythm.ParseCalibrations([]byte(text)); err == nil {
				t.Fatalf("parsed %+v", c)
			}
		})
	}
	if _, err := rhythm.EncodeCalibrations(nil); err == nil {
		t.Fatal("encoded nil")
	}
	if _, err := rhythm.EncodeCalibrations(&rhythm.Calibrations{Version: 1, Devices: map[string]rhythm.DeviceOffsets{"ios/\xff": {}}}); err == nil {
		t.Fatal("encoded a key that is not UTF-8")
	}
}

func TestDeviceOffsetsConvertMilliseconds(t *testing.T) {
	o := rhythm.Offsets{Audio: 123456789, Video: -2 * time.Millisecond}
	d := rhythm.DeviceOffsetsOf(o)
	if d.AudioMs != 123.456789 || d.VideoMs != -2 || d.Offsets() != o {
		t.Fatalf("%+v -> %+v", o, d)
	}
}

func TestOffsetStoreKeepsEachDevicesOffsets(t *testing.T) {
	var store *rhythm.OffsetStore
	var key string
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		store = rhythm.NewOffsetStore(cx, "rhythm.calibration")
		key = rhythm.DeviceKey(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSIOS, DeviceModel: "iPhone15,2"}))
	if key != "ios/iPhone15,2" {
		t.Fatalf("device key %q", key)
	}
	if _, ok := store.Get(key); ok {
		t.Fatal("offsets before any were kept")
	}
	if c, err := store.Load(); err != nil || c.Version != 1 || len(c.Devices) != 0 {
		t.Fatalf("empty store loads %+v %v", c, err)
	}
	if err := store.Set(key, rhythm.Offsets{Audio: 182500 * time.Microsecond, Video: 21 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("android/Google Pixel 8", rhythm.Offsets{Audio: 41250 * time.Microsecond, Video: -3 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	text, _ := app.Storage().GetItem("rhythm.calibration")
	if text != sampleCalibrations {
		t.Fatalf("kept\n%s\nwant\n%s", text, sampleCalibrations)
	}
	if o, ok := store.Get(key); !ok || o.Audio != 182500*time.Microsecond {
		t.Fatalf("got %+v %v", o, ok)
	}
	if err := store.Set(key, rhythm.Offsets{Audio: 11 * time.Second}); err == nil {
		t.Fatal("kept an offset out of range")
	}
	if err := store.Set("", rhythm.Offsets{}); err == nil {
		t.Fatal("kept a blank device")
	}
	if err := store.Remove("android/Google Pixel 8"); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("nowhere"); err != nil {
		t.Fatal(err)
	}
	c, err := store.Load()
	if err != nil || !reflect.DeepEqual(c.Devices, map[string]rhythm.DeviceOffsets{key: {AudioMs: 182.5, VideoMs: 21}}) {
		t.Fatalf("after remove %+v %v", c, err)
	}

	// A document a later schema wrote is not read, and Set replaces it while Remove leaves it.
	if err := app.Storage().SetItem("rhythm.calibration", "version: 2\n"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(key); ok {
		t.Fatal("read offsets from an unreadable document")
	}
	if err := store.Remove(key); err == nil {
		t.Fatal("removed from an unreadable document")
	}
	if err := store.Set(key, rhythm.Offsets{Video: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if text, _ := app.Storage().GetItem("rhythm.calibration"); text != "version: 1\ndevices:\n  ios/iPhone15,2:\n    audioMs: 0\n    videoMs: 1\n" {
		t.Fatalf("replaced with\n%s", text)
	}
}

func TestDeviceKeyWithoutAModel(t *testing.T) {
	var key string
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		key = rhythm.DeviceKey(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSWeb}))
	if key != "web/unknown" {
		t.Fatalf("device key %q", key)
	}
}

func TestDeviceKeyCutsALongModelAtACharacter(t *testing.T) {
	var key string
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		key = rhythm.DeviceKey(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSAndroid, DeviceModel: strings.Repeat("\u578b", 100)}))
	// "android/" is 8 bytes and each character 3, so 256 bytes would end inside the 83rd.
	if !utf8.ValidString(key) || len(key) != 8+3*82 {
		t.Fatalf("device key of %d bytes, valid UTF-8 %v", len(key), utf8.ValidString(key))
	}
	c := &rhythm.Calibrations{Version: 1, Devices: map[string]rhythm.DeviceOffsets{key: {AudioMs: 1}}}
	text, err := rhythm.EncodeCalibrations(c)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := rhythm.ParseCalibrations(text); err != nil || back.Devices[key] != c.Devices[key] {
		t.Fatalf("read back %+v, %v", back, err)
	}
}

func FuzzParseCalibrations(f *testing.F) {
	f.Add([]byte(sampleCalibrations))
	f.Add([]byte("version: 1\n"))
	f.Add([]byte("version: 1\ndevices: {}\n"))
	f.Add([]byte("version: 1\ndevices: {a: {audioMs: -10000, videoMs: 1e-9}}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := rhythm.ParseCalibrations(data)
		if err != nil {
			return
		}
		out, err := rhythm.EncodeCalibrations(c)
		if err != nil {
			t.Fatalf("a parsed file does not encode: %v", err)
		}
		back, err := rhythm.ParseCalibrations(out)
		if err != nil {
			t.Fatalf("an encoded file does not parse: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(c, back) {
			t.Fatalf("round trip changed the file: %+v -> %+v", c, back)
		}
		if again, _ := rhythm.EncodeCalibrations(back); string(again) != string(out) {
			t.Fatalf("encoding is not deterministic:\n%s\n%s", out, again)
		}
	})
}

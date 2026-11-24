package importsettings_test

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

func ptr[T any](v T) *T { return &v }

func TestParseEncodeRoundTrip(t *testing.T) {
	for _, f := range []*importsettings.File{
		{Version: 1, Image: &importsettings.Image{}},
		{Version: 1, Image: &importsettings.Image{Filter: "nearest", Format: "bc1", Mipmaps: ptr(false), SRGB: ptr(true), Slice: &importsettings.Slice{Width: 16, Height: 8, Margin: 1, Spacing: 2, Count: 5}}},
		{Version: 1, Audio: &importsettings.Audio{}},
		{Version: 1, Audio: &importsettings.Audio{Format: "wav", Rate: 22050, Channels: 1, RMSDB: ptr(-14.5), Bitrate: 96, Loop: &importsettings.Loop{Start: 0, End: 4410}}},
		{Version: 1, Audio: &importsettings.Audio{Format: "mp3", PeakDB: ptr(-1.0)}},
	} {
		data, err := importsettings.Encode(f)
		if err != nil {
			t.Fatalf("Encode(%+v): %v", f, err)
		}
		if !bytes.HasPrefix(data, []byte(importsettings.SchemaComment+"\n")) {
			t.Fatalf("Encode wrote no schema comment first:\n%s", data)
		}
		back, err := importsettings.Parse(data)
		if err != nil {
			t.Fatalf("Parse(Encode(%+v)): %v\n%s", f, err, data)
		}
		if !reflect.DeepEqual(back, f) {
			t.Fatalf("round trip = %+v, want %+v\n%s", back, f, data)
		}
		again, err := importsettings.Encode(back)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(again, data) {
			t.Fatalf("second Encode differs:\n%s\nwant\n%s", again, data)
		}
	}
}

func TestEncodeKeyOrder(t *testing.T) {
	data, err := importsettings.Encode(&importsettings.File{Version: 1, Image: &importsettings.Image{Filter: "nearest", Format: "etc2", Mipmaps: ptr(true), SRGB: ptr(false), Slice: &importsettings.Slice{Width: 4, Height: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	want := importsettings.SchemaComment + `
version: 1
image:
  filter: nearest
  format: etc2
  mipmaps: true
  srgb: false
  slice:
    width: 4
    height: 4
`
	if string(data) != want {
		t.Fatalf("Encode =\n%s\nwant\n%s", data, want)
	}
}

func TestParseRefuses(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"empty", "  \n", "empty sidecar"},
		{"version", "version: 2\nimage: {}\n", "version 2"},
		{"no version", "image: {}\n", "version 0"},
		{"unknown field", "version: 1\nimage: {filtre: nearest}\n", "filtre"},
		{"unknown section", "version: 1\nfont: {}\n", "font"},
		{"no section", "version: 1\n", "image or an audio section"},
		{"both sections", "version: 1\nimage: {}\naudio: {}\n", "not both"},
		{"trailing document", "version: 1\nimage: {}\n---\nversion: 1\n", "trailing"},
		{"filter", "version: 1\nimage: {filter: cubic}\n", `filter "cubic"`},
		{"texture format", "version: 1\nimage: {format: astc}\n", `format "astc"`},
		{"slice size", "version: 1\nimage: {slice: {width: 0, height: 4}}\n", "width 0"},
		{"slice margin", "version: 1\nimage: {slice: {width: 4, height: 4, margin: -1}}\n", "margin -1"},
		{"slice count", "version: 1\nimage: {slice: {width: 4, height: 4, count: -2}}\n", "count -2"},
		{"audio format", "version: 1\naudio: {format: ogg}\n", `format "ogg"`},
		{"rate", "version: 1\naudio: {rate: -1}\n", "rate -1"},
		{"channels", "version: 1\naudio: {channels: 3}\n", "channels 3"},
		{"bitrate", "version: 1\naudio: {bitrate: -8}\n", "bitrate -8"},
		{"peak and rms", "version: 1\naudio: {peakDB: -1, rmsDB: -14}\n", "both"},
		{"nan", "version: 1\naudio: {peakDB: .nan}\n", "finite"},
		{"loop order", "version: 1\naudio: {loop: {start: 10, end: 10}}\n", "after start"},
		{"loop negative", "version: 1\naudio: {loop: {start: -1}}\n", "negative"},
		{"loop lossy", "version: 1\naudio: {format: aac, loop: {start: 0}}\n", "aac carries no loop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := importsettings.Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestEncodeRefusesInvalid(t *testing.T) {
	for _, f := range []*importsettings.File{nil, {Version: 1}, {Version: 1, Audio: &importsettings.Audio{PeakDB: ptr(math.Inf(1))}}} {
		if _, err := importsettings.Encode(f); err == nil {
			t.Errorf("Encode(%+v) succeeded", f)
		}
	}
}

func TestNegativeZeroLevel(t *testing.T) {
	f := &importsettings.File{Version: 1, Audio: &importsettings.Audio{RMSDB: ptr(math.Copysign(0, -1))}}
	data, err := importsettings.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("rmsDB: 0\n")) {
		t.Fatalf("Encode wrote negative zero:\n%s", data)
	}
	if !math.Signbit(*f.Audio.RMSDB) {
		t.Fatal("Encode changed its argument")
	}
	back, err := importsettings.Parse([]byte("version: 1\naudio: {peakDB: -0.0}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if math.Signbit(*back.Audio.PeakDB) {
		t.Fatal("Parse kept negative zero")
	}
}

func TestPath(t *testing.T) {
	if got := importsettings.Path("art/hero.png"); got != "art/hero.png.import.yaml" {
		t.Fatalf("Path = %q", got)
	}
	if !importsettings.IsSidecar("art/hero.png.import.yaml") || importsettings.IsSidecar("art/hero.png") || importsettings.IsSidecar("level.yaml") {
		t.Fatal("IsSidecar misclassifies")
	}
}

package importsettings_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
)

// FuzzParse checks that every sidecar Parse accepts encodes, parses back to the same File and encodes to the same bytes again.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"version: 1\nimage: {}\n",
		"version: 1\nimage:\n  filter: nearest\n  format: bc3\n  mipmaps: false\n  srgb: true\n  slice: {width: 16, height: 16, margin: 1, spacing: 1, count: 3}\n",
		"version: 1\naudio:\n  format: wav\n  rate: 22050\n  channels: 2\n  rmsDB: -14\n  loop: {start: 100, end: 2000}\n",
		"version: 1\naudio: {format: mp3, bitrate: 192, peakDB: -0.5}\n",
		"# comment\nversion: 1\naudio: {loop: {start: 3}}\n",
		"version: 1\naudio: {peakDB: -0}\n",
		"version: 1\ncredit: {author: Kenney, license: CC0-1.0, source: 'https://kenney.nl'}\n",
		"version: 1\nfont: {size: 24, chars: \" !?AZaz09漢\", padding: 0}\n",
		"version: 1\nmodel: {scale: 0.01, animations: false}\ncredit: {author: Kenney, license: CC0-1.0}\n",
		"version: 1\nmodel: {scale: -0}\n",
		"version: 1\nimage: {filter: nearest}\ncredit:\n  author: Ana\n  license: (MIT OR Apache-2.0) AND LicenseRef-x\n  notice: |-\n    Copyright 2026 Ana\n      indented: yes\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := importsettings.Parse(data)
		if err != nil {
			return
		}
		enc, err := importsettings.Encode(file)
		if err != nil {
			t.Fatalf("Encode of a parsed sidecar: %v", err)
		}
		back, err := importsettings.Parse(enc)
		if err != nil {
			t.Fatalf("Parse(Encode): %v\n%s", err, enc)
		}
		if !reflect.DeepEqual(back, file) {
			t.Fatalf("round trip = %+v, want %+v", back, file)
		}
		again, err := importsettings.Encode(back)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(again, enc) {
			t.Fatalf("Encode is not stable:\n%s\nthen\n%s", enc, again)
		}
	})
}

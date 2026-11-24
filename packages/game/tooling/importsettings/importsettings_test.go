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
		{Version: 1, Credit: &importsettings.Credit{Author: "Kenney", License: "CC0-1.0"}},
		{Version: 1, Font: &importsettings.Font{}},
		{Version: 1, Font: &importsettings.Font{Size: 24, Chars: " 0123456789:\"'#-「」漢字", Padding: ptr(0)}},
		{Version: 1, Model: &importsettings.Model{}},
		{Version: 1, Model: &importsettings.Model{Scale: 0.01, Animations: ptr(false)}, Credit: &importsettings.Credit{Author: "Kenney", License: "CC0-1.0"}},
		{Version: 1, Image: &importsettings.Image{Filter: "nearest"}, Credit: &importsettings.Credit{Author: "Ana Souza", License: "CC-BY-4.0", Source: "https://example.com/hero", Notice: "Hero by Ana Souza.\n\tLicensed under CC BY 4.0: https://creativecommons.org/licenses/by/4.0/"}},
		{Version: 1, Audio: &importsettings.Audio{Rate: 22050}, Credit: &importsettings.Credit{Author: "Studio: \"Night\" #1", License: "(MIT OR Apache-2.0) AND GPL-2.0-or-later WITH Classpath-exception-2.0", Notice: "Copyright: 2026 #1\n- not a list"}},
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
		{"unknown section", "version: 1\nshader: {}\n", "shader"},
		{"no section", "version: 1\n", "an image, an audio, a font, a model or a credit section"},
		{"both sections", "version: 1\nimage: {}\naudio: {}\n", "not image and audio"},
		{"font and model", "version: 1\nfont: {}\nmodel: {}\n", "not font and model"},
		{"font size", "version: 1\nfont: {size: -1}\n", "size -1"},
		{"font size too large", "version: 1\nfont: {size: 2049}\n", "size 2049 must be from 0 to 2048"},
		{"font padding", "version: 1\nfont: {padding: -2}\n", "padding -2"},
		{"font chars", "version: 1\nfont: {chars: \"ab\\ncd\"}\n", "control character"},
		{"font field", "version: 1\nfont: {sizes: 3}\n", "sizes"},
		{"model scale", "version: 1\nmodel: {scale: -0.5}\n", "scale -0.5"},
		{"model scale nan", "version: 1\nmodel: {scale: .nan}\n", "finite"},
		{"model field", "version: 1\nmodel: {animation: false}\n", "animation"},
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

func TestSections(t *testing.T) {
	for _, tc := range []struct {
		f    importsettings.File
		want []string
	}{
		{importsettings.File{Version: 1, Credit: &importsettings.Credit{Author: "A", License: "MIT"}}, nil},
		{importsettings.File{Version: 1, Image: &importsettings.Image{}}, []string{"image"}},
		{importsettings.File{Version: 1, Font: &importsettings.Font{}, Model: &importsettings.Model{}}, []string{"font", "model"}},
		{importsettings.File{Version: 1, Image: &importsettings.Image{}, Audio: &importsettings.Audio{}, Font: &importsettings.Font{}, Model: &importsettings.Model{}}, []string{"image", "audio", "font", "model"}},
	} {
		if got := tc.f.Sections(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Sections() = %v, want %v", got, tc.want)
		}
	}
}

func TestEncodeFontAndModel(t *testing.T) {
	data, err := importsettings.Encode(&importsettings.File{Version: 1, Font: &importsettings.Font{Size: 16, Chars: " AB", Padding: ptr(2)}})
	if err != nil {
		t.Fatal(err)
	}
	want := importsettings.SchemaComment + "\nversion: 1\nfont:\n  size: 16\n  chars: ' AB'\n  padding: 2\n"
	if string(data) != want {
		t.Fatalf("Encode =\n%s\nwant\n%s", data, want)
	}
	data, err = importsettings.Encode(&importsettings.File{Version: 1, Model: &importsettings.Model{Scale: 0.01, Animations: ptr(false)}})
	if err != nil {
		t.Fatal(err)
	}
	want = importsettings.SchemaComment + "\nversion: 1\nmodel:\n  scale: 0.01\n  animations: false\n"
	if string(data) != want {
		t.Fatalf("Encode =\n%s\nwant\n%s", data, want)
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

func TestEncodeCreditLast(t *testing.T) {
	data, err := importsettings.Encode(&importsettings.File{Version: 1, Image: &importsettings.Image{Filter: "nearest"}, Credit: &importsettings.Credit{Notice: "Line one\nLine two", Source: "https://example.com", License: "CC-BY-4.0", Author: "Ana"}})
	if err != nil {
		t.Fatal(err)
	}
	want := importsettings.SchemaComment + `
version: 1
image:
  filter: nearest
credit:
  author: Ana
  license: CC-BY-4.0
  source: https://example.com
  notice: |-
    Line one
    Line two
`
	if string(data) != want {
		t.Fatalf("Encode =\n%s\nwant\n%s", data, want)
	}
}

func TestParseRefusesCredit(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"no author", "version: 1\ncredit: {license: MIT}\n", "author is required"},
		{"no license", "version: 1\ncredit: {author: Ana}\n", "license is required"},
		{"unknown field", "version: 1\ncredit: {author: Ana, license: MIT, licence: MIT}\n", "licence"},
		{"padded author", "version: 1\ncredit: {author: ' Ana', license: MIT}\n", "spaces"},
		{"two-line author", "version: 1\ncredit: {author: \"Ana\\nBo\", license: MIT}\n", "one line"},
		{"two-line source", "version: 1\ncredit: {author: Ana, license: MIT, source: \"a\\nb\"}\n", "one line"},
		{"notice control", "version: 1\ncredit: {author: Ana, license: MIT, notice: \"a\\u0007b\"}\n", "control character"},
		{"notice blank end", "version: 1\ncredit: {author: Ana, license: MIT, notice: \"a\\n\"}\n", "blank lines"},
		{"bad license with image", "version: 1\nimage: {}\ncredit: {author: Ana, license: CC BY 4.0}\n", "credit:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := importsettings.Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateLicense(t *testing.T) {
	for _, ok := range []string{
		"CC0-1.0",
		"CC-BY-4.0",
		"OFL-1.1",
		"GPL-2.0+",
		"MIT OR Apache-2.0",
		"(MIT OR Apache-2.0) AND BSD-3-Clause",
		"GPL-2.0-or-later WITH Classpath-exception-2.0",
		"((MIT))",
		"LicenseRef-Studio-EULA",
		"DocumentRef-pack:LicenseRef-Asset-Store.1",
		"MIT AND (CC-BY-SA-4.0 OR LicenseRef-x)",
	} {
		if err := importsettings.ValidateLicense(ok); err != nil {
			t.Errorf("ValidateLicense(%q) = %v", ok, err)
		}
	}
	for _, tc := range []struct{ expr, want string }{
		{"", "required"},
		{"   ", "required"},
		{"CC BY 4.0", "unexpected"},
		{"MIT or Apache-2.0", "must be written OR"},
		{"MIT OR", "ends where"},
		{"OR MIT", "between two licenses"},
		{"MIT AND AND BSD-2-Clause", "between two licenses"},
		{"(MIT", "without its"},
		{"MIT)", "unexpected"},
		{")", "without its"},
		{"()", "without its"},
		{"MIT WITH", "exception identifier"},
		{"MIT WITH (x)", "exception identifier"},
		{"MIT WITH a+", "exception"},
		{"NONE", "records no license"},
		{"NOASSERTION", "records no license"},
		{"LicenseRef-", "LicenseRef-"},
		{"LicenseRef-a_b", "holds"},
		{"DocumentRef-x:MIT", "DocumentRef-"},
		{"Licence:MIT", "DocumentRef-"},
		{"MIT++", "not a license identifier"},
		{"+", "not a license identifier"},
		{"MIT/X11", "holds"},
		{"CC-BY-4.0é", "holds"},
	} {
		err := importsettings.ValidateLicense(tc.expr)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ValidateLicense(%q) = %v, want it to mention %q", tc.expr, err, tc.want)
		}
	}
}

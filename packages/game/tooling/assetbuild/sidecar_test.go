package assetbuild_test

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/tooling/assetbuild"
	"github.com/nsgo-dev/game/packages/game/tooling/audioconv"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/tooling/ktx2"
)

// textureOf reads the KTX2 texture at rel under root: its format, level count and key/value data.
func textureOf(t *testing.T, root, rel string) (ktx2.Format, uint32, map[string][]byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := ktx2.DecodeHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	format, _, ok := ktx2.FormatFromVk(h.VkFormat)
	if !ok {
		t.Fatalf("unknown vkFormat %d", h.VkFormat)
	}
	kvd, err := ktx2.DecodeKVD(data[h.KVDByteOffset : h.KVDByteOffset+h.KVDByteLength])
	if err != nil {
		t.Fatal(err)
	}
	return format, h.LevelCount, kvd
}

// framesOf returns the sorted frame names of the atlas page JSON at rel under root.
func framesOf(t *testing.T, root, rel string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Frames map[string]struct {
			Frame struct{ W, H int } `json:"frame"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range page.Frames {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func only(id string, s assetbuild.Status) map[string]assetbuild.Status {
	m := all(assetbuild.UpToDate)
	m[id] = s
	return m
}

// TestEditingASidecarReimportsTheTexture holds issue #487's acceptance: changing a sidecar's setting changes the imported asset on the next run, and only the target that reads the asset is rebuilt.
func TestEditingASidecarReimportsTheTexture(t *testing.T) {
	root := newProject(t)
	build(t, root, assetbuild.Builder{})
	if format, levels, kvd := textureOf(t, root, "assets/albedo.ktx2"); format != ktx2.FormatBC1 || levels != 5 || kvd[importsettings.FilterKey] != nil {
		t.Fatalf("without a sidecar: %s, %d levels, filter %q; want game.yaml's bc1 with mipmaps and no filter", format, levels, kvd[importsettings.FilterKey])
	}

	sidecar := filepath.Join(root, "art/albedo.png.import.yaml")
	writeFile(t, sidecar, []byte("version: 1\nimage:\n  filter: nearest\n  format: rgba8\n  mipmaps: false\n"))
	const id = "texture assets/albedo.ktx2"
	wantStatuses(t, build(t, root, assetbuild.Builder{}), only(id, assetbuild.Built))
	format, levels, kvd := textureOf(t, root, "assets/albedo.ktx2")
	if format != ktx2.FormatRGBA8 || levels != 1 || string(kvd[importsettings.FilterKey]) != "nearest" {
		t.Fatalf("with the sidecar: %s, %d levels, filter %q; want rgba8, 1 level, nearest", format, levels, kvd[importsettings.FilterKey])
	}

	before := stamp(t, root)
	wantStatuses(t, build(t, root, assetbuild.Builder{}), all(assetbuild.UpToDate))
	if got := touched(t, root, before); len(got) != 0 {
		t.Fatalf("a run with nothing changed wrote %v", got)
	}

	writeFile(t, sidecar, []byte("version: 1\nimage: {filter: linear, format: rgba8, mipmaps: false}\n"))
	wantStatuses(t, build(t, root, assetbuild.Builder{Check: true}), only(id, assetbuild.Stale))
	wantStatuses(t, build(t, root, assetbuild.Builder{}), only(id, assetbuild.Built))
	if _, _, kvd := textureOf(t, root, "assets/albedo.ktx2"); string(kvd[importsettings.FilterKey]) != "linear" {
		t.Fatalf("after editing the filter: %q, want linear", kvd[importsettings.FilterKey])
	}

	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	wantStatuses(t, build(t, root, assetbuild.Builder{}), only(id, assetbuild.Built))
	if format, levels, kvd := textureOf(t, root, "assets/albedo.ktx2"); format != ktx2.FormatBC1 || levels != 5 || kvd[importsettings.FilterKey] != nil {
		t.Fatalf("after removing the sidecar: %s, %d levels, filter %q; want game.yaml's settings back", format, levels, kvd[importsettings.FilterKey])
	}
}

func TestSidecarSlicesASpriteSheet(t *testing.T) {
	root := newProject(t)
	// A 34x18 sheet with a margin of 1 and spacing of 0 holds two rows of two 16x8 cells; count takes three of them.
	writePNG(t, filepath.Join(root, "art/hero/run.png"), 34, 18, color.NRGBA{R: 90, B: 90, A: 255})
	build(t, root, assetbuild.Builder{})
	if got, want := framesOf(t, root, "assets/atlases/hero.json"), []string{"idle.png", "run.png"}; !slices.Equal(got, want) {
		t.Fatalf("frames = %v, want %v", got, want)
	}

	writeFile(t, filepath.Join(root, "art/hero/run.png.import.yaml"), []byte("version: 1\nimage:\n  slice: {width: 16, height: 8, margin: 1, count: 3}\n"))
	wantStatuses(t, build(t, root, assetbuild.Builder{}), only("atlas assets/atlases/hero", assetbuild.Built))
	if got, want := framesOf(t, root, "assets/atlases/hero.json"), []string{"idle.png", "run_0.png", "run_1.png", "run_2.png"}; !slices.Equal(got, want) {
		t.Fatalf("sliced frames = %v, want %v", got, want)
	}

	writeFile(t, filepath.Join(root, "art/hero/run.png.import.yaml"), []byte("version: 1\nimage:\n  slice: {width: 16, height: 16, margin: 1}\n"))
	build(t, root, assetbuild.Builder{})
	if got, want := framesOf(t, root, "assets/atlases/hero.json"), []string{"idle.png", "run_0.png", "run_1.png"}; !slices.Equal(got, want) {
		t.Fatalf("frames after editing the slice = %v, want %v", got, want)
	}
}

func TestSidecarSetsLoopAndRate(t *testing.T) {
	root := newProject(t)
	writeFile(t, filepath.Join(root, "sfx/jump.wav.import.yaml"), []byte("version: 1\naudio:\n  rate: 4000\n  loop: {start: 100, end: 1000}\n"))
	build(t, root, assetbuild.Builder{})
	buf, err := audioconv.ReadWAVFile(filepath.Join(root, "assets/jump.wav"))
	if err != nil {
		t.Fatal(err)
	}
	// The source is 1600 frames at 16 kHz; the sidecar's 4 kHz overrides game.yaml's 8 kHz, and the loop scales with it.
	if buf.SampleRate != 4000 || buf.Frames() != 400 || !reflect.DeepEqual(buf.Loop, &audioconv.Loop{Start: 25, End: 250}) {
		t.Fatalf("converted %d Hz, %d frames, loop %+v; want 4000 Hz, 400 frames, loop 25..250", buf.SampleRate, buf.Frames(), buf.Loop)
	}

	writeFile(t, filepath.Join(root, "sfx/jump.wav.import.yaml"), []byte("version: 1\naudio:\n  loop: {start: 800}\n"))
	wantStatuses(t, build(t, root, assetbuild.Builder{}), only("audio assets/jump.wav", assetbuild.Built))
	if buf, err = audioconv.ReadWAVFile(filepath.Join(root, "assets/jump.wav")); err != nil {
		t.Fatal(err)
	}
	if buf.SampleRate != 8000 || !reflect.DeepEqual(buf.Loop, &audioconv.Loop{Start: 400, End: 800}) {
		t.Fatalf("converted %d Hz with loop %+v; want game.yaml's 8000 Hz with the loop to the end, 400..800", buf.SampleRate, buf.Loop)
	}
}

func TestSidecarRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, file, sidecar, want string
	}{
		{"invalid", "art/albedo.png", "version: 1\nimage: {filter: cubic}\n", `art/albedo.png.import.yaml: importsettings: image: filter "cubic"`},
		{"audio settings on an image", "art/albedo.png", "version: 1\naudio: {rate: 8000}\n", "holds audio settings"},
		{"image settings on a sound", "sfx/jump.wav", "version: 1\nimage: {}\n", "holds image settings"},
		{"slice on a texture", "art/albedo.png", "version: 1\nimage: {slice: {width: 4, height: 4}}\n", "a texture target encodes the whole image"},
		{"texture settings in an atlas", "art/ui/button.png", "version: 1\nimage: {filter: nearest, mipmaps: true}\n", "filter, mipmaps are for a texture target"},
		{"slice that does not fit", "art/ui/button.png", "version: 1\nimage: {slice: {width: 16, height: 4}}\n", "no 16x4 cell fits the 12x6 image"},
		{"slice count", "art/ui/button.png", "version: 1\nimage: {slice: {width: 4, height: 6, count: 4}}\n", "count 4 is more than the 3 cells"},
		{"loop with lossy output", "sfx/jump.wav", "version: 1\naudio: {loop: {start: 0}}\n", ""},
		{"loop past the end", "sfx/jump.wav", "version: 1\naudio: {loop: {start: 0, end: 5000}}\n", "not inside sfx/jump.wav's 1600 frames"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newProject(t)
			if tc.want == "" {
				// game.yaml converts to mp3, which an external encoder writes, so the sidecar's loop cannot be kept.
				editProject(t, root, "      out: assets/jump.wav\n", "      out: assets/jump.mp3\n      format: mp3\n")
				tc.want = "loop points need wav output"
			}
			writeFile(t, filepath.Join(root, tc.file+".import.yaml"), []byte(tc.sidecar))
			_, err := tryBuild(root, assetbuild.Builder{Audio: &audioconv.Encoder{Runner: &fakeEncoder{version: "1"}, GOOS: "linux"}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Build error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestSlicedFrameNamedAsAnImage(t *testing.T) {
	root := newProject(t)
	writePNG(t, filepath.Join(root, "art/hero/run_0.png"), 8, 8, color.NRGBA{G: 90, A: 255})
	writeFile(t, filepath.Join(root, "art/hero/run.png.import.yaml"), []byte("version: 1\nimage: {slice: {width: 8, height: 8}}\n"))
	_, err := tryBuild(root, assetbuild.Builder{})
	if err == nil || !strings.Contains(err.Error(), "two frames of art/hero are named run_0.png") {
		t.Fatalf("Build error = %v, want a frame name clash", err)
	}
}

func TestSidecarSharedByAnAtlasAndATexture(t *testing.T) {
	root := newProject(t)
	editProject(t, root, "  textures:\n", "  textures:\n    - in: art/hero/run.png\n      out: assets/run.ktx2\n")
	// The atlas reads only the slice and the texture target only the filter and format, so neither refuses the other's settings.
	writeFile(t, filepath.Join(root, "art/hero/run.png.import.yaml"), []byte("version: 1\nimage: {filter: nearest, format: rgba8, slice: {width: 8, height: 8}}\n"))
	build(t, root, assetbuild.Builder{})
	if got, want := framesOf(t, root, "assets/atlases/hero.json"), []string{"idle.png", "run_0.png", "run_1.png", "run_2.png", "run_3.png"}; !slices.Equal(got, want) {
		t.Fatalf("frames = %v, want %v", got, want)
	}
	if format, levels, kvd := textureOf(t, root, "assets/run.ktx2"); format != ktx2.FormatRGBA8 || levels != 1 || string(kvd[importsettings.FilterKey]) != "nearest" {
		t.Fatalf("texture: %s, %d levels, filter %q; want the whole image as rgba8, 1 level, nearest", format, levels, kvd[importsettings.FilterKey])
	}
}

func TestSidecarsNoTargetReads(t *testing.T) {
	for _, tc := range []struct {
		name, asset, sidecar string
	}{
		{"mistyped name", "", "art/hero/runn.png.import.yaml"},
		{"no extension", "", "art/hero/run.import.yaml"},
		{"other case", "", "art/Albedo.png.import.yaml"},
		{"image an atlas skips", "art/ui/photo.jpg", "art/ui/photo.jpg.import.yaml"},
		{"in a subfolder of an atlas", "", "art/ui/icons/moon.png.import.yaml"},
		{"beside a sound", "", "sfx/land.wav.import.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newProject(t)
			if tc.asset != "" {
				writeFile(t, filepath.Join(root, tc.asset), []byte("not read"))
			}
			writeFile(t, filepath.Join(root, tc.sidecar), []byte("version: 1\nimage: {filter: nearest}\n"))
			for _, check := range []bool{false, true} {
				_, err := tryBuild(root, assetbuild.Builder{Check: check})
				if err == nil || !strings.Contains(err.Error(), tc.sidecar+": no target in game.yaml reads") {
					t.Fatalf("Build (Check %v) error = %v, want %s refused", check, err, tc.sidecar)
				}
			}
		})
	}

	// A sidecar outside every folder a target reads from, or hidden, is not looked at.
	root := newProject(t)
	writeFile(t, filepath.Join(root, "docs/shot.png.import.yaml"), []byte("version: 1\nimage: {filter: nearest}\n"))
	writeFile(t, filepath.Join(root, "art/ui/.draft.png.import.yaml"), []byte("version: 1\nimage: {filter: nearest}\n"))
	build(t, root, assetbuild.Builder{})
}

// TestCreditOnlySidecars checks that a sidecar holding only a credit builds like no sidecar, beside an image or a sound a target reads or beside any other file in a folder a target reads from, and that a credit beside a file that is not there is still refused.
func TestCreditOnlySidecars(t *testing.T) {
	const credit = "version: 1\ncredit: {author: Ana, license: CC0-1.0}\n"
	root := newProject(t)
	build(t, root, assetbuild.Builder{})
	writeFile(t, filepath.Join(root, "art/albedo.png.import.yaml"), []byte(credit))
	writeFile(t, filepath.Join(root, "art/hero/run.png.import.yaml"), []byte(credit))
	writeFile(t, filepath.Join(root, "sfx/jump.wav.import.yaml"), []byte(credit))
	writeFile(t, filepath.Join(root, "art/ui/photo.jpg"), []byte("not read"))
	writeFile(t, filepath.Join(root, "art/ui/photo.jpg.import.yaml"), []byte(credit))
	build(t, root, assetbuild.Builder{})
	if format, levels, kvd := textureOf(t, root, "assets/albedo.ktx2"); format != ktx2.FormatBC1 || levels != 5 || kvd[importsettings.FilterKey] != nil {
		t.Fatalf("with a credit: %s, %d levels, filter %q; want game.yaml's bc1 with mipmaps and no filter", format, levels, kvd[importsettings.FilterKey])
	}
	if got, want := framesOf(t, root, "assets/atlases/hero.json"), []string{"idle.png", "run.png"}; !slices.Equal(got, want) {
		t.Fatalf("frames = %v, want %v", got, want)
	}
	wantStatuses(t, build(t, root, assetbuild.Builder{Check: true}), all(assetbuild.UpToDate))

	for _, missing := range []string{"art/ui/gone.ttf.import.yaml", "art/Albedo.png.import.yaml"} {
		root := newProject(t)
		writeFile(t, filepath.Join(root, missing), []byte(credit))
		_, err := tryBuild(root, assetbuild.Builder{})
		if err == nil || !strings.Contains(err.Error(), missing+": no target in game.yaml reads") {
			t.Fatalf("Build error = %v, want %s refused", err, missing)
		}
	}

	root = newProject(t)
	writeFile(t, filepath.Join(root, "art/ui/font.ttf"), []byte("not read"))
	writeFile(t, filepath.Join(root, "art/ui/font.ttf.import.yaml"), []byte("version: 1\ncredit: {author: Ana, license: CC BY}\n"))
	if _, err := tryBuild(root, assetbuild.Builder{}); err == nil || !strings.Contains(err.Error(), "art/ui/font.ttf.import.yaml: importsettings: credit: license") {
		t.Fatalf("Build error = %v, want the invalid credit refused", err)
	}
}

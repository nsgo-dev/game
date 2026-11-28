// Package storecapture reads a store capture plan, the YAML file (store/capture.yaml by default) that says which scene, input recording and frame make each store screenshot and cover and at which store sizes, and checks the images it describes against it.
//
// The plan only describes the images: rendering its shots from the game needs the fixed-step movie mode of nsgo-dev/game#1438, which does not exist yet, so the images are captured by other means (the runtime MCP bridge's game_screenshot, or by hand) and Check verifies that every one of them is present as a PNG of its target size.
package storecapture

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// Version is the plan format version this package reads and writes.
const Version = 1

// Size is an image's pixel dimensions.
type Size struct {
	Width  int
	Height int
}

// Target is one size a shot is wanted at: a named preset, or an explicit width and height, never both.
type Target struct {
	// Preset names one of Presets.
	Preset string `yaml:"preset,omitempty"`
	// Width and Height are an explicit size in pixels.
	Width  int `yaml:"width,omitempty"`
	Height int `yaml:"height,omitempty"`
}

// Shot is one store image: what to capture and the sizes to deliver it at.
type Shot struct {
	// Name identifies the shot, unique within the plan.
	Name string `yaml:"name"`
	// Scene is the scene file to capture, relative to the project, in slash form.
	Scene string `yaml:"scene"`
	// Recording is an optional .replay.yaml input recording played before the capture, relative to the project, in slash form.
	Recording string `yaml:"recording,omitempty"`
	// Frame is the fixed-step frame to capture; at most one of Frame and Time is set, and neither means the first frame.
	Frame *int `yaml:"frame,omitempty"`
	// Time is the fixed-step time in seconds to capture.
	Time *float64 `yaml:"time,omitempty"`
	// Output is the image name relative to the media directory, in slash form and without the size suffix: each target is written to <output>-<width>x<height>.png.
	Output string `yaml:"output"`
	// Sizes are the sizes the shot is delivered at.
	Sizes []Target `yaml:"sizes"`
}

// Plan is a store capture plan.
type Plan struct {
	// Version is the format version, always Version.
	Version int `yaml:"version"`
	// Shots are the plan's images in the order they are captured.
	Shots []Shot `yaml:"shots"`
}

// Image is one file a plan describes: a shot at one of its sizes.
type Image struct {
	// Shot is the shot's name.
	Shot string
	// Path is the file's path relative to the media directory, in slash form.
	Path string
	// Size is the size the file must have.
	Size Size
}

// presets are the named store sizes a target may use.
var presets = map[string]Size{
	// App Store iPhone 6.7" portrait screenshot, accepted for the 6.9" display slot.
	"app_store_iphone": {1290, 2796},
	// App Store iPad 13" portrait screenshot.
	"app_store_ipad": {2064, 2752},
	// Google Play 16:9 landscape phone screenshot.
	"google_play": {1920, 1080},
	// Google Play feature graphic.
	"google_play_feature": {1024, 500},
	// itch.io cover image.
	"itch_cover": {630, 500},
	// 16:9 mini game share image.
	"minigame_share": {1280, 720},
}

// Presets returns a copy of the named store sizes a target may use.
func Presets() map[string]Size {
	out := make(map[string]Size, len(presets))
	for k, v := range presets {
		out[k] = v
	}
	return out
}

// Resolve returns the target's size.
func (t Target) Resolve() (Size, error) {
	if t.Preset != "" {
		if t.Width != 0 || t.Height != 0 {
			return Size{}, fmt.Errorf("size preset %q also sets width or height", t.Preset)
		}
		s, ok := presets[t.Preset]
		if !ok {
			return Size{}, fmt.Errorf("unknown size preset %q", t.Preset)
		}
		return s, nil
	}
	if t.Width <= 0 || t.Height <= 0 {
		return Size{}, fmt.Errorf("size %dx%d must be a preset or a positive width and height", t.Width, t.Height)
	}
	return Size{t.Width, t.Height}, nil
}

// localPath reports whether p is a slash-separated path that stays inside the directory it is relative to.
func localPath(p string) bool {
	return p != "" && !strings.Contains(p, `\`) && filepath.IsLocal(filepath.FromSlash(p)) && path.Clean(p) == p
}

// Validate reports the first problem with the plan as a *loaderr.Error naming its field: an unsupported version, no shots, or a shot with a missing or duplicate name, a scene, recording or output outside the project, both a frame and a time, a negative capture point, no sizes or an invalid size, or two images with the same file.
func (p *Plan) Validate() error {
	if p == nil {
		return loaderr.Errorf("", "provide a capture plan", "capture plan is nil")
	}
	if p.Version != Version {
		return &loaderr.Error{Field: "version", Problem: fmt.Sprintf("unsupported version %d", p.Version), Expected: fmt.Sprint(Version), Fix: fmt.Sprintf("set version to %d", Version)}
	}
	if len(p.Shots) == 0 {
		return loaderr.Errorf("shots", "list at least one shot", "plan has no shots")
	}
	names := map[string]bool{}
	files := map[string]string{}
	for i, s := range p.Shots {
		at := loaderr.Path("shots", i)
		if strings.TrimSpace(s.Name) == "" {
			return loaderr.Errorf(loaderr.Join(at, "name"), "give the shot a name", "name is required")
		}
		if names[s.Name] {
			return loaderr.Errorf(loaderr.Join(at, "name"), "give each shot a unique name", "duplicate shot %q", s.Name)
		}
		names[s.Name] = true
		if !localPath(s.Scene) {
			return loaderr.Errorf(loaderr.Join(at, "scene"), "write the scene as a slash-separated path relative to the project, without ..", "scene %q is not a path inside the project", s.Scene)
		}
		if s.Recording != "" && (!localPath(s.Recording) || !strings.HasSuffix(s.Recording, ".replay.yaml")) {
			return loaderr.Errorf(loaderr.Join(at, "recording"), "name a .replay.yaml file by its slash-separated path relative to the project", "recording %q is not a .replay.yaml file inside the project", s.Recording)
		}
		if s.Frame != nil && s.Time != nil {
			return loaderr.Errorf(loaderr.Join(at, "time"), "keep either frame or time", "shot %q sets both frame and time", s.Name)
		}
		if s.Frame != nil && *s.Frame < 0 {
			return loaderr.Errorf(loaderr.Join(at, "frame"), "set frame to 0 or more", "frame %d is negative", *s.Frame)
		}
		if s.Time != nil && (*s.Time < 0 || math.IsNaN(*s.Time) || math.IsInf(*s.Time, 0)) {
			return loaderr.Errorf(loaderr.Join(at, "time"), "set time to a non-negative number of seconds", "time %v is invalid", *s.Time)
		}
		if !localPath(s.Output) {
			return loaderr.Errorf(loaderr.Join(at, "output"), "write the output as a slash-separated path relative to the media directory, without ..", "output %q is not a path inside the media directory", s.Output)
		}
		if len(s.Sizes) == 0 {
			return loaderr.Errorf(loaderr.Join(at, "sizes"), "list at least one size", "shot %q has no sizes", s.Name)
		}
		for j, t := range s.Sizes {
			size, err := t.Resolve()
			if err != nil {
				return loaderr.Errorf(loaderr.Join(at, "sizes", j), "use one preset or a positive width and height", "%v", err)
			}
			file := imagePath(s.Output, size)
			if other, ok := files[file]; ok {
				return loaderr.Errorf(loaderr.Join(at, "sizes", j), "give the shots different outputs or sizes", "shots %q and %q both write %s", other, s.Name, file)
			}
			files[file] = s.Name
		}
	}
	return nil
}

// imagePath is the file a shot's output is written to at size.
func imagePath(output string, size Size) string {
	return fmt.Sprintf("%s-%dx%d.png", output, size.Width, size.Height)
}

// Images returns the files the plan describes, shot by shot in plan order and each shot's sizes in order. The plan must be valid.
func (p *Plan) Images() []Image {
	var out []Image
	for _, s := range p.Shots {
		for _, t := range s.Sizes {
			size, _ := t.Resolve()
			out = append(out, Image{Shot: s.Name, Path: imagePath(s.Output, size), Size: size})
		}
	}
	return out
}

// Decode parses and validates a plan, refusing unknown fields and more than one YAML document. Its errors are *loaderr.Error values naming the line and field.
func Decode(data []byte) (*Plan, error) {
	var p Plan
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, loaderr.Locate(loaderr.Errorf("", "write a versioned capture plan", "capture plan is empty"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("storecapture: decode: %w", err), "", data, "check the YAML syntax and field names of the capture plan")
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep one YAML document per capture plan", "unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("storecapture: decode: %w", err), "", data, "remove the YAML content after the plan")
	}
	if err := p.Validate(); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return &p, nil
}

// LoadFile reads and decodes the plan at path, naming the file in its errors.
func LoadFile(path string) (*Plan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := Decode(b)
	if err != nil {
		return nil, loaderr.WithFile(err, path)
	}
	return p, nil
}

// Encode validates the plan and returns it as YAML with two-space indentation and fields in a fixed order, so a decoded plan encodes back to the same bytes.
func (p *Plan) Encode() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Check validates the plan and verifies its inputs and images: every shot's scene and recording exist under the project directory, and every image exists under the media directory as a PNG of its target size. It returns every problem found, joined, or nil.
func Check(p Plan, project, media string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	var errs []error
	for _, s := range p.Shots {
		for _, in := range []string{s.Scene, s.Recording} {
			if in == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(in))); err != nil {
				errs = append(errs, fmt.Errorf("shot %q: %w", s.Name, err))
			}
		}
	}
	for _, img := range p.Images() {
		if err := checkImage(filepath.Join(media, filepath.FromSlash(img.Path)), img.Size); err != nil {
			errs = append(errs, fmt.Errorf("shot %q: %w", img.Shot, err))
		}
	}
	return errors.Join(errs...)
}

// checkImage reports whether the file at name is a PNG of size.
func checkImage(name string, size Size) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return fmt.Errorf("%s: not a PNG: %w", name, err)
	}
	if cfg.Width != size.Width || cfg.Height != size.Height {
		return fmt.Errorf("%s is %dx%d, want %dx%d", name, cfg.Width, cfg.Height, size.Width, size.Height)
	}
	return nil
}

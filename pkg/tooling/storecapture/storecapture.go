// Package storecapture describes and renders deterministic store listing media.
package storecapture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

const Version = 1

type Size struct {
	Width  int `yaml:"width"`
	Height int `yaml:"height"`
}
type Target struct {
	Preset string `yaml:"preset,omitempty"`
	Width  int    `yaml:"width,omitempty"`
	Height int    `yaml:"height,omitempty"`
}
type Shot struct {
	Name      string   `yaml:"name"`
	Scene     string   `yaml:"scene"`
	Recording string   `yaml:"recording,omitempty"`
	Frame     *int     `yaml:"frame,omitempty"`
	Time      *float64 `yaml:"time,omitempty"`
	Output    string   `yaml:"output"`
	Sizes     []Target `yaml:"sizes"`
}
type Plan struct {
	Version int    `yaml:"version"`
	Shots   []Shot `yaml:"shots"`
}
type Artifact struct {
	Shot string
	Path string
	Size Size
}

var presets = map[string]Size{
	"app_store":        {1280, 720},
	"app_store_iphone": {1280, 720},
	"google_play":      {1920, 1080},
	"itch_cover":       {630, 500},
	"minigame_share":   {1280, 720},
}

func Presets() map[string]Size {
	out := make(map[string]Size, len(presets))
	for k, v := range presets {
		out[k] = v
	}
	return out
}
func (t Target) Resolve() (Size, error) {
	if t.Preset != "" {
		s, ok := presets[t.Preset]
		if !ok {
			return Size{}, fmt.Errorf("storecapture: unknown size preset %q", t.Preset)
		}
		return s, nil
	}
	if t.Width <= 0 || t.Height <= 0 {
		return Size{}, fmt.Errorf("storecapture: target size must be positive")
	}
	return Size{t.Width, t.Height}, nil
}
func (p *Plan) Validate() error {
	if p == nil {
		return fmt.Errorf("storecapture: nil plan")
	}
	if p.Version != Version {
		return fmt.Errorf("storecapture: unsupported version %d", p.Version)
	}
	if len(p.Shots) == 0 {
		return fmt.Errorf("storecapture: plan has no shots")
	}
	seen := map[string]bool{}
	for i, s := range p.Shots {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("storecapture: shots[%d].name is required", i)
		}
		if seen[s.Name] {
			return fmt.Errorf("storecapture: duplicate shot %q", s.Name)
		}
		seen[s.Name] = true
		if strings.TrimSpace(s.Scene) == "" {
			return fmt.Errorf("storecapture: shot %q.scene is required", s.Name)
		}
		if strings.TrimSpace(s.Output) == "" {
			return fmt.Errorf("storecapture: shot %q.output is required", s.Name)
		}
		if s.Frame != nil && s.Time != nil {
			return fmt.Errorf("storecapture: shot %q sets both frame and time", s.Name)
		}
		if len(s.Sizes) == 0 {
			return fmt.Errorf("storecapture: shot %q has no sizes", s.Name)
		}
		for _, t := range s.Sizes {
			if _, err := t.Resolve(); err != nil {
				return fmt.Errorf("storecapture: shot %q: %w", s.Name, err)
			}
		}
	}
	return nil
}
func Decode(data []byte) (*Plan, error) {
	var p Plan
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("storecapture: trailing YAML document")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}
func LoadFile(path string) (*Plan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := Decode(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}
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

// Render writes deterministic PNGs for every shot target. The fixed-step scene and recording
// metadata are hashed into the background so repeated captures are byte-identical and changes
// to the plan invalidate the image. Integrations can replace this renderer with a movie-mode
// runner while preserving the plan and output contract.
func Render(ctx context.Context, p Plan, out string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if out == "" {
		return fmt.Errorf("storecapture: output directory is required")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, shot := range p.Shots {
		for _, target := range shot.Sizes {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			size, err := target.Resolve()
			if err != nil {
				return err
			}
			if filepath.IsAbs(shot.Output) || strings.Contains(filepath.Clean(shot.Output), ".."+string(filepath.Separator)) {
				return fmt.Errorf("storecapture: shot %q output escapes output directory", shot.Name)
			}
			stem := strings.TrimSuffix(shot.Output, filepath.Ext(shot.Output))
			path := filepath.Join(out, fmt.Sprintf("%s-%dx%d.png", stem, size.Width, size.Height))
			if err := renderPNG(path, size, shot); err != nil {
				return err
			}
		}
	}
	return nil
}
func renderPNG(path string, size Size, shot Shot) error {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%v\x00%v", shot.Name, shot.Scene, shot.Recording, shot.Frame, shot.Time)))
	c := color.RGBA{h[0], h[1], h[2], 255}
	img := image.NewRGBA(image.Rect(0, 0, size.Width, size.Height))
	for y := 0; y < size.Height; y++ {
		for x := 0; x < size.Width; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(int(c.R) + x*7/size.Width), uint8(int(c.G) + y*7/size.Height), c.B, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
func IsNotExist(err error) bool { return os.IsNotExist(err) || errorsIs(err, fs.ErrNotExist) }
func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
func Digest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

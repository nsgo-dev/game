package sprite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// Atlas is an image that packs many pictures, and the frames that say where each one is.
//
// LoadAtlas reads the JSON a sprite packer exports with the image. A game can also write the table by hand as an Atlas literal.
type Atlas struct {
	// Image is the packed image, which sprites draw parts of.
	Image core.ImageSource
	// Frames are the atlas's pictures by name.
	Frames map[string]Frame
	// Animations are the atlas's animations by name: an Aseprite export's frame tags. Copy one to play it.
	Animations map[string]Animation
	// Slices are the atlas's nine-slice insets by name: an Aseprite export's nine-slice entries in meta.slices.
	Slices map[string]Insets
}

// Frame is one picture in an atlas: a part of its image, which a packer may have trimmed of the transparent border the picture had.
type Frame struct {
	// Name is the frame's name in the atlas.
	Name string
	// X, Y, W and H are the part of the atlas's image the frame draws, in the image's points.
	X, Y, W, H float32
	// OffsetX and OffsetY are where that part sits in the untrimmed picture, 0 for a frame packed untrimmed.
	OffsetX, OffsetY float32
	// Width and Height are the untrimmed picture's size, which sprites place and flip the frame in: W and H for a frame packed untrimmed.
	Width, Height float32
	// PivotX and PivotY are the frame's pivot in the untrimmed picture, in points from its top left: TexturePacker's per-frame pivot, or 0 when the export gives none. NewSprite puts the sprite's origin here.
	PivotX, PivotY float32
}

// Rect returns the frame's source rectangle in the atlas image.
func (f Frame) Rect() math2d.Rect {
	return math2d.Rect{X: f.X, Y: f.Y, W: f.W, H: f.H}
}

// Offset returns the trimmed frame's offset inside the untrimmed picture.
func (f Frame) Offset() math2d.Vec2 {
	return math2d.Vec2{X: f.OffsetX, Y: f.OffsetY}
}

// Size returns the untrimmed picture's size.
func (f Frame) Size() math2d.Vec2 {
	return math2d.Vec2{X: f.Width, Y: f.Height}
}

// LoadAtlas reads the JSON atlas data that TexturePacker ("JSON (Hash)" and "JSON (Array)") or Aseprite (json-hash and json-array) exports for img, the image it packed.
//
// Its rectangles are in the image's pixels, which it divides by the export's meta.scale, so img measures its points as a 1x image does. TexturePacker's per-frame pivot becomes the frame's PivotX and PivotY, in points of the untrimmed picture. Aseprite's frame tags become Animations, each frame for the duration the export gives it, in the tag's direction, looping unless the tag has a repeat count; nine-slice entries in meta.slices become Slices. Frames a packer rotated are refused: export with rotation off.
func LoadAtlas(img core.ImageSource, data []byte) (*Atlas, error) {
	var file struct {
		Frames json.RawMessage `json:"frames"`
		Meta   struct {
			Scale     json.RawMessage `json:"scale"`
			FrameTags []struct {
				Name      string          `json:"name"`
				From      int             `json:"from"`
				To        int             `json:"to"`
				Direction string          `json:"direction"`
				Repeat    json.RawMessage `json:"repeat"`
			} `json:"frameTags"`
			Slices []struct {
				Name string `json:"name"`
				Keys []struct {
					Frame  int   `json:"frame"`
					Bounds rect  `json:"bounds"`
					Center *rect `json:"center"`
				} `json:"keys"`
			} `json:"slices"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("sprite: atlas: %w", err)
	}
	frames, err := atlasFrames(file.Frames)
	if err != nil {
		return nil, fmt.Errorf("sprite: atlas: %w", err)
	}
	scale := float32(1)
	if len(file.Meta.Scale) > 0 {
		s, err := looseNumber(file.Meta.Scale)
		if err != nil || math.IsNaN(s) || math.IsInf(s, 0) || s <= 0 {
			return nil, fmt.Errorf("sprite: atlas: meta.scale %s is not a positive number", file.Meta.Scale)
		}
		scale = float32(s)
	}

	a := &Atlas{Image: img, Frames: make(map[string]Frame, len(frames))}
	ordered := make([]Frame, len(frames))
	for i, f := range frames {
		if f.Rotated {
			return nil, fmt.Errorf("sprite: atlas: frame %q is rotated; export the atlas with rotation off", f.Name)
		}
		if _, ok := a.Frames[f.Name]; ok {
			return nil, fmt.Errorf("sprite: atlas: two frames are named %q", f.Name)
		}
		fr := Frame{
			Name: f.Name,
			X:    f.Frame.X / scale, Y: f.Frame.Y / scale, W: f.Frame.W / scale, H: f.Frame.H / scale,
			Width: f.Frame.W / scale, Height: f.Frame.H / scale,
		}
		if f.Trimmed {
			fr.OffsetX, fr.OffsetY = f.SpriteSourceSize.X/scale, f.SpriteSourceSize.Y/scale
			fr.Width, fr.Height = f.SourceSize.W/scale, f.SourceSize.H/scale
		}
		fr.PivotX, fr.PivotY = f.Pivot.X*fr.Width, f.Pivot.Y*fr.Height
		a.Frames[f.Name] = fr
		ordered[i] = fr
	}

	if len(file.Meta.FrameTags) > 0 {
		a.Animations = make(map[string]Animation, len(file.Meta.FrameTags))
	}
	for _, tag := range file.Meta.FrameTags {
		if tag.From < 0 || tag.To < tag.From || tag.To >= len(frames) {
			return nil, fmt.Errorf("sprite: atlas: tag %q runs from frame %d to %d of %d", tag.Name, tag.From, tag.To, len(frames))
		}
		repeat := 0
		if len(tag.Repeat) > 0 {
			r, err := looseNumber(tag.Repeat)
			if err != nil || math.IsNaN(r) || math.IsInf(r, 0) || r < 0 || r > 100000 || r != float64(int(r)) {
				return nil, fmt.Errorf("sprite: atlas: tag %q repeats %s times", tag.Name, tag.Repeat)
			}
			repeat = int(r)
		}
		order, err := tagOrder(tag.From, tag.To, tag.Direction, repeat)
		if err != nil {
			return nil, fmt.Errorf("sprite: atlas: tag %q: %w", tag.Name, err)
		}
		anim := Animation{Loop: repeat == 0, Frames: make([]AnimationFrame, len(order))}
		for i, n := range order {
			if frames[n].Duration <= 0 || int64(frames[n].Duration) > math.MaxInt64/int64(time.Millisecond) {
				return nil, fmt.Errorf("sprite: atlas: tag %q: frame %q has no duration", tag.Name, frames[n].Name)
			}
			anim.Frames[i] = AnimationFrame{Frame: ordered[n], Duration: time.Duration(frames[n].Duration) * time.Millisecond}
		}
		a.Animations[tag.Name] = anim
	}

	for _, sl := range file.Meta.Slices {
		if len(sl.Keys) == 0 || sl.Keys[0].Center == nil {
			continue
		}
		b, c := sl.Keys[0].Bounds, sl.Keys[0].Center
		if c.X < 0 || c.Y < 0 || c.W < 0 || c.H < 0 || c.X+c.W > b.W || c.Y+c.H > b.H {
			return nil, fmt.Errorf("sprite: atlas: slice %q center falls outside its bounds", sl.Name)
		}
		if a.Slices == nil {
			a.Slices = make(map[string]Insets)
		}
		a.Slices[sl.Name] = Insets{
			Left:   c.X / scale,
			Top:    c.Y / scale,
			Right:  (b.W - c.X - c.W) / scale,
			Bottom: (b.H - c.Y - c.H) / scale,
		}
	}
	return a, nil
}

// packedFrame is a frame as the JSON of TexturePacker and Aseprite writes it.
type packedFrame struct {
	Name             string `json:"filename"`
	Frame            rect   `json:"frame"`
	Rotated          bool   `json:"rotated"`
	Trimmed          bool   `json:"trimmed"`
	SpriteSourceSize rect   `json:"spriteSourceSize"`
	SourceSize       rect   `json:"sourceSize"`
	// Pivot is TexturePacker's, as a fraction of the untrimmed picture's size.
	Pivot point `json:"pivot"`
	// Duration is Aseprite's, in milliseconds.
	Duration int `json:"duration"`
}

type rect struct {
	X, Y, W, H float32
}

type point struct {
	X, Y float32
}

// atlasFrames reads the frames of an export, an array of frames with a filename each or an object of frames by name, in the order the export lists them, which Aseprite's tags count frames in.
func atlasFrames(raw json.RawMessage) ([]packedFrame, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("no frames")
	}
	if raw[0] == '[' {
		var frames []packedFrame
		if err := json.Unmarshal(raw, &frames); err != nil {
			return nil, err
		}
		for i, f := range frames {
			if f.Name == "" {
				return nil, fmt.Errorf("frame %d has no filename", i)
			}
		}
		return frames, nil
	}
	// A map would lose the order, so the object is read key by key.
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("frames are neither an array nor an object")
	}
	var frames []packedFrame
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var f packedFrame
		if err := dec.Decode(&f); err != nil {
			return nil, err
		}
		name, ok := t.(string)
		if !ok {
			return nil, fmt.Errorf("frame key is not a string")
		}
		f.Name = name
		frames = append(frames, f)
	}
	return frames, nil
}

// tagOrder returns the frames, from and to included, that an Aseprite tag plays, in order: once round for a tag that loops (repeat 0), or all of it for one that plays repeat times. A ping-pong tag counts each way through as a repeat, as Aseprite does, and never shows the frame it turns at twice in a row.
func tagOrder(from, to int, direction string, repeat int) ([]int, error) {
	forward := func(skip int) []int {
		var s []int
		for n := from + skip; n <= to; n++ {
			s = append(s, n)
		}
		return s
	}
	backward := func(skip int) []int {
		var s []int
		for n := to - skip; n >= from; n-- {
			s = append(s, n)
		}
		return s
	}
	var passes [2]func(skip int) []int
	pingPong := false
	switch direction {
	case "", "forward":
		passes = [2]func(int) []int{forward, forward}
	case "reverse":
		passes = [2]func(int) []int{backward, backward}
	case "pingpong":
		passes = [2]func(int) []int{forward, backward}
		pingPong = true
	case "pingpong_reverse":
		passes = [2]func(int) []int{backward, forward}
		pingPong = true
	default:
		return nil, fmt.Errorf("unknown direction %q", direction)
	}
	if repeat == 0 {
		if !pingPong || from == to {
			return passes[0](0), nil
		}
		// The way back leaves out both ends: the loop starts over on the first.
		back := passes[1](1)
		return append(passes[0](0), back[:len(back)-1]...), nil
	}
	order := passes[0](0)
	for i := 1; i < repeat; i++ {
		if pingPong {
			if from < to {
				order = append(order, passes[i%2](1)...)
			}
		} else {
			order = append(order, passes[0](0)...)
		}
	}
	return order, nil
}

// looseNumber reads a number that an export may write as a JSON number or a string, as meta.scale and a tag's repeat are.
func looseNumber(raw json.RawMessage) (float64, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strconv.ParseFloat(s, 64)
	}
	var n float64
	err := json.Unmarshal(raw, &n)
	return n, err
}

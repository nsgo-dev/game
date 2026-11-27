package pixelfx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
)

// IsScreen reports whether an effect fills the screen (transitions, weather, impact frames): 16:9 and at least 320 wide. Its frames keep their light to the edge.
func IsScreen(m *Meta) bool {
	return m.Size[0] >= 320 && math.Abs(float64(m.Size[0])/float64(m.Size[1])-16.0/9) < 0.05
}

// sheetEdgeFade is how many pixels of light a sheet frame eases out at its edges.
const sheetEdgeFade = 8

// EachFrame renders every frame of e the way pixelfx renders a sprite sheet, calling fn with each frame's index and the frame: e is placed at its meta's origin with o's flip, angle, light, actor and values, played at fps (its meta's when 0) for round(duration * fps) frames, one more for a one-shot so its last frame is its end, stepping 1/fps between them, with light eased out over the last 8 pixels of each edge unless it fills the screen. fn must not keep the frame, which is reused.
func EachFrame(e *Effect, o Options, fps int, fn func(i int, f *Frame)) {
	m := &e.Meta
	if fps <= 0 {
		fps = m.FPS
	}
	n := sheetFrames(m, fps)
	frame := NewFrame(m.Size[0], m.Size[1], 0, 0)
	o.X, o.Y = float64(m.Origin[0]), float64(m.Origin[1])
	inst := NewPlayer(e, o)
	for i := range n {
		if i > 0 {
			inst.Update(1 / float64(fps))
		}
		frame.Clear()
		inst.Draw(frame, LayerBoth)
		if !IsScreen(m) {
			EdgeFade(frame, sheetEdgeFade)
		}
		fn(i, frame)
	}
}

func sheetFrames(m *Meta, fps int) int {
	n := int(Round(m.Duration * float64(fps)))
	if !m.Loop {
		n++
	}
	return max(1, n)
}

// actPad is the room an acted-out frame leaves round the act's bounds.
const actPad = 6

// ActFrameSize returns the size of the frames EachActFrame renders and where the feet sit in them, with the act's bounds sampled by rig.
func ActFrameSize(e *Effect, rig *Rig) (w, h int, x, y float64) {
	b := ActBounds(e, rig)
	return int(b.X1 - b.X0 + actPad*2), int(b.Y1 - b.Y0 + actPad*2), actPad - b.X0, actPad - b.Y0
}

// EachActFrame renders e acted out, the character and the target with it, the way pixelfx's act tool does: in a frame sized by ActFrameSize, at fps (60 when 0), from the first pose until the act is done or after 8 seconds (two loops for a loop), then 0.3 seconds more for a one-shot. fn gets each frame's index, the performance's time and shake, and the frame, which it must not keep.
func EachActFrame(e *Effect, o Options, fps int, fn func(i int, t, shakeX, shakeY float64, f *Frame)) {
	if fps <= 0 {
		fps = 60
	}
	rig := NewRig()
	w, h, x, y := ActFrameSize(e, rig)
	frame := NewFrame(w, h, 0, 0)
	o.X, o.Y = x, y
	pf := Perform(e, o, rig)
	m := &e.Meta
	seconds := 8.0
	if m.Loop {
		seconds = m.Duration*2 - pf.T()
	}
	limit := int(Round(seconds * float64(fps)))
	i := 0
	show := func() {
		frame.Clear()
		pf.Draw(frame, true, true)
		sx, sy := pf.Shake()
		fn(i, pf.T(), sx, sy, frame)
		i++
	}
	show()
	for k := 0; k < limit && !pf.Done(); k++ {
		pf.Update(1 / float64(fps))
		show()
	}
	// a beat on the end, so a video doesn't cut off the last pose
	if !m.Loop {
		for k := 0; float64(k) < float64(fps)*0.3; k++ {
			pf.Update(1 / float64(fps))
			show()
		}
	}
}

// Frames are an effect rendered to RGBA pixels (straight alpha, W x H x 4 bytes each), ready for a sprite sheet.
type Frames struct {
	Effect *Effect
	W, H   int
	FPS    int
	// Pivot is where the effect's origin (or, acted out, the character's feet) sits in each frame.
	Pivot [2]float64
	// Acted is true when the frames show the effect acted out.
	Acted  bool
	Pixels [][]byte
}

// Render renders every frame of e as EachFrame does.
func Render(e *Effect, o Options, fps int) *Frames {
	if fps <= 0 {
		fps = e.Meta.FPS
	}
	fr := &Frames{Effect: e, W: e.Meta.Size[0], H: e.Meta.Size[1], FPS: fps, Pivot: [2]float64{float64(e.Meta.Origin[0]), float64(e.Meta.Origin[1])}}
	EachFrame(e, o, fps, func(_ int, f *Frame) {
		fr.Pixels = append(fr.Pixels, Composite(f, nil, nil, 1))
	})
	return fr
}

// RenderAct renders e acted out as EachActFrame does, without the shake.
func RenderAct(e *Effect, o Options, fps int) *Frames {
	if fps <= 0 {
		fps = 60
	}
	w, h, x, y := ActFrameSize(e, nil)
	fr := &Frames{Effect: e, W: w, H: h, FPS: fps, Pivot: [2]float64{x, y}, Acted: true}
	EachActFrame(e, o, fps, func(_ int, _, _, _ float64, f *Frame) {
		fr.Pixels = append(fr.Pixels, Composite(f, nil, nil, 1))
	})
	return fr
}

// sheetJSON is the Aseprite JSON (json-array) a sheet's frames and tag are written as, with pixelfx's own meta beside them.
type sheetJSON struct {
	Frames []sheetFrame `json:"frames"`
	Meta   sheetMeta    `json:"meta"`
}

type sheetRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type sheetSize struct {
	W int `json:"w"`
	H int `json:"h"`
}

type sheetPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type sheetFrame struct {
	Filename         string     `json:"filename"`
	Frame            sheetRect  `json:"frame"`
	Rotated          bool       `json:"rotated"`
	Trimmed          bool       `json:"trimmed"`
	SpriteSourceSize sheetRect  `json:"spriteSourceSize"`
	SourceSize       sheetSize  `json:"sourceSize"`
	Pivot            sheetPoint `json:"pivot"`
	Duration         int        `json:"duration"`
}

type sheetTag struct {
	Name      string `json:"name"`
	From      int    `json:"from"`
	To        int    `json:"to"`
	Direction string `json:"direction"`
	Repeat    string `json:"repeat,omitempty"`
}

type sheetMeta struct {
	App       string     `json:"app"`
	Version   string     `json:"version"`
	Image     string     `json:"image"`
	Format    string     `json:"format"`
	Size      sheetSize  `json:"size"`
	Scale     string     `json:"scale"`
	FrameTags []sheetTag `json:"frameTags"`
	Pixelfx   sheetInfo  `json:"pixelfx"`
}

type sheetInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	FPS       int        `json:"fps"`
	Loop      bool       `json:"loop"`
	Acted     bool       `json:"acted,omitempty"`
	Frames    int        `json:"frames"`
	FrameSize [2]int     `json:"frameSize"`
	Pivot     [2]float64 `json:"pivot"`
	Columns   int        `json:"columns"`
	Act       *Timing    `json:"act,omitempty"`
}

// Sheet lays the frames out in a grid, as pixelfx's sheets do (about square, row by row), and returns the image and its Aseprite JSON, which sprite.LoadAtlas reads: frames named "<name> <i>" with their duration (1000 / fps ms, rounded) and their pivot at the effect's origin, one tag called name that plays them once for a one-shot or loops a loop, and pixelfx's meta (id, fps, loop, frame size, pivot, columns, and an act's timing). image is the file name the JSON gives the image.
func (fr *Frames) Sheet(name, image string) (*image.NRGBA, []byte, error) {
	n, w, h := len(fr.Pixels), fr.W, fr.H
	if n == 0 || w <= 0 || h <= 0 {
		return nil, nil, fmt.Errorf("pixelfx: no frames to lay out")
	}
	if fr.Effect == nil {
		return nil, nil, fmt.Errorf("pixelfx: frames without an effect")
	}
	if fr.FPS <= 0 {
		return nil, nil, fmt.Errorf("pixelfx: frames at %d fps, which is not above 0", fr.FPS)
	}
	for i, px := range fr.Pixels {
		if len(px) != w*h*4 {
			return nil, nil, fmt.Errorf("pixelfx: frame %d has %d bytes, not %d for %dx%d", i, len(px), w*h*4, w, h)
		}
	}
	cols := min(n, max(1, int(math.Ceil(math.Sqrt(float64(n*h)/float64(w))))))
	rows := (n + cols - 1) / cols
	img := newNRGBA(cols*w, rows*h)
	m := &fr.Effect.Meta
	doc := sheetJSON{Frames: make([]sheetFrame, n)}
	dur := int(Round(1000 / float64(fr.FPS)))
	for i, px := range fr.Pixels {
		x, y := (i%cols)*w, (i/cols)*h
		for row := 0; row < h; row++ {
			copy(img.Pix[(y+row)*img.Stride+x*4:(y+row)*img.Stride+(x+w)*4], px[row*w*4:(row+1)*w*4])
		}
		doc.Frames[i] = sheetFrame{
			Filename:         fmt.Sprintf("%s %d", name, i),
			Frame:            sheetRect{x, y, w, h},
			SpriteSourceSize: sheetRect{0, 0, w, h},
			SourceSize:       sheetSize{w, h},
			Pivot:            sheetPoint{fr.Pivot[0] / float64(w), fr.Pivot[1] / float64(h)},
			Duration:         dur,
		}
	}
	tag := sheetTag{Name: name, From: 0, To: n - 1, Direction: "forward"}
	if !m.Loop || fr.Acted {
		tag.Repeat = "1"
	}
	doc.Meta = sheetMeta{
		App: "https://github.com/nsgo-dev/game/tree/main/pkg/pixelfx", Version: "1", Image: image, Format: "RGBA8888",
		Size: sheetSize{cols * w, rows * h}, Scale: "1", FrameTags: []sheetTag{tag},
		Pixelfx: sheetInfo{ID: fr.Effect.ID, Name: m.Name, FPS: fr.FPS, Loop: m.Loop && !fr.Acted, Acted: fr.Acted, Frames: n, FrameSize: [2]int{w, h}, Pivot: fr.Pivot, Columns: cols, Act: ActTiming(fr.Effect)},
	}
	data, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, nil, err
	}
	return img, append(data, '\n'), nil
}

func newNRGBA(w, h int) *image.NRGBA { return image.NewNRGBA(image.Rect(0, 0, w, h)) }

// SheetPNG returns the sheet's image encoded as PNG with its JSON, as Sheet lays them out.
func (fr *Frames) SheetPNG(name, image string) (pngData, jsonData []byte, err error) {
	img, js, err := fr.Sheet(name, image)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), js, nil
}

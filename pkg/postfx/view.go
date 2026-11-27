package postfx

import (
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// View draws a frame through a chain, stretched over its box: a core.Shader whose image0 is the frame. Give the box the frame's aspect ratio, as a screen.Viewport places a virtual canvas, for square pixels.
type View struct {
	// Chain is the effects the frame is drawn through; a nil Chain draws the frame as it is.
	Chain *Chain
	// Frame is the picture the chain reads, usually a pixel buffer the game writes each frame, with Version telling its frames apart; FrameFunc, when set, follows the graph instead.
	Frame     core.ImageSource
	FrameFunc func() core.ImageSource
	// Uniforms are each effect's uniforms, in the chain's order, as Chain.Uniforms takes them: an AnimatedNode among them follows the platform's frame clock with no commit per frame. UniformsFunc, when set, follows the graph, its values over Uniforms'.
	Uniforms     []core.Uniforms
	UniformsFunc func() []core.Uniforms
	// Textures overrides each textured effect's Texture, in the chain's order, as Chain.Images takes them; a zero entry keeps the effect's own Texture. TexturesFunc, when set, follows the graph instead.
	Textures     []core.ImageSource
	TexturesFunc func() []core.ImageSource
	// Class is Tailwind CSS classes, as core.View's.
	Class     string
	Style     core.Style
	StyleFunc func() core.Style
	// Children draw over the chain's picture, such as a HUD.
	Children []core.Node
	OnPress  func(core.PressEvent)
	OnLayout func(core.Bounds)
	Ref      *core.Ref
	// Label names what the view shows for assistive technology, which skips an unlabeled view as a decoration.
	Label     string
	LabelFunc func() string
	TestID    string
}

// Render implements core.Node.
func (v View) Render(*core.Scope) core.Node {
	c := v.Chain
	if c == nil {
		c = plain()
	}
	s := core.Shader{
		Program:   c.program,
		Uniforms:  c.Uniforms(v.Uniforms),
		Class:     v.Class,
		Style:     v.Style,
		StyleFunc: v.StyleFunc,
		Children:  v.Children,
		OnPress:   v.OnPress,
		OnLayout:  v.OnLayout,
		Ref:       v.Ref,
		Label:     v.Label,
		LabelFunc: v.LabelFunc,
		TestID:    v.TestID,
	}
	if f := v.UniformsFunc; f != nil {
		s.UniformsFunc = func() core.Uniforms { return c.Uniforms(f()) }
	}
	if v.FrameFunc != nil || v.TexturesFunc != nil {
		frameFunc, texturesFunc := v.FrameFunc, v.TexturesFunc
		staticFrame, staticTextures := v.Frame, v.Textures
		s.ImagesFunc = func() []core.ImageSource {
			frame := staticFrame
			if frameFunc != nil {
				frame = frameFunc()
			}
			textures := staticTextures
			if texturesFunc != nil {
				textures = texturesFunc()
			}
			return c.Images(frame, textures)
		}
	} else {
		s.Images = c.Images(v.Frame, v.Textures)
	}
	return s
}

// plain is the chain of no effects, compiled on first use: a package variable's initializer would compile it in every program that imports the package.
var plain = sync.OnceValue(func() *Chain { return MustCompile() })

// Clock returns a value that counts seconds on the platform's frame clock, from 0 up to period and from 0 again, for as long as cx's owner lasts: a uniform set to it animates an effect with no work per frame. A period of 0 or less is an hour. Choose a period after which the effect looks as it started, such as a whole number of its cycles, as the value jumps back to 0.
func Clock(cx *core.Scope, period time.Duration) *core.AnimatedValue {
	if period <= 0 {
		period = time.Hour
	}
	v := core.NewAnimatedValue(cx, 0)
	cx.StartAnimation(core.Loop(core.Timing(v, period.Seconds(), core.TimingConfig{Duration: period, Easing: core.BezierEasing(core.Linear)}), 0), nil)
	return v
}

package project

import (
	"fmt"
	"io/fs"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
)

// Title returns the window's title: Window.Title, or Name when it is empty.
func (f *File) Title() string {
	if f.Window.Title != "" {
		return f.Window.Title
	}
	return f.Name
}

// WindowSize returns the main window's starting content size, as nsgo.WindowSize takes it: nsgo.WindowSize(f.WindowSize()).
func (f *File) WindowSize() (width, height core.Points) {
	return core.Points(f.Window.Width), core.Points(f.Window.Height)
}

// WindowConstraints returns the main window's size limits and whether the user may resize it, as nsgo.WindowConstraints takes them.
func (f *File) WindowConstraints() core.WindowConstraints {
	w := f.Window
	return core.WindowConstraints{
		MinWidth:  core.Points(w.MinWidth),
		MinHeight: core.Points(w.MinHeight),
		MaxWidth:  core.Points(w.MaxWidth),
		MaxHeight: core.Points(w.MaxHeight),
		NoResize:  w.Resizable != nil && !*w.Resizable,
	}
}

// Appearance returns the appearance the app starts in, as nsgo.Appearance takes it: core.AppearanceSystem when Window.Appearance is empty.
func (f *File) Appearance() core.Appearance {
	switch f.Window.Appearance {
	case "light":
		return core.AppearanceLight
	case "dark":
		return core.AppearanceDark
	default:
		return core.AppearanceSystem
	}
}

// Viewport returns the screen.Viewport of the virtual resolution, and false without a Display.
func (f *File) Viewport() (screen.Viewport, bool) {
	d := f.Display
	if d == nil {
		return screen.Viewport{}, false
	}
	v := screen.Viewport{Width: d.Width, Height: d.Height, IntegerScale: d.IntegerScale, PixelSnap: d.PixelSnap}
	switch d.Mode {
	case "fill":
		v.Mode = screen.ModeFill
	case "stretch":
		v.Mode = screen.ModeStretch
	case "expand":
		v.Mode = screen.ModeExpand
	default:
		v.Mode = screen.ModeFit
	}
	if d.BarColor != "" {
		// Validate accepted the color, so it parses.
		v.BarColor, _ = core.ParseColor(d.BarColor)
	}
	return v, true
}

// BindInput binds every action with bindings, axis, motion axis and gamepad axis of the input map into a, replacing a's bindings of the same names and leaving others, such as ones the game binds in code, as they are.
func (f *File) BindInput(a *input.Actions) {
	if f.Input == nil {
		return
	}
	for _, act := range f.Input.Actions {
		if len(act.Bindings) == 0 {
			continue
		}
		inputs := make([]input.Input, len(act.Bindings))
		for i, b := range act.Bindings {
			inputs[i] = b.Input()
		}
		a.Bind(act.Name, inputs...)
	}
	for _, ax := range f.Input.Axes {
		a.BindAxis(ax.Name, ax.Negative, ax.Positive)
	}
	for _, m := range f.Input.MotionAxes {
		a.BindMotionAxis(m.Name, motionAxis(m.Axis), input.MotionAxisOptions{
			DeadZone:    m.DeadZone,
			Sensitivity: m.Sensitivity,
			MaxRange:    m.MaxRange,
			Smoothing:   m.Smoothing,
			Invert:      m.Invert,
		})
	}
	for _, g := range f.Input.GamepadAxes {
		a.BindGamepadAxis(g.Name, gamepadAxis(g.Axis), input.GamepadAxisOptions{
			DeadZone:    g.DeadZone,
			Axial:       g.Axial,
			Sensitivity: g.Sensitivity,
			Invert:      g.Invert,
		})
	}
}

// Input returns b as an input.Input; a Binding Validate refused returns the zero Input, which is never active.
func (b Binding) Input() input.Input {
	var region core.Bounds
	if b.Region != nil {
		region = b.Region.Bounds()
	}
	switch b.Kind {
	case KindTouchRegion:
		return input.TouchRegion(region)
	case KindMouseButton:
		return input.MouseButton(mouseButton(b.Button))
	case KindMouseRegion:
		return input.MouseRegion(region, mouseButton(b.Button))
	case KindKey:
		if b.Key == "" {
			return input.Input{}
		}
		return input.Key(b.Key)
	case KindKeyCode:
		if b.Code == "" {
			return input.Input{}
		}
		return input.KeyCode(b.Code)
	case KindGamepadButton:
		btn, ok := gamepadButton(b.Button)
		if !ok {
			return input.Input{}
		}
		return input.GamepadButton(btn)
	default:
		return input.Input{}
	}
}

// Region returns the first region bound to action, as a game draws its on-screen control there, and false when the action has no region binding.
func (f *File) Region(action string) (core.Bounds, bool) {
	if f.Input == nil {
		return core.Bounds{}, false
	}
	for _, act := range f.Input.Actions {
		if act.Name != action {
			continue
		}
		for _, b := range act.Bindings {
			if b.Region != nil {
				return b.Region.Bounds(), true
			}
		}
	}
	return core.Bounds{}, false
}

func mouseButton(s string) core.MouseButton {
	switch s {
	case "left":
		return core.MouseButtonLeft
	case "right":
		return core.MouseButtonRight
	case "middle":
		return core.MouseButtonMiddle
	case "back":
		return core.MouseButtonBack
	case "forward":
		return core.MouseButtonForward
	default:
		return core.MouseButtonNone
	}
}

func motionAxis(s string) input.MotionAxis {
	switch s {
	case "tiltX":
		return input.TiltX
	case "tiltY":
		return input.TiltY
	case "tiltZ":
		return input.TiltZ
	case "gyroX":
		return input.GyroX
	case "gyroY":
		return input.GyroY
	case "gyroZ":
		return input.GyroZ
	default:
		return input.MotionAxisNone
	}
}

func gamepadButton(s string) (gamepad.Button, bool) {
	switch s {
	case "south":
		return gamepad.ButtonSouth, true
	case "east":
		return gamepad.ButtonEast, true
	case "west":
		return gamepad.ButtonWest, true
	case "north":
		return gamepad.ButtonNorth, true
	case "leftShoulder":
		return gamepad.ButtonLB, true
	case "rightShoulder":
		return gamepad.ButtonRB, true
	case "leftTrigger":
		return gamepad.ButtonLT, true
	case "rightTrigger":
		return gamepad.ButtonRT, true
	case "back":
		return gamepad.ButtonBack, true
	case "start":
		return gamepad.ButtonStart, true
	case "leftStick":
		return gamepad.ButtonLeftStick, true
	case "rightStick":
		return gamepad.ButtonRightStick, true
	case "dpadUp":
		return gamepad.ButtonDPadUp, true
	case "dpadDown":
		return gamepad.ButtonDPadDown, true
	case "dpadLeft":
		return gamepad.ButtonDPadLeft, true
	case "dpadRight":
		return gamepad.ButtonDPadRight, true
	case "home":
		return gamepad.ButtonHome, true
	default:
		return 0, false
	}
}

func gamepadAxis(s string) gamepad.Axis {
	switch s {
	case "leftX":
		return gamepad.AxisLeftX
	case "leftY":
		return gamepad.AxisLeftY
	case "rightX":
		return gamepad.AxisRightX
	case "rightY":
		return gamepad.AxisRightY
	default:
		return gamepad.Axis(gamepad.AxisCount)
	}
}

// ConfigureMixer gives m the default master volume and each bus's default volume and DSP chain. Volumes the player kept in storage win over the defaults. A bus without effects leaves the chain the game set in code, as BindInput leaves the bindings of an action without any.
func (f *File) ConfigureMixer(m *sound.Mixer) {
	if f.Audio == nil {
		return
	}
	if f.Audio.Master != nil {
		m.SetDefaultMaster(*f.Audio.Master)
	}
	for _, b := range f.Audio.Buses {
		if b.Volume != nil {
			m.SetDefaultVolume(b.Name, *b.Volume)
		}
		if len(b.Effects) > 0 {
			m.SetGroupEffects(b.Name, b.Chain()...)
		}
	}
}

// Chain returns the bus's effects as a sound.EffectChain, nil without any.
func (b Bus) Chain() sound.EffectChain {
	if len(b.Effects) == 0 {
		return nil
	}
	chain := make(sound.EffectChain, 0, len(b.Effects))
	for _, e := range b.Effects {
		if fx := e.BusEffect(); fx != nil {
			chain = append(chain, fx)
		}
	}
	return chain
}

// BusEffect returns e as a sound.BusEffect, nil when no field is set.
func (e Effect) BusEffect() sound.BusEffect {
	switch {
	case e.LowPass != nil:
		return sound.LowPass(e.LowPass.CutoffHz, e.LowPass.Q)
	case e.HighPass != nil:
		return sound.HighPass(e.HighPass.CutoffHz, e.HighPass.Q)
	case e.Delay != nil:
		return sound.Delay(e.Delay.Time, e.Delay.Feedback, e.Delay.Wet)
	case e.Reverb != nil:
		return sound.Reverb(e.Reverb.RoomSize, e.Reverb.Damping, e.Reverb.Wet)
	case e.Gain != nil:
		return sound.Gain(e.Gain.Factor)
	case e.Bitcrush != nil:
		return sound.Bitcrush(e.Bitcrush.Bits, e.Bitcrush.Downsample)
	default:
		return nil
	}
}

// Loaded holds the autoloaded scenes and resources of a project.
type Loaded struct {
	// MainScene is the name of the scene the game starts in, empty when the project names none.
	MainScene string
	// Scenes are the parsed and validated autoloaded scenes by name.
	Scenes map[string]*sceneformat.SceneDoc
	// Resources are the autoloaded resources' bytes by name.
	Resources map[string][]byte
}

// Main returns the main scene, nil when the project names none.
func (l *Loaded) Main() *sceneformat.SceneDoc {
	if l == nil || l.MainScene == "" {
		return nil
	}
	return l.Scenes[l.MainScene]
}

// LoadAutoloads reads every autoloaded scene and resource from fsys, the project root, parsing and validating the scenes. It fails on the first file that is missing or, for a scene, invalid.
func (f *File) LoadAutoloads(fsys fs.FS) (*Loaded, error) {
	l := &Loaded{Scenes: map[string]*sceneformat.SceneDoc{}, Resources: map[string][]byte{}}
	if f.Autoload == nil {
		return l, nil
	}
	l.MainScene = f.Autoload.MainScene
	for _, s := range f.Autoload.Scenes {
		data, err := fs.ReadFile(fsys, s.Path)
		if err != nil {
			return nil, fmt.Errorf("project: autoload scene %q: %w", s.Name, err)
		}
		doc, err := sceneformat.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("project: autoload scene %q (%s): %w", s.Name, s.Path, err)
		}
		l.Scenes[s.Name] = doc
	}
	for _, r := range f.Autoload.Resources {
		data, err := fs.ReadFile(fsys, r.Path)
		if err != nil {
			return nil, fmt.Errorf("project: autoload resource %q: %w", r.Name, err)
		}
		l.Resources[r.Name] = data
	}
	return l, nil
}

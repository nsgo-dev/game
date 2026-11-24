package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// FileName is the project file's name at a game's root.
const FileName = "game.yaml"

// CurrentVersion is the project file schema version this package reads and writes.
const CurrentVersion = 1

// File is the root of a versioned game.yaml project file.
type File struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version"`
	// Name is the game's display name, which nsgo.yaml's name follows.
	Name string `yaml:"name"`
	// ID is the game's reverse-DNS application identifier, such as dev.nsgo.caverun, which nsgo.yaml's id follows.
	ID string `yaml:"id"`
	// AppVersion is the game's release version, such as 0.1.0, which nsgo.yaml's version follows.
	AppVersion string `yaml:"appVersion"`
	// Window is the main window's size, limits, title and appearance.
	Window Window `yaml:"window"`
	// Display is the virtual resolution the game draws at; nil draws at the window's own size.
	Display *Display `yaml:"display,omitempty"`
	// Input is the input map bound into an input.Actions; nil binds nothing.
	Input *Input `yaml:"input,omitempty"`
	// Audio holds the master volume and the audio buses a sound.Mixer is configured with; nil configures nothing.
	Audio *Audio `yaml:"audio,omitempty"`
	// Autoload lists the scenes and resources loaded at startup; nil loads nothing.
	Autoload *Autoload `yaml:"autoload,omitempty"`
	// Export holds the build output directory and the export presets per platform, which nsgo.yaml's build section follows; nil leaves nsgo.yaml without them.
	Export *Export `yaml:"export,omitempty"`
	// Editor holds the scene editor's settings for this project; nil uses its defaults.
	Editor *Editor `yaml:"editor,omitempty"`
	// Assets declares the atlases, textures, audio and asset manifest game assets builds; nil declares none.
	Assets *Assets `yaml:"assets,omitempty"`
	// Packs records the library packs installed into the project, in the order they were first installed: the files each wrote and the autoload entries and editor plugins each added, so that a later install of the pack can update it and a removal can undo it (see the tooling/library package). nil records none.
	Packs []Pack `yaml:"packs,omitempty"`
}

// Window is the main window's content size in points, its size limits, its title and its appearance.
type Window struct {
	// Title is the window's title; empty uses File.Name.
	Title string `yaml:"title,omitempty"`
	// Width and Height are the window's starting content size in points; both must be positive.
	Width  float32 `yaml:"width"`
	Height float32 `yaml:"height"`
	// MinWidth and MinHeight are the smallest content size the user may resize the window to; 0 is no limit.
	MinWidth  float32 `yaml:"minWidth,omitempty"`
	MinHeight float32 `yaml:"minHeight,omitempty"`
	// MaxWidth and MaxHeight are the largest content size the user may resize the window to; 0 is no limit.
	MaxWidth  float32 `yaml:"maxWidth,omitempty"`
	MaxHeight float32 `yaml:"maxHeight,omitempty"`
	// Resizable says whether the user may resize the window; nil defaults to true.
	Resizable *bool `yaml:"resizable,omitempty"`
	// Appearance starts the app "light" or "dark" whatever the system's; empty follows the system. nsgo.yaml's appearance follows it.
	Appearance string `yaml:"appearance,omitempty"`
}

// Display is the virtual resolution a screen.Viewport maps onto the window.
type Display struct {
	// Width and Height are the virtual canvas size in game points; both must be positive.
	Width  float32 `yaml:"width"`
	Height float32 `yaml:"height"`
	// Mode is how the canvas fits a window of another aspect ratio: "fit" (the default when empty), "fill", "stretch" or "expand", as screen.Mode names them.
	Mode string `yaml:"mode,omitempty"`
	// IntegerScale scales by whole multiples only, as screen.Viewport.IntegerScale does.
	IntegerScale bool `yaml:"integerScale,omitempty"`
	// PixelSnap aligns the canvas to whole points, as screen.Viewport.PixelSnap does.
	PixelSnap bool `yaml:"pixelSnap,omitempty"`
	// BarColor is the CSS color of letterbox and pillarbox bars, parsed with core.ParseColor; empty is opaque black.
	BarColor string `yaml:"barColor,omitempty"`
}

// Input is a game's input map: named actions with their physical bindings, axes over pairs of actions and device motion axes.
type Input struct {
	// Actions are the named actions in the order they are declared.
	Actions []Action `yaml:"actions,omitempty"`
	// Axes are the named axes, each reading -1 for its negative action and +1 for its positive one.
	Axes []Axis `yaml:"axes,omitempty"`
	// MotionAxes are the named axes read from the device's tilt and gyroscope.
	MotionAxes []MotionAxis `yaml:"motionAxes,omitempty"`
}

// Action is one named action and the physical inputs that trigger it.
type Action struct {
	// Name uniquely identifies the action.
	Name string `yaml:"name"`
	// Bindings are the inputs that press the action; an action without any is declared for code to bind, such as one an axis reads.
	Bindings []Binding `yaml:"bindings,omitempty"`
}

// Binding kinds name the input.Input constructors.
const (
	// KindTouchRegion is a touch inside Region (input.TouchRegion).
	KindTouchRegion = "touchRegion"
	// KindMouseButton is Button held anywhere on the view (input.MouseButton).
	KindMouseButton = "mouseButton"
	// KindMouseRegion is Button held inside Region (input.MouseRegion).
	KindMouseRegion = "mouseRegion"
)

// Binding is one physical input bound to an action.
type Binding struct {
	// Kind is KindTouchRegion, KindMouseButton or KindMouseRegion.
	Kind string `yaml:"kind"`
	// Button is "left", "right", "middle", "back" or "forward"; required by the mouse kinds and refused by KindTouchRegion.
	Button string `yaml:"button,omitempty"`
	// Region is the rectangle in virtual coordinates (or view coordinates without a Display) that KindTouchRegion and KindMouseRegion test; refused by KindMouseButton.
	Region *Rect `yaml:"region,omitempty,flow"`
}

// Rect is an axis-aligned rectangle with a positive size.
type Rect struct {
	X      float32 `yaml:"x,omitempty"`
	Y      float32 `yaml:"y,omitempty"`
	Width  float32 `yaml:"width"`
	Height float32 `yaml:"height"`
}

// Bounds returns r as core.Bounds.
func (r Rect) Bounds() core.Bounds {
	return core.Bounds{
		Origin: core.Point{X: core.Points(r.X), Y: core.Points(r.Y)},
		Size:   core.Size{Width: core.Points(r.Width), Height: core.Points(r.Height)},
	}
}

// Axis is a named axis over two actions, as input.Actions.BindAxis binds it.
type Axis struct {
	// Name uniquely identifies the axis.
	Name string `yaml:"name"`
	// Negative and Positive name declared actions; at least one is required.
	Negative string `yaml:"negative,omitempty"`
	Positive string `yaml:"positive,omitempty"`
}

// MotionAxis is a named axis read from a device motion sensor, as input.Actions.BindMotionAxis binds it.
type MotionAxis struct {
	// Name uniquely identifies the motion axis.
	Name string `yaml:"name"`
	// Axis is "tiltX", "tiltY", "tiltZ", "gyroX", "gyroY" or "gyroZ".
	Axis string `yaml:"axis"`
	// DeadZone, Sensitivity, MaxRange, Smoothing and Invert are input.MotionAxisOptions' fields; 0 takes their defaults.
	DeadZone    float64 `yaml:"deadZone,omitempty"`
	Sensitivity float64 `yaml:"sensitivity,omitempty"`
	MaxRange    float64 `yaml:"maxRange,omitempty"`
	Smoothing   float64 `yaml:"smoothing,omitempty"`
	Invert      bool    `yaml:"invert,omitempty"`
}

// Audio is the master volume and the buses (sound.Mixer groups) a game plays through.
type Audio struct {
	// Master is the default master volume in [0, 1]; nil leaves it at 1.
	Master *float32 `yaml:"master,omitempty"`
	// Buses are the mixer groups in declaration order.
	Buses []Bus `yaml:"buses,omitempty"`
}

// Bus is one sound.Mixer group: its default volume and its DSP effect chain.
type Bus struct {
	// Name is the mixer group, such as "music" or "sfx".
	Name string `yaml:"name"`
	// Volume is the default volume in [0, 1] until the player sets one; nil leaves it at 1.
	Volume *float32 `yaml:"volume,omitempty"`
	// Effects is the bus's DSP chain, applied in order.
	Effects []Effect `yaml:"effects,omitempty"`
}

// Effect is one DSP effect on a bus; exactly one field must be set.
type Effect struct {
	LowPass  *Filter   `yaml:"lowPass,omitempty,flow"`
	HighPass *Filter   `yaml:"highPass,omitempty,flow"`
	Delay    *Delay    `yaml:"delay,omitempty,flow"`
	Reverb   *Reverb   `yaml:"reverb,omitempty,flow"`
	Gain     *Gain     `yaml:"gain,omitempty,flow"`
	Bitcrush *Bitcrush `yaml:"bitcrush,omitempty,flow"`
}

// Filter is a biquad low-pass or high-pass filter (sound.LowPass, sound.HighPass).
type Filter struct {
	// CutoffHz is the cutoff frequency in hertz; it must be positive.
	CutoffHz float64 `yaml:"cutoffHz"`
	// Q is the filter's quality factor; 0 is Butterworth.
	Q float64 `yaml:"q,omitempty"`
}

// Delay is a feedback delay line (sound.Delay).
type Delay struct {
	// Time is the echo spacing, such as 250ms; it must be positive.
	Time time.Duration `yaml:"time"`
	// Feedback is in [0, 0.99] and Wet in [0, 1].
	Feedback float64 `yaml:"feedback,omitempty"`
	Wet      float64 `yaml:"wet,omitempty"`
}

// Reverb is a Freeverb-style reverb (sound.Reverb); each field is in [0, 1].
type Reverb struct {
	RoomSize float64 `yaml:"roomSize,omitempty"`
	Damping  float64 `yaml:"damping,omitempty"`
	Wet      float64 `yaml:"wet,omitempty"`
}

// Gain scales every sample by Factor (sound.Gain), which must be finite and non-negative.
type Gain struct {
	Factor float64 `yaml:"factor"`
}

// Bitcrush is a lo-fi quantizer (sound.Bitcrush): Bits in [1, 15] and Downsample 0 or more, where 0 and 1 keep every frame.
type Bitcrush struct {
	Bits       int `yaml:"bits"`
	Downsample int `yaml:"downsample,omitempty"`
}

// Autoload lists the scenes and resources a game loads at startup.
type Autoload struct {
	// MainScene names the scene in Scenes the game starts in; empty leaves the choice to the game.
	MainScene string `yaml:"mainScene,omitempty"`
	// Scenes are sceneformat YAML documents, loaded and validated at startup.
	Scenes []Entry `yaml:"scenes,omitempty"`
	// Resources are other files loaded as bytes at startup, such as data tables.
	Resources []Entry `yaml:"resources,omitempty"`
}

// Entry is one autoloaded file.
type Entry struct {
	// Name uniquely identifies the entry within its list.
	Name string `yaml:"name"`
	// Path is the file's slash-separated path relative to the project root, as fs.ValidPath accepts it.
	Path string `yaml:"path"`
}

// Export holds the build output directory and one export preset per platform.
type Export struct {
	// Output is the directory builds are written to; empty is NSGo's default (dist).
	Output string `yaml:"output,omitempty"`
	// Presets are the platforms the game exports to, in build order.
	Presets []Preset `yaml:"presets,omitempty"`
}

// Preset is the export preset of one platform.
type Preset struct {
	// Platform is one of Platforms, such as "web" or "macos".
	Platform string `yaml:"platform"`
	// Targets are the NSGo build targets of the platform to build, such as macos_arm64; empty builds the platform name itself, which NSGo expands to its default targets.
	Targets []string `yaml:"targets,omitempty,flow"`
}

// Editor is the scene editor's settings for one project.
type Editor struct {
	// Plugins names the editor plugins the scene editor turns on for this project, in the order their panels and tools appear. A plugin is Go code built into the editor binary (editor.Plugin), so this list only turns plugins on; each name is unique and has no spaces or control characters.
	Plugins []string `yaml:"plugins,omitempty,flow"`
}

// Pack kinds name what a library pack holds.
const (
	// PackAsset is a pack of assets, such as images, atlases, sounds and data files.
	PackAsset = "asset"
	// PackPlugin is a pack that turns on editor plugins, with any files they read.
	PackPlugin = "plugin"
	// PackTemplate is a pack of scenes and prefabs a game starts from or builds on.
	PackTemplate = "template"
)

// PackKinds are the kinds a Pack may have, in the order the tools list them.
var PackKinds = []string{PackAsset, PackPlugin, PackTemplate}

// Pack is one library pack installed into the project. Its files are Dir joined with each of Files, and Scenes, Resources and Plugins name the autoload entries and editor plugins the install added, which must still be in the file; an entry or plugin the project held before the install is not the pack's and is not listed.
type Pack struct {
	// Name identifies the pack in its library: lower-case ASCII letters, digits and '-', starting with a letter or digit.
	Name string `yaml:"name"`
	// Kind is one of PackKinds.
	Kind string `yaml:"kind"`
	// PackVersion is the version of the pack that was installed, such as 1.2.0.
	PackVersion string `yaml:"packVersion"`
	// SHA256 is the pack's digest, 64 lower-case hexadecimal digits, which tells one release of the pack from another whatever its version says.
	SHA256 string `yaml:"sha256"`
	// Dir is the slash-separated directory inside the project the pack's files were written to.
	Dir string `yaml:"dir"`
	// Files are the slash-separated paths of the pack's files relative to Dir, sorted and unique.
	Files []string `yaml:"files,omitempty"`
	// Scenes names the autoload scenes the install added.
	Scenes []string `yaml:"scenes,omitempty,flow"`
	// Resources names the autoload resources the install added.
	Resources []string `yaml:"resources,omitempty,flow"`
	// Plugins names the editor plugins the install turned on.
	Plugins []string `yaml:"plugins,omitempty,flow"`
}

// Paths returns the slash-separated paths of p's files from the project root, in the order of Files.
func (p *Pack) Paths() []string {
	out := make([]string, len(p.Files))
	for i, f := range p.Files {
		out[i] = path.Join(p.Dir, f)
	}
	return out
}

// FindPack returns the installed pack called name, or nil.
func (f *File) FindPack(name string) *Pack {
	for i := range f.Packs {
		if f.Packs[i].Name == name {
			return &f.Packs[i]
		}
	}
	return nil
}

// Bool returns a pointer to v for optional fields such as Window.Resizable.
func Bool(v bool) *bool { return &v }

// Float32 returns a pointer to v for optional fields such as Bus.Volume and Audio.Master.
func Float32(v float32) *float32 { return &v }

// Parse decodes a YAML project file and validates it. Unknown fields and trailing YAML documents are refused.
func Parse(data []byte) (*File, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("project: empty project file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("project: decode: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("project: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("project: decode: %w", err)
	}
	if err := Validate(&f); err != nil {
		return nil, err
	}
	if f.Assets != nil {
		f.Assets.normalize()
	}
	return &f, nil
}

// MustParse is Parse for a project file that is part of the program, such as an embedded game.yaml: it panics on an invalid file.
func MustParse(data []byte) *File {
	f, err := Parse(data)
	if err != nil {
		panic(err)
	}
	return f
}

// ReadFS reads and parses FileName at the root of fsys.
func ReadFS(fsys fs.FS) (*File, error) {
	data, err := fs.ReadFile(fsys, FileName)
	if err != nil {
		return nil, fmt.Errorf("project: %w", err)
	}
	return Parse(data)
}

// Encode validates f and serializes it as deterministic 2-space YAML that Parse reads back unchanged.
func Encode(f *File) ([]byte, error) {
	if err := Validate(f); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("project: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("project: encode: %w", err)
	}
	return buf.Bytes(), nil
}

// Clone returns a deep copy of f, which Validate accepts exactly when it accepts f.
func (f *File) Clone() *File {
	if f == nil {
		return nil
	}
	c := *f
	c.Window.Resizable = clonePtr(f.Window.Resizable)
	if f.Display != nil {
		d := *f.Display
		c.Display = &d
	}
	if f.Input != nil {
		in := Input{
			Axes:       append([]Axis(nil), f.Input.Axes...),
			MotionAxes: append([]MotionAxis(nil), f.Input.MotionAxes...),
		}
		if f.Input.Actions != nil {
			in.Actions = make([]Action, len(f.Input.Actions))
			for i, a := range f.Input.Actions {
				in.Actions[i] = Action{Name: a.Name}
				if a.Bindings != nil {
					in.Actions[i].Bindings = make([]Binding, len(a.Bindings))
					for j, b := range a.Bindings {
						b.Region = clonePtr(b.Region)
						in.Actions[i].Bindings[j] = b
					}
				}
			}
		}
		c.Input = &in
	}
	if f.Audio != nil {
		a := Audio{Master: clonePtr(f.Audio.Master)}
		if f.Audio.Buses != nil {
			a.Buses = make([]Bus, len(f.Audio.Buses))
			for i, b := range f.Audio.Buses {
				nb := Bus{Name: b.Name, Volume: clonePtr(b.Volume)}
				if b.Effects != nil {
					nb.Effects = make([]Effect, len(b.Effects))
					for j, e := range b.Effects {
						nb.Effects[j] = Effect{
							LowPass:  clonePtr(e.LowPass),
							HighPass: clonePtr(e.HighPass),
							Delay:    clonePtr(e.Delay),
							Reverb:   clonePtr(e.Reverb),
							Gain:     clonePtr(e.Gain),
							Bitcrush: clonePtr(e.Bitcrush),
						}
					}
				}
				a.Buses[i] = nb
			}
		}
		c.Audio = &a
	}
	if f.Autoload != nil {
		c.Autoload = &Autoload{
			MainScene: f.Autoload.MainScene,
			Scenes:    append([]Entry(nil), f.Autoload.Scenes...),
			Resources: append([]Entry(nil), f.Autoload.Resources...),
		}
	}
	if f.Export != nil {
		e := Export{Output: f.Export.Output}
		if f.Export.Presets != nil {
			e.Presets = make([]Preset, len(f.Export.Presets))
			for i, p := range f.Export.Presets {
				e.Presets[i] = Preset{Platform: p.Platform, Targets: append([]string(nil), p.Targets...)}
			}
		}
		c.Export = &e
	}
	if f.Editor != nil {
		c.Editor = &Editor{Plugins: append([]string(nil), f.Editor.Plugins...)}
	}
	c.Assets = f.Assets.clone()
	if f.Packs != nil {
		c.Packs = make([]Pack, len(f.Packs))
		for i, p := range f.Packs {
			p.Files = slices.Clone(p.Files)
			p.Scenes = slices.Clone(p.Scenes)
			p.Resources = slices.Clone(p.Resources)
			p.Plugins = slices.Clone(p.Plugins)
			c.Packs[i] = p
		}
	}
	return &c
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// Validate checks that f conforms to the version 1 schema.
func Validate(f *File) error {
	if f == nil {
		return errors.New("project: nil project file")
	}
	if f.Version != CurrentVersion {
		return fmt.Errorf("project: unsupported version %d (want %d)", f.Version, CurrentVersion)
	}
	if err := validateText("name", f.Name, true); err != nil {
		return err
	}
	if err := validateID(f.ID); err != nil {
		return err
	}
	if f.AppVersion == "" || strings.IndexFunc(f.AppVersion, unicode.IsSpace) >= 0 || strings.IndexFunc(f.AppVersion, unicode.IsControl) >= 0 {
		return fmt.Errorf("project: appVersion %q must be non-empty without spaces", f.AppVersion)
	}
	if err := validateWindow(&f.Window); err != nil {
		return err
	}
	if f.Display != nil {
		if err := validateDisplay(f.Display); err != nil {
			return err
		}
	}
	if f.Input != nil {
		if err := validateInput(f.Input); err != nil {
			return err
		}
	}
	if f.Audio != nil {
		if err := validateAudio(f.Audio); err != nil {
			return err
		}
	}
	if f.Autoload != nil {
		if err := validateAutoload(f.Autoload); err != nil {
			return err
		}
	}
	if f.Export != nil {
		if err := validateExport(f.Export); err != nil {
			return err
		}
	}
	if f.Editor != nil {
		if err := validateEditor(f.Editor); err != nil {
			return err
		}
	}
	if err := f.Assets.Validate(); err != nil {
		return err
	}
	return validatePacks(f)
}

// validateText refuses control characters and surrounding spaces, and an empty value when required.
func validateText(field, s string, required bool) error {
	if s == "" {
		if required {
			return fmt.Errorf("project: %s is required", field)
		}
		return nil
	}
	if strings.TrimSpace(s) != s || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return fmt.Errorf("project: %s %q must have no surrounding spaces or control characters", field, s)
	}
	return nil
}

// validateID accepts a reverse-DNS identifier: two or more dot-separated segments of ASCII letters, digits, '_' and '-', each starting with a letter.
func validateID(id string) error {
	segments := strings.Split(id, ".")
	if len(segments) < 2 {
		return fmt.Errorf("project: id %q must be a reverse-DNS identifier such as dev.example.game", id)
	}
	for _, seg := range segments {
		if seg == "" || !isASCIILetter(seg[0]) {
			return fmt.Errorf("project: id %q must be a reverse-DNS identifier such as dev.example.game", id)
		}
		for i := range len(seg) {
			c := seg[i]
			if !isASCIILetter(c) && (c < '0' || c > '9') && c != '_' && c != '-' {
				return fmt.Errorf("project: id %q has invalid character %q", id, c)
			}
		}
	}
	return nil
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func validateWindow(w *Window) error {
	if err := validateText("window title", w.Title, false); err != nil {
		return err
	}
	if !positive(w.Width) || !positive(w.Height) {
		return fmt.Errorf("project: window size (%v, %v) must be finite and positive", w.Width, w.Height)
	}
	for _, v := range []float32{w.MinWidth, w.MinHeight, w.MaxWidth, w.MaxHeight} {
		if !isFinite32(v) || v < 0 {
			return errors.New("project: window limits must be finite and non-negative")
		}
	}
	if w.MaxWidth > 0 && w.MaxWidth < w.MinWidth || w.MaxHeight > 0 && w.MaxHeight < w.MinHeight {
		return errors.New("project: window maximum size must be at least its minimum")
	}
	if w.Width < w.MinWidth || w.Height < w.MinHeight || w.MaxWidth > 0 && w.Width > w.MaxWidth || w.MaxHeight > 0 && w.Height > w.MaxHeight {
		return fmt.Errorf("project: window size (%v, %v) must lie within its limits", w.Width, w.Height)
	}
	switch w.Appearance {
	case "", "light", "dark":
	default:
		return fmt.Errorf("project: window appearance %q must be light, dark or empty", w.Appearance)
	}
	return nil
}

// Display modes name screen.Mode values.
var displayModes = []string{"fit", "fill", "stretch", "expand"}

func validateDisplay(d *Display) error {
	if !positive(d.Width) || !positive(d.Height) {
		return fmt.Errorf("project: display size (%v, %v) must be finite and positive", d.Width, d.Height)
	}
	if d.Mode != "" && !slices.Contains(displayModes, d.Mode) {
		return fmt.Errorf("project: display mode %q must be one of %s", d.Mode, strings.Join(displayModes, ", "))
	}
	if d.BarColor != "" {
		if _, err := core.ParseColor(d.BarColor); err != nil {
			return fmt.Errorf("project: display barColor %q: %w", d.BarColor, err)
		}
	}
	return nil
}

var mouseButtons = []string{"left", "right", "middle", "back", "forward"}

var motionAxes = []string{"tiltX", "tiltY", "tiltZ", "gyroX", "gyroY", "gyroZ"}

func validateInput(in *Input) error {
	actions := make(map[string]struct{}, len(in.Actions))
	for i, a := range in.Actions {
		if err := validateName(fmt.Sprintf("input action %d", i), a.Name); err != nil {
			return err
		}
		if _, dup := actions[a.Name]; dup {
			return fmt.Errorf("project: duplicate input action %q", a.Name)
		}
		actions[a.Name] = struct{}{}
		for j, b := range a.Bindings {
			if err := validateBinding(b); err != nil {
				return fmt.Errorf("project: input action %q binding %d: %w", a.Name, j, err)
			}
		}
	}
	axes := make(map[string]struct{}, len(in.Axes))
	for i, ax := range in.Axes {
		if err := validateName(fmt.Sprintf("input axis %d", i), ax.Name); err != nil {
			return err
		}
		if _, dup := axes[ax.Name]; dup {
			return fmt.Errorf("project: duplicate input axis %q", ax.Name)
		}
		axes[ax.Name] = struct{}{}
		if ax.Negative == "" && ax.Positive == "" {
			return fmt.Errorf("project: input axis %q needs a negative or a positive action", ax.Name)
		}
		for _, name := range []string{ax.Negative, ax.Positive} {
			if _, ok := actions[name]; name != "" && !ok {
				return fmt.Errorf("project: input axis %q reads undeclared action %q", ax.Name, name)
			}
		}
	}
	// A motion axis may share its name with an axis: input.Actions.Axis sums both, as input.Actions.BindMotionAxis documents.
	motion := make(map[string]struct{}, len(in.MotionAxes))
	for i, m := range in.MotionAxes {
		if err := validateName(fmt.Sprintf("input motion axis %d", i), m.Name); err != nil {
			return err
		}
		if _, dup := motion[m.Name]; dup {
			return fmt.Errorf("project: duplicate input motion axis %q", m.Name)
		}
		motion[m.Name] = struct{}{}
		if !slices.Contains(motionAxes, m.Axis) {
			return fmt.Errorf("project: input motion axis %q axis %q must be one of %s", m.Name, m.Axis, strings.Join(motionAxes, ", "))
		}
		if !isFinite64(m.DeadZone) || m.DeadZone < 0 || m.DeadZone >= 1 {
			return fmt.Errorf("project: input motion axis %q deadZone %v must be in [0, 1)", m.Name, m.DeadZone)
		}
		if !isFinite64(m.Smoothing) || m.Smoothing < 0 || m.Smoothing >= 1 {
			return fmt.Errorf("project: input motion axis %q smoothing %v must be in [0, 1)", m.Name, m.Smoothing)
		}
		if !isFinite64(m.Sensitivity) || m.Sensitivity < 0 || !isFinite64(m.MaxRange) || m.MaxRange < 0 {
			return fmt.Errorf("project: input motion axis %q sensitivity and maxRange must be finite and non-negative", m.Name)
		}
	}
	return nil
}

func validateBinding(b Binding) error {
	switch b.Kind {
	case KindTouchRegion:
		if b.Button != "" {
			return errors.New("touchRegion takes no button")
		}
		return validateRegion(b.Region)
	case KindMouseButton:
		if b.Region != nil {
			return errors.New("mouseButton takes no region")
		}
		return validateButton(b.Button)
	case KindMouseRegion:
		if err := validateButton(b.Button); err != nil {
			return err
		}
		return validateRegion(b.Region)
	default:
		return fmt.Errorf("kind %q must be %s, %s or %s", b.Kind, KindTouchRegion, KindMouseButton, KindMouseRegion)
	}
}

func validateButton(button string) error {
	if !slices.Contains(mouseButtons, button) {
		return fmt.Errorf("button %q must be one of %s", button, strings.Join(mouseButtons, ", "))
	}
	return nil
}

func validateRegion(r *Rect) error {
	if r == nil {
		return errors.New("region is required")
	}
	if !isFinite32(r.X) || !isFinite32(r.Y) || !positive(r.Width) || !positive(r.Height) {
		return fmt.Errorf("region (%v, %v, %v, %v) must be finite with a positive size", r.X, r.Y, r.Width, r.Height)
	}
	return nil
}

func validateAudio(a *Audio) error {
	if a.Master != nil && !unit32(*a.Master) {
		return fmt.Errorf("project: audio master %v must be in [0, 1]", *a.Master)
	}
	buses := make(map[string]struct{}, len(a.Buses))
	for i, b := range a.Buses {
		if err := validateName(fmt.Sprintf("audio bus %d", i), b.Name); err != nil {
			return err
		}
		if _, dup := buses[b.Name]; dup {
			return fmt.Errorf("project: duplicate audio bus %q", b.Name)
		}
		buses[b.Name] = struct{}{}
		if b.Volume != nil && !unit32(*b.Volume) {
			return fmt.Errorf("project: audio bus %q volume %v must be in [0, 1]", b.Name, *b.Volume)
		}
		for j, e := range b.Effects {
			if err := validateEffect(e); err != nil {
				return fmt.Errorf("project: audio bus %q effect %d: %w", b.Name, j, err)
			}
		}
	}
	return nil
}

func validateEffect(e Effect) error {
	set := 0
	for _, isSet := range []bool{e.LowPass != nil, e.HighPass != nil, e.Delay != nil, e.Reverb != nil, e.Gain != nil, e.Bitcrush != nil} {
		if isSet {
			set++
		}
	}
	if set != 1 {
		return errors.New("must set exactly one of lowPass, highPass, delay, reverb, gain and bitcrush")
	}
	switch {
	case e.LowPass != nil:
		return validateFilter(e.LowPass)
	case e.HighPass != nil:
		return validateFilter(e.HighPass)
	case e.Delay != nil:
		d := e.Delay
		if d.Time <= 0 {
			return fmt.Errorf("delay time %v must be positive", d.Time)
		}
		if !isFinite64(d.Feedback) || d.Feedback < 0 || d.Feedback > 0.99 {
			return fmt.Errorf("delay feedback %v must be in [0, 0.99]", d.Feedback)
		}
		if !unit64(d.Wet) {
			return fmt.Errorf("delay wet %v must be in [0, 1]", d.Wet)
		}
	case e.Reverb != nil:
		r := e.Reverb
		if !unit64(r.RoomSize) || !unit64(r.Damping) || !unit64(r.Wet) {
			return errors.New("reverb roomSize, damping and wet must be in [0, 1]")
		}
	case e.Gain != nil:
		if !isFinite64(e.Gain.Factor) || e.Gain.Factor < 0 {
			return fmt.Errorf("gain factor %v must be finite and non-negative", e.Gain.Factor)
		}
	case e.Bitcrush != nil:
		b := e.Bitcrush
		if b.Bits < 1 || b.Bits > 15 {
			return fmt.Errorf("bitcrush bits %d must be in [1, 15]", b.Bits)
		}
		if b.Downsample < 0 {
			return fmt.Errorf("bitcrush downsample %d must not be negative", b.Downsample)
		}
	}
	return nil
}

func validateFilter(f *Filter) error {
	if !isFinite64(f.CutoffHz) || f.CutoffHz <= 0 {
		return fmt.Errorf("filter cutoffHz %v must be finite and positive", f.CutoffHz)
	}
	if !isFinite64(f.Q) || f.Q < 0 {
		return fmt.Errorf("filter q %v must be finite and non-negative", f.Q)
	}
	return nil
}

func validateAutoload(a *Autoload) error {
	scenes, err := validateEntries("scene", a.Scenes)
	if err != nil {
		return err
	}
	if _, err := validateEntries("resource", a.Resources); err != nil {
		return err
	}
	for _, s := range a.Scenes {
		if !strings.HasSuffix(s.Path, ".yaml") && !strings.HasSuffix(s.Path, ".yml") {
			return fmt.Errorf("project: autoload scene %q path %q must be a .yaml or .yml file", s.Name, s.Path)
		}
	}
	if _, ok := scenes[a.MainScene]; a.MainScene != "" && !ok {
		return fmt.Errorf("project: autoload mainScene %q is not an autoloaded scene", a.MainScene)
	}
	return nil
}

func validateEntries(kind string, entries []Entry) (map[string]struct{}, error) {
	names := make(map[string]struct{}, len(entries))
	for i, e := range entries {
		if err := validateName(fmt.Sprintf("autoload %s %d", kind, i), e.Name); err != nil {
			return nil, err
		}
		if _, dup := names[e.Name]; dup {
			return nil, fmt.Errorf("project: duplicate autoload %s %q", kind, e.Name)
		}
		names[e.Name] = struct{}{}
		if e.Path == "." || !fs.ValidPath(e.Path) || strings.Contains(e.Path, `\`) {
			return nil, fmt.Errorf("project: autoload %s %q path %q must be a slash-separated path inside the project", kind, e.Name, e.Path)
		}
	}
	return names, nil
}

func validateExport(e *Export) error {
	if e.Output != "" && (e.Output == "." || !fs.ValidPath(e.Output) || strings.Contains(e.Output, `\`)) {
		return fmt.Errorf("project: export output %q must be a slash-separated directory inside the project", e.Output)
	}
	platforms := make(map[string]struct{}, len(e.Presets))
	for _, p := range e.Presets {
		if !slices.Contains(Platforms, p.Platform) {
			return fmt.Errorf("project: export platform %q must be one of %s", p.Platform, strings.Join(Platforms, ", "))
		}
		if _, dup := platforms[p.Platform]; dup {
			return fmt.Errorf("project: duplicate export preset for platform %q", p.Platform)
		}
		platforms[p.Platform] = struct{}{}
		seen := make(map[string]struct{}, len(p.Targets))
		for _, t := range p.Targets {
			if !slices.Contains(nsgoTargets, t) || platformOf(t) != p.Platform {
				return fmt.Errorf("project: export platform %q target %q must be one of %s", p.Platform, t, strings.Join(TargetsOf(p.Platform), ", "))
			}
			if _, dup := seen[t]; dup {
				return fmt.Errorf("project: export platform %q lists target %q twice", p.Platform, t)
			}
			seen[t] = struct{}{}
		}
	}
	return nil
}

func validateEditor(e *Editor) error {
	seen := make(map[string]struct{}, len(e.Plugins))
	for i, name := range e.Plugins {
		if err := validateName(fmt.Sprintf("editor plugin %d", i), name); err != nil {
			return err
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("project: editor plugin %q is listed twice", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validatePacks(f *File) error {
	names := make(map[string]struct{}, len(f.Packs))
	// owner maps each file, autoload entry and plugin a pack claims to that pack, so no two packs claim one. A file's path is folded to lower case, as a case-insensitive file system such as macOS's or Windows's sees it.
	owner := map[string]string{}
	claim := func(pack, key, what string) error {
		if other, ok := owner[key]; ok {
			return fmt.Errorf("project: packs %q and %q both claim %s", other, pack, what)
		}
		owner[key] = pack
		return nil
	}
	for i := range f.Packs {
		p := &f.Packs[i]
		if err := ValidatePackName(p.Name); err != nil {
			return fmt.Errorf("project: pack %d: %w", i, err)
		}
		if _, dup := names[p.Name]; dup {
			return fmt.Errorf("project: pack %q is listed twice", p.Name)
		}
		names[p.Name] = struct{}{}
		where := fmt.Sprintf("project: pack %q", p.Name)
		if !slices.Contains(PackKinds, p.Kind) {
			return fmt.Errorf("%s: kind %q must be one of %s", where, p.Kind, strings.Join(PackKinds, ", "))
		}
		if err := ValidatePackVersion(p.PackVersion); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if !IsSHA256(p.SHA256) {
			return fmt.Errorf("%s: sha256 %q must be 64 lower-case hexadecimal digits", where, p.SHA256)
		}
		if !isProjectPath(p.Dir) {
			return fmt.Errorf("%s: dir %q must be a slash-separated directory inside the project", where, p.Dir)
		}
		for j, file := range p.Files {
			if !isProjectPath(file) {
				return fmt.Errorf("%s: file %q must be a slash-separated path inside its dir", where, file)
			}
			if j > 0 && p.Files[j-1] >= file {
				return fmt.Errorf("%s: files must be sorted and unique, but %q follows %q", where, file, p.Files[j-1])
			}
			what := "file " + path.Join(p.Dir, file)
			if err := claim(p.Name, strings.ToLower(what), what); err != nil {
				return err
			}
		}
		for _, ref := range []struct {
			kind  string
			names []string
			has   func(string) bool
		}{
			{"autoload scene", p.Scenes, func(n string) bool { return f.Autoload != nil && hasEntry(f.Autoload.Scenes, n) }},
			{"autoload resource", p.Resources, func(n string) bool { return f.Autoload != nil && hasEntry(f.Autoload.Resources, n) }},
			{"editor plugin", p.Plugins, func(n string) bool { return f.Editor != nil && slices.Contains(f.Editor.Plugins, n) }},
		} {
			for _, n := range ref.names {
				if !ref.has(n) {
					return fmt.Errorf("%s: added %s %q, which the project does not have", where, ref.kind, n)
				}
				what := ref.kind + " " + strconv.Quote(n)
				if err := claim(p.Name, what, what); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func hasEntry(entries []Entry, name string) bool {
	return slices.ContainsFunc(entries, func(e Entry) bool { return e.Name == name })
}

// isProjectPath reports whether p is a slash-separated relative path that stays inside the directory it is relative to, and is not that directory itself.
func isProjectPath(p string) bool {
	return p != "." && fs.ValidPath(p) && !strings.Contains(p, `\`)
}

// ValidatePackName reports whether name can name a library pack: 1 to 64 lower-case ASCII letters, digits and '-', starting with a letter or digit, so that it is also a directory name on every platform.
func ValidatePackName(name string) error {
	if name == "" || len(name) > 64 || name[0] == '-' {
		return fmt.Errorf("pack name %q must be 1 to 64 lower-case letters, digits and '-', starting with a letter or digit", name)
	}
	for i := range len(name) {
		if c := name[i]; (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return fmt.Errorf("pack name %q must be 1 to 64 lower-case letters, digits and '-', starting with a letter or digit", name)
		}
	}
	return nil
}

// ValidatePackVersion reports whether v can be a pack's version: 1 to 64 characters with no spaces or control characters, such as 1.2.0.
func ValidatePackVersion(v string) error {
	if v == "" || len(v) > 64 || strings.IndexFunc(v, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("packVersion %q must be 1 to 64 characters without spaces", v)
	}
	return nil
}

// IsSHA256 reports whether s is a SHA-256 digest written as 64 lower-case hexadecimal digits.
func IsSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validateName refuses empty names and names with spaces or control characters, which input actions, axes, buses, autoload entries and editor plugins are looked up by.
func validateName(where, name string) error {
	if name == "" {
		return fmt.Errorf("project: %s has an empty name", where)
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("project: %s name %q must have no spaces or control characters", where, name)
	}
	return nil
}

func positive(v float32) bool { return isFinite32(v) && v > 0 }

func unit32(v float32) bool { return isFinite32(v) && v >= 0 && v <= 1 }

func unit64(v float64) bool { return isFinite64(v) && v >= 0 && v <= 1 }

func isFinite32(v float32) bool { return isFinite64(float64(v)) }

func isFinite64(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

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

	"github.com/nsgo-dev/game/internal/semver"
	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// FileName is the project file's name at a game's root.
const FileName = "game.yaml"

// CurrentVersion is the project file schema version this package reads and writes.
const CurrentVersion = 1

// SchemaURL is where the JSON Schema of version 1 project files (game.yaml) is published.
const SchemaURL = "https://game.nsgo.dev/schemas/game.v1.json"

// File is the root of a versioned game.yaml project file.
type File struct {
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Name is the game's display name, which nsgo.yaml's name follows.
	Name string `yaml:"name" schema:"required"`
	// ID is the game's reverse-DNS application identifier, such as dev.nsgo.caverun, which nsgo.yaml's id follows.
	ID string `yaml:"id" schema:"required"`
	// AppVersion is the game's release version, such as 0.1.0, which nsgo.yaml's version follows.
	AppVersion string `yaml:"appVersion" schema:"required"`
	// Window is the main window's size, limits, title and appearance.
	Window Window `yaml:"window" schema:"required"`
	// Display is the virtual resolution the game draws at; nil draws at the window's own size.
	Display *Display `yaml:"display,omitempty"`
	// Input is the input map bound into an input.Actions; nil binds nothing.
	Input *Input `yaml:"input,omitempty"`
	// Audio holds the master volume and the audio buses a sound.Mixer is configured with; nil configures nothing.
	Audio *Audio `yaml:"audio,omitempty"`
	// Physics holds the default 2D and 3D gravity, solver iterations and named collision layers shared by scenes; nil uses each engine's defaults.
	Physics *Physics `yaml:"physics,omitempty"`
	// Autoload lists the scenes and resources loaded at startup; nil loads nothing.
	Autoload *Autoload `yaml:"autoload,omitempty"`
	// Export holds the build output directory and the export presets per platform, which nsgo.yaml's build section follows; nil leaves nsgo.yaml without them.
	Export *Export `yaml:"export,omitempty"`
	// Editor holds the scene editor's settings for this project; nil uses its defaults.
	Editor *Editor `yaml:"editor,omitempty"`
	// Assets declares the atlases, textures, audio, fonts, models and asset manifest game assets builds; nil declares none.
	Assets *Assets `yaml:"assets,omitempty"`
	// Navigation holds the settings and agent types game navmesh bakes a scene's navigation meshes with (see the navbake3d package); nil bakes one agent type called DefaultNavAgent with the default settings.
	Navigation *Navigation `yaml:"navigation,omitempty"`
	// Packs records the library packs installed into the project, in the order they were first installed: the files each wrote and the autoload entries and editor plugins each added, so that a later install of the pack can update it and a removal can undo it (see the tooling/library package). nil records none.
	Packs []Pack `yaml:"packs,omitempty"`
}

// Window is the main window's content size in points, its size limits, its title and its appearance.
type Window struct {
	// Title is the window's title; empty uses File.Name.
	Title string `yaml:"title,omitempty"`
	// Width is the window's starting content width in points; it must be positive.
	Width float32 `yaml:"width" schema:"required,exclusiveMinimum=0"`
	// Height is the window's starting content height in points; it must be positive.
	Height float32 `yaml:"height" schema:"required,exclusiveMinimum=0"`
	// MinWidth is the smallest content width the user may resize the window to; 0 is no limit.
	MinWidth float32 `yaml:"minWidth,omitempty" schema:"minimum=0"`
	// MinHeight is the smallest content height the user may resize the window to; 0 is no limit.
	MinHeight float32 `yaml:"minHeight,omitempty" schema:"minimum=0"`
	// MaxWidth is the largest content width the user may resize the window to; 0 is no limit.
	MaxWidth float32 `yaml:"maxWidth,omitempty" schema:"minimum=0"`
	// MaxHeight is the largest content height the user may resize the window to; 0 is no limit.
	MaxHeight float32 `yaml:"maxHeight,omitempty" schema:"minimum=0"`
	// Resizable says whether the user may resize the window; nil defaults to true.
	Resizable *bool `yaml:"resizable,omitempty"`
	// Appearance starts the app "light" or "dark" whatever the system's; empty follows the system. nsgo.yaml's appearance follows it.
	Appearance string `yaml:"appearance,omitempty" schema:"enum=light|dark"`
}

// Display is the virtual resolution a screen.Viewport maps onto the window.
type Display struct {
	// Width is the virtual canvas width in game points; it must be positive.
	Width float32 `yaml:"width" schema:"required,exclusiveMinimum=0"`
	// Height is the virtual canvas height in game points; it must be positive.
	Height float32 `yaml:"height" schema:"required,exclusiveMinimum=0"`
	// Mode is how the canvas fits a window of another aspect ratio: "fit" (the default when empty), "fill", "stretch" or "expand", as screen.Mode names them.
	Mode string `yaml:"mode,omitempty" schema:"enum=fit|fill|stretch|expand"`
	// IntegerScale scales by whole multiples only, as screen.Viewport.IntegerScale does.
	IntegerScale bool `yaml:"integerScale,omitempty"`
	// PixelSnap aligns the canvas to whole points, as screen.Viewport.PixelSnap does.
	PixelSnap bool `yaml:"pixelSnap,omitempty"`
	// BarColor is the CSS color of letterbox and pillarbox bars, parsed with core.ParseColor; empty is opaque black.
	BarColor string `yaml:"barColor,omitempty"`
}

// Input is a game's input map: named actions with their physical bindings, axes over pairs of actions, device motion axes and game controller stick axes.
type Input struct {
	// Actions are the named actions in the order they are declared.
	Actions []Action `yaml:"actions,omitempty"`
	// Axes are the named axes, each reading -1 for its negative action and +1 for its positive one.
	Axes []Axis `yaml:"axes,omitempty"`
	// MotionAxes are the named axes read from the device's tilt and gyroscope.
	MotionAxes []MotionAxis `yaml:"motionAxes,omitempty"`
	// GamepadAxes are the named axes read from a connected game controller's analog sticks.
	GamepadAxes []GamepadAxis `yaml:"gamepadAxes,omitempty"`
}

// Action is one named action and the physical inputs that trigger it.
type Action struct {
	// Name uniquely identifies the action.
	Name string `yaml:"name" schema:"required"`
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
	// KindKey is Key held on a keyboard, following the active keyboard layout (input.Key).
	KindKey = "key"
	// KindKeyCode is Code held at a physical keyboard position, independent of layout (input.KeyCode).
	KindKeyCode = "keyCode"
	// KindGamepadButton is Button held on a connected game controller (input.GamepadButton).
	KindGamepadButton = "gamepadButton"
)

// BindingKinds lists every binding kind a game.yaml action may declare, in display order.
var BindingKinds = []string{KindTouchRegion, KindMouseButton, KindMouseRegion, KindKey, KindKeyCode, KindGamepadButton}

// MouseButtons lists the mouse button names accepted by KindMouseButton and KindMouseRegion.
var MouseButtons = []string{"left", "right", "middle", "back", "forward"}

// GamepadButtons lists the W3C Standard Gamepad button names accepted by KindGamepadButton.
var GamepadButtons = []string{
	"south", "east", "west", "north",
	"leftShoulder", "rightShoulder", "leftTrigger", "rightTrigger",
	"back", "start", "leftStick", "rightStick",
	"dpadUp", "dpadDown", "dpadLeft", "dpadRight",
	"home",
}

// MotionAxes lists the device motion sensor axis names accepted by MotionAxis.Axis.
var MotionAxes = []string{"tiltX", "tiltY", "tiltZ", "gyroX", "gyroY", "gyroZ"}

// GamepadAxes lists the W3C Standard Gamepad stick axis names accepted by GamepadAxis.Axis.
var GamepadAxes = []string{"leftX", "leftY", "rightX", "rightY"}

// Binding is one physical input bound to an action.
type Binding struct {
	// Kind is KindTouchRegion, KindMouseButton, KindMouseRegion, KindKey, KindKeyCode or KindGamepadButton.
	Kind string `yaml:"kind" schema:"required,enum=touchRegion|mouseButton|mouseRegion|key|keyCode|gamepadButton"`
	// Button is a mouse button ("left", "right", "middle", "back" or "forward") for KindMouseButton and KindMouseRegion, or a standard gamepad button (one of GamepadButtons) for KindGamepadButton; refused by other kinds.
	Button string `yaml:"button,omitempty"`
	// Key is the keyboard key name (such as "a", "space", "left", "enter" or "shift") for KindKey, matched case-insensitively as input.Key does; refused by other kinds.
	Key string `yaml:"key,omitempty"`
	// Code is the physical keyboard position (such as "KeyW", "ArrowLeft", "Space" or "ShiftLeft") for KindKeyCode, as input.KeyCode reads it; refused by other kinds.
	Code string `yaml:"code,omitempty"`
	// Region is the rectangle in virtual coordinates (or view coordinates without a Display) that KindTouchRegion and KindMouseRegion test; refused by other kinds.
	Region *Rect `yaml:"region,omitempty,flow"`
}

// Rect is an axis-aligned rectangle with a positive size.
type Rect struct {
	// X is the horizontal origin in virtual points.
	X float32 `yaml:"x,omitempty"`
	// Y is the vertical origin in virtual points.
	Y float32 `yaml:"y,omitempty"`
	// Width is the rectangle's width in virtual points; it must be positive.
	Width float32 `yaml:"width" schema:"required,exclusiveMinimum=0"`
	// Height is the rectangle's height in virtual points; it must be positive.
	Height float32 `yaml:"height" schema:"required,exclusiveMinimum=0"`
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
	Name string `yaml:"name" schema:"required"`
	// Negative names a declared action that drives the axis toward -1; at least one of Negative and Positive is required.
	Negative string `yaml:"negative,omitempty"`
	// Positive names a declared action that drives the axis toward +1; at least one of Negative and Positive is required.
	Positive string `yaml:"positive,omitempty"`
}

// MotionAxis is a named axis read from a device motion sensor, as input.Actions.BindMotionAxis binds it.
type MotionAxis struct {
	// Name uniquely identifies the motion axis.
	Name string `yaml:"name" schema:"required"`
	// Axis is "tiltX", "tiltY", "tiltZ", "gyroX", "gyroY" or "gyroZ".
	Axis string `yaml:"axis" schema:"required,enum=tiltX|tiltY|tiltZ|gyroX|gyroY|gyroZ"`
	// DeadZone is the sensor dead zone in [0, 1); 0 takes input.MotionAxisOptions' default.
	DeadZone float64 `yaml:"deadZone,omitempty" schema:"minimum=0,exclusiveMaximum=1"`
	// Sensitivity scales the reading; 0 takes input.MotionAxisOptions' default.
	Sensitivity float64 `yaml:"sensitivity,omitempty" schema:"minimum=0"`
	// MaxRange is the full-deflection sensor range; 0 takes input.MotionAxisOptions' default.
	MaxRange float64 `yaml:"maxRange,omitempty" schema:"minimum=0"`
	// Smoothing is the low-pass smoothing factor in [0, 1); 0 disables smoothing.
	Smoothing float64 `yaml:"smoothing,omitempty" schema:"minimum=0,exclusiveMaximum=1"`
	// Invert negates the resulting axis value.
	Invert bool `yaml:"invert,omitempty"`
}

// GamepadAxis is a named axis read from a connected game controller's analog stick, as input.Actions.BindGamepadAxis binds it.
type GamepadAxis struct {
	// Name uniquely identifies the gamepad axis.
	Name string `yaml:"name" schema:"required"`
	// Axis is "leftX", "leftY", "rightX" or "rightY".
	Axis string `yaml:"axis" schema:"required,enum=leftX|leftY|rightX|rightY"`
	// DeadZone is the stick deflection below which the axis reads 0, strictly less than 1; 0 uses input.DefaultGamepadDeadZone and a negative value turns the dead zone off.
	DeadZone float64 `yaml:"deadZone,omitempty" schema:"exclusiveMaximum=1"`
	// Axial applies DeadZone to this axis alone instead of radially over both axes of the stick.
	Axial bool `yaml:"axial,omitempty"`
	// Sensitivity multiplies the dead-zoned reading before clamping to [-1, 1]; 0 uses 1.
	Sensitivity float64 `yaml:"sensitivity,omitempty" schema:"minimum=0"`
	// Invert negates the resulting axis value.
	Invert bool `yaml:"invert,omitempty"`
}

// Audio is the master volume and the buses (sound.Mixer groups) a game plays through.
type Audio struct {
	// Master is the default master volume in [0, 1]; nil leaves it at 1.
	Master *float32 `yaml:"master,omitempty" schema:"minimum=0,maximum=1"`
	// Buses are the mixer groups in declaration order.
	Buses []Bus `yaml:"buses,omitempty"`
}

// Bus is one sound.Mixer group: its default volume and its DSP effect chain.
type Bus struct {
	// Name is the mixer group, such as "music" or "sfx".
	Name string `yaml:"name" schema:"required"`
	// Volume is the default volume in [0, 1] until the player sets one; nil leaves it at 1.
	Volume *float32 `yaml:"volume,omitempty" schema:"minimum=0,maximum=1"`
	// Effects is the bus's DSP chain, applied in order.
	Effects []Effect `yaml:"effects,omitempty"`
}

// Effect is one DSP effect on a bus; exactly one field must be set.
type Effect struct {
	// LowPass is a biquad low-pass filter.
	LowPass *Filter `yaml:"lowPass,omitempty,flow"`
	// HighPass is a biquad high-pass filter.
	HighPass *Filter `yaml:"highPass,omitempty,flow"`
	// Delay is a feedback delay line.
	Delay *Delay `yaml:"delay,omitempty,flow"`
	// Reverb is a Freeverb-style reverb.
	Reverb *Reverb `yaml:"reverb,omitempty,flow"`
	// Gain scales every sample by a non-negative factor.
	Gain *Gain `yaml:"gain,omitempty,flow"`
	// Bitcrush quantizes bit depth and sample rate.
	Bitcrush *Bitcrush `yaml:"bitcrush,omitempty,flow"`
}

// Filter is a biquad low-pass or high-pass filter (sound.LowPass, sound.HighPass).
type Filter struct {
	// CutoffHz is the cutoff frequency in hertz; it must be positive.
	CutoffHz float64 `yaml:"cutoffHz" schema:"required,exclusiveMinimum=0"`
	// Q is the filter's quality factor; 0 is Butterworth.
	Q float64 `yaml:"q,omitempty" schema:"minimum=0"`
}

// Delay is a feedback delay line (sound.Delay).
type Delay struct {
	// Time is the echo spacing, such as 250ms; it must be positive.
	Time time.Duration `yaml:"time" schema:"required"`
	// Feedback is the feedback gain in [0, 0.99].
	Feedback float64 `yaml:"feedback,omitempty" schema:"minimum=0,maximum=0.99"`
	// Wet is the wet mix level in [0, 1].
	Wet float64 `yaml:"wet,omitempty" schema:"minimum=0,maximum=1"`
}

// Reverb is a Freeverb-style reverb (sound.Reverb); each field is in [0, 1].
type Reverb struct {
	// RoomSize controls reverb decay size in [0, 1].
	RoomSize float64 `yaml:"roomSize,omitempty" schema:"minimum=0,maximum=1"`
	// Damping controls high-frequency attenuation in [0, 1].
	Damping float64 `yaml:"damping,omitempty" schema:"minimum=0,maximum=1"`
	// Wet is the wet mix level in [0, 1].
	Wet float64 `yaml:"wet,omitempty" schema:"minimum=0,maximum=1"`
}

// Gain scales every sample by Factor (sound.Gain), which must be finite and non-negative.
type Gain struct {
	// Factor is the non-negative linear gain multiplier.
	Factor float64 `yaml:"factor" schema:"required,minimum=0"`
}

// Bitcrush is a lo-fi quantizer (sound.Bitcrush): Bits in [1, 15] and Downsample 0 or more, where 0 and 1 keep every frame.
type Bitcrush struct {
	// Bits is the quantized bit depth in [1, 15].
	Bits int `yaml:"bits" schema:"required,minimum=1,maximum=15"`
	// Downsample holds each sample for this many frames; 0 and 1 keep every frame.
	Downsample int `yaml:"downsample,omitempty" schema:"minimum=0"`
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
	Name string `yaml:"name" schema:"required"`
	// Path is the file's slash-separated path relative to the project root, as fs.ValidPath accepts it.
	Path string `yaml:"path" schema:"required"`
}

// Export holds the build output directory, optional release budgets, and one export preset per platform.
type Export struct {
	// Output is the directory builds are written to; empty is NSGo's default (dist).
	Output string `yaml:"output,omitempty"`
	// Budgets are optional release thresholds checked before and after each build (download size and frame time p95 at a quality profile).
	Budgets *Budgets `yaml:"budgets,omitempty"`
	// Presets are the platforms the game exports to, in build order.
	Presets []Preset `yaml:"presets,omitempty"`
}

// Budgets holds release size and performance thresholds checked against each built package.
type Budgets struct {
	// QualityProfile names the quality preset frameTimeP95Ms is measured at (for example "low", "medium", or "high"); empty defaults to "low".
	QualityProfile string `yaml:"qualityProfile,omitempty"`
	// DownloadSizeMB is the maximum acceptable release download size in megabytes; 0 leaves it unset.
	DownloadSizeMB float64 `yaml:"downloadSizeMB,omitempty" schema:"minimum=0"`
	// FrameTimeP95Ms is the maximum acceptable 95th-percentile frame time in milliseconds; 0 leaves it unset.
	FrameTimeP95Ms float64 `yaml:"frameTimeP95Ms,omitempty" schema:"minimum=0"`
}

// Preset is the export preset of one platform.
type Preset struct {
	// Platform is one of Platforms, such as "web" or "macos".
	Platform string `yaml:"platform" schema:"required,enum=macos|windows|linux|ios|tvos|watchos|android|harmonyos|web|miniprogram"`
	// Targets are the NSGo build targets of the platform to build, such as macos_arm64; empty builds the platform name itself, which NSGo expands to its default targets.
	Targets []string `yaml:"targets,omitempty,flow"`
}

// Editor is the scene editor's settings for one project.
type Editor struct {
	// Plugins names the editor plugins the scene editor turns on for this project, in the order their panels and tools appear. A plugin is Go code built into the editor binary (editor.Plugin), so this list only turns plugins on; each name is unique and has no spaces or control characters.
	Plugins []string `yaml:"plugins,omitempty,flow"`
}

// DefaultNavAgent names the agent type game navmesh bakes for when game.yaml declares none.
const DefaultNavAgent = "default"

// MaxNavVertsPerPoly is the largest Navigation.MaxVertsPerPoly.
const MaxNavVertsPerPoly = 12

// Navigation is the navigation mesh bake settings of a project, as navbake3d.Settings defines them: each field 0 or omitted takes navbake3d's default. The scene's own layers are all baked.
type Navigation struct {
	// CellSize is the width and depth of one cell of the voxel grid the level is baked on, in world units.
	CellSize float32 `yaml:"cellSize,omitempty"`
	// CellHeight is the height of one cell, the precision of heights, clearances and steps.
	CellHeight float32 `yaml:"cellHeight,omitempty"`
	// MinRegionArea is the smallest area of an island of walkable surface that is kept.
	MinRegionArea float32 `yaml:"minRegionArea,omitempty"`
	// MaxEdgeLength is the longest a polygon edge along a wall may be.
	MaxEdgeLength float32 `yaml:"maxEdgeLength,omitempty"`
	// MaxSimplificationError is the farthest a simplified outline may stray from the walkable area.
	MaxSimplificationError float32 `yaml:"maxSimplificationError,omitempty"`
	// MaxVertsPerPoly is the largest number of vertices in one polygon, from 3 to MaxNavVertsPerPoly.
	MaxVertsPerPoly int `yaml:"maxVertsPerPoly,omitempty"`
	// DetailSampleDistance is the spacing of the height samples polygons follow uneven ground with, at least CellSize.
	DetailSampleDistance float32 `yaml:"detailSampleDistance,omitempty"`
	// DetailSampleMaxError is how far the polygons' triangles may stray from the sampled height.
	DetailSampleMaxError float32 `yaml:"detailSampleMaxError,omitempty"`
	// Agents are the agent types a scene gets a navigation mesh each for, in the order the baked file lists them; empty bakes one called DefaultNavAgent.
	Agents []NavAgent `yaml:"agents,omitempty"`
}

// NavAgent is one agent type navigation meshes are baked for, as navbake3d.AgentType defines it; each size 0 or omitted takes navbake3d's default.
type NavAgent struct {
	// Name uniquely identifies the agent type, such as "humanoid".
	Name string `yaml:"name"`
	// Radius is how far the mesh keeps from walls and ledges.
	Radius float32 `yaml:"radius,omitempty"`
	// Height is the clearance the agent needs.
	Height float32 `yaml:"height,omitempty"`
	// MaxSlope is the steepest walkable slope in degrees, below 90.
	MaxSlope float32 `yaml:"maxSlope,omitempty"`
	// MaxClimb is the highest step the agent climbs.
	MaxClimb float32 `yaml:"maxClimb,omitempty"`
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
	Name string `yaml:"name" schema:"required"`
	// Kind is one of PackKinds.
	Kind string `yaml:"kind" schema:"required,enum=asset|plugin|template"`
	// PackVersion is the version of the pack that was installed, such as 1.2.0.
	PackVersion string `yaml:"packVersion" schema:"required"`
	// SHA256 is the pack's digest, 64 lower-case hexadecimal digits, which tells one release of the pack from another whatever its version says.
	SHA256 string `yaml:"sha256" schema:"required"`
	// Dir is the slash-separated directory inside the project the pack's files were written to.
	Dir string `yaml:"dir" schema:"required"`
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

const fixProject = "check game.yaml against the project schema (version: 1 with name, id, appVersion, and window)"

// Parse decodes a YAML project file and validates it. Unknown fields and trailing YAML documents are refused.
func Parse(data []byte) (*File, error) {
	f, err := parse(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixProject)
	}
	return f, nil
}

func parse(data []byte) (*File, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Errorf("", fixProject, "project: empty project file")
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
			return nil, loaderr.Errorf("", "keep one YAML document in game.yaml", "project: unexpected trailing YAML document")
		}
		return nil, fmt.Errorf("project: decode: %w", err)
	}
	if err := Validate(&f); err != nil {
		return nil, err
	}
	if f.Assets != nil {
		f.Assets.normalize()
	}
	yamlzero.Normalize(&f)
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
	f, err := Parse(data)
	if err != nil {
		return nil, loaderr.WithFile(err, FileName)
	}
	return f, nil
}

// Encode validates f and serializes it as deterministic 2-space YAML that Parse reads back unchanged.
func Encode(f *File) ([]byte, error) {
	if err := Validate(f); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(f)); err != nil {
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
			Axes:        append([]Axis(nil), f.Input.Axes...),
			MotionAxes:  append([]MotionAxis(nil), f.Input.MotionAxes...),
			GamepadAxes: append([]GamepadAxis(nil), f.Input.GamepadAxes...),
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
	c.Physics = clonePhysics(f.Physics)
	if f.Autoload != nil {
		c.Autoload = &Autoload{
			MainScene: f.Autoload.MainScene,
			Scenes:    append([]Entry(nil), f.Autoload.Scenes...),
			Resources: append([]Entry(nil), f.Autoload.Resources...),
		}
	}
	if f.Export != nil {
		e := Export{
			Output:  f.Export.Output,
			Budgets: clonePtr(f.Export.Budgets),
		}
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
	if f.Navigation != nil {
		n := *f.Navigation
		n.Agents = slices.Clone(f.Navigation.Agents)
		c.Navigation = &n
	}
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
		return loaderr.Errorf("", fixProject, "project: nil project file")
	}
	if f.Version != CurrentVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d", CurrentVersion), "project: unsupported version %d (want %d)", f.Version, CurrentVersion)
	}
	if err := validateText("name", "name", f.Name, true); err != nil {
		return err
	}
	if err := validateID(f.ID); err != nil {
		return err
	}
	if f.AppVersion == "" || strings.IndexFunc(f.AppVersion, unicode.IsSpace) >= 0 || strings.IndexFunc(f.AppVersion, unicode.IsControl) >= 0 {
		return loaderr.Errorf("appVersion", "set appVersion to a non-empty version string without spaces", "project: appVersion %q must be non-empty without spaces", f.AppVersion)
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
	if f.Physics != nil {
		if err := validatePhysics(f.Physics); err != nil {
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
	if f.Navigation != nil {
		if err := validateNavigation(f.Navigation); err != nil {
			return err
		}
	}
	return validatePacks(f)
}

// validateNavigation refuses negative or non-finite sizes, a detail sample spacing below the cell size, a vertex limit out of range, slopes of 90 degrees or more and agent types without a name or with one repeated.
func validateNavigation(n *Navigation) error {
	for _, v := range []struct {
		field string
		v     float32
	}{
		{"cellSize", n.CellSize}, {"cellHeight", n.CellHeight}, {"minRegionArea", n.MinRegionArea},
		{"maxEdgeLength", n.MaxEdgeLength}, {"maxSimplificationError", n.MaxSimplificationError},
		{"detailSampleDistance", n.DetailSampleDistance}, {"detailSampleMaxError", n.DetailSampleMaxError},
	} {
		if !isFinite32(v.v) || v.v < 0 {
			return loaderr.Errorf("navigation."+v.field, fmt.Sprintf("set navigation.%s to a finite number >= 0", v.field), "project: navigation %s %v must be a finite number, 0 or more", v.field, v.v)
		}
	}
	if n.DetailSampleDistance != 0 && n.DetailSampleDistance < n.CellSize {
		return loaderr.Errorf("navigation.detailSampleDistance", "set navigation.detailSampleDistance to 0 or at least navigation.cellSize", "project: navigation detailSampleDistance %v must be at least cellSize %v", n.DetailSampleDistance, n.CellSize)
	}
	if n.MaxVertsPerPoly != 0 && (n.MaxVertsPerPoly < 3 || n.MaxVertsPerPoly > MaxNavVertsPerPoly) {
		return loaderr.Errorf("navigation.maxVertsPerPoly", fmt.Sprintf("set navigation.maxVertsPerPoly to 0 or between 3 and %d", MaxNavVertsPerPoly), "project: navigation maxVertsPerPoly %d must be 0 or from 3 to %d", n.MaxVertsPerPoly, MaxNavVertsPerPoly)
	}
	seen := make(map[string]struct{}, len(n.Agents))
	for i, a := range n.Agents {
		prefix := fmt.Sprintf("navigation.agents[%d]", i)
		if err := validateNameAt(prefix+".name", fmt.Sprintf("navigation agent %d", i), a.Name); err != nil {
			return err
		}
		if _, dup := seen[a.Name]; dup {
			return loaderr.Errorf(prefix+".name", "give each navigation agent a unique name", "project: navigation agent %q is listed twice", a.Name)
		}
		seen[a.Name] = struct{}{}
		for _, v := range []struct {
			field string
			v     float32
		}{{"radius", a.Radius}, {"height", a.Height}, {"maxSlope", a.MaxSlope}, {"maxClimb", a.MaxClimb}} {
			if !isFinite32(v.v) || v.v < 0 {
				return loaderr.Errorf(prefix+"."+v.field, fmt.Sprintf("set %s to a finite number >= 0", v.field), "project: navigation agent %q %s %v must be a finite number, 0 or more", a.Name, v.field, v.v)
			}
		}
		if a.MaxSlope >= 90 {
			return loaderr.Errorf(prefix+".maxSlope", "set maxSlope below 90 degrees", "project: navigation agent %q maxSlope %v must be below 90 degrees", a.Name, a.MaxSlope)
		}
	}
	return nil
}

// validateText refuses control characters and surrounding spaces, and an empty value when required.
func validateText(pathField, label, s string, required bool) error {
	if s == "" {
		if required {
			return loaderr.Errorf(pathField, fmt.Sprintf("set %s to a non-empty string", pathField), "project: %s is required", label)
		}
		return nil
	}
	if strings.TrimSpace(s) != s || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return loaderr.Errorf(pathField, fmt.Sprintf("remove surrounding spaces and control characters from %s", pathField), "project: %s %q must have no surrounding spaces or control characters", label, s)
	}
	return nil
}

// validateID accepts a reverse-DNS identifier: two or more dot-separated segments of ASCII letters, digits, '_' and '-', each starting with a letter.
func validateID(id string) error {
	const fixID = "set id to a reverse-DNS identifier such as dev.example.game"
	segments := strings.Split(id, ".")
	if len(segments) < 2 {
		return loaderr.Errorf("id", fixID, "project: id %q must be a reverse-DNS identifier such as dev.example.game", id)
	}
	for _, seg := range segments {
		if seg == "" || !isASCIILetter(seg[0]) {
			return loaderr.Errorf("id", fixID, "project: id %q must be a reverse-DNS identifier such as dev.example.game", id)
		}
		for i := range len(seg) {
			c := seg[i]
			if !isASCIILetter(c) && (c < '0' || c > '9') && c != '_' && c != '-' {
				return loaderr.Errorf("id", fixID, "project: id %q has invalid character %q", id, c)
			}
		}
	}
	return nil
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func validateWindow(w *Window) error {
	if err := validateText("window.title", "window title", w.Title, false); err != nil {
		return err
	}
	if !positive(w.Width) || !positive(w.Height) {
		return loaderr.Errorf("window", "set window.width and window.height to positive numbers", "project: window size (%v, %v) must be finite and positive", w.Width, w.Height)
	}
	for _, lim := range []struct {
		field string
		v     float32
	}{
		{"window.minWidth", w.MinWidth},
		{"window.minHeight", w.MinHeight},
		{"window.maxWidth", w.MaxWidth},
		{"window.maxHeight", w.MaxHeight},
	} {
		if !isFinite32(lim.v) || lim.v < 0 {
			return loaderr.Errorf(lim.field, "set window size limits to finite numbers >= 0", "project: window limits must be finite and non-negative")
		}
	}
	if w.MaxWidth > 0 && w.MaxWidth < w.MinWidth || w.MaxHeight > 0 && w.MaxHeight < w.MinHeight {
		return loaderr.Errorf("window", "make window maxWidth/maxHeight at least minWidth/minHeight", "project: window maximum size must be at least its minimum")
	}
	if w.Width < w.MinWidth || w.Height < w.MinHeight || w.MaxWidth > 0 && w.Width > w.MaxWidth || w.MaxHeight > 0 && w.Height > w.MaxHeight {
		return loaderr.Errorf("window", "choose window width and height within min/max limits", "project: window size (%v, %v) must lie within its limits", w.Width, w.Height)
	}
	switch w.Appearance {
	case "", "light", "dark":
	default:
		return loaderr.Errorf("window.appearance", `set window.appearance to "light", "dark", or omit it`, "project: window appearance %q must be light, dark or empty", w.Appearance)
	}
	return nil
}

// Display modes name screen.Mode values.
var displayModes = []string{"fit", "fill", "stretch", "expand"}

func validateDisplay(d *Display) error {
	if !positive(d.Width) || !positive(d.Height) {
		return loaderr.Errorf("display", "set display.width and display.height to positive numbers", "project: display size (%v, %v) must be finite and positive", d.Width, d.Height)
	}
	if d.Mode != "" && !slices.Contains(displayModes, d.Mode) {
		return loaderr.Errorf("display.mode", "set display.mode to one of "+strings.Join(displayModes, ", "), "project: display mode %q must be one of %s", d.Mode, strings.Join(displayModes, ", "))
	}
	if d.BarColor != "" {
		if _, err := core.ParseColor(d.BarColor); err != nil {
			return loaderr.Errorf("display.barColor", "set display.barColor to a valid CSS color such as #000000", "project: display barColor %q: %w", d.BarColor, err)
		}
	}
	return nil
}

func validateInput(in *Input) error {
	actions := make(map[string]struct{}, len(in.Actions))
	for i, a := range in.Actions {
		prefix := fmt.Sprintf("input.actions[%d]", i)
		if err := validateNameAt(prefix+".name", fmt.Sprintf("input action %d", i), a.Name); err != nil {
			return err
		}
		if _, dup := actions[a.Name]; dup {
			return loaderr.Errorf(prefix+".name", "give each input action a unique name", "project: duplicate input action %q", a.Name)
		}
		actions[a.Name] = struct{}{}
		for j, b := range a.Bindings {
			if err := validateBindingAt(fmt.Sprintf("%s.bindings[%d]", prefix, j), a.Name, j, b); err != nil {
				return err
			}
		}
	}
	axes := make(map[string]struct{}, len(in.Axes))
	for i, ax := range in.Axes {
		prefix := fmt.Sprintf("input.axes[%d]", i)
		if err := validateNameAt(prefix+".name", fmt.Sprintf("input axis %d", i), ax.Name); err != nil {
			return err
		}
		if _, dup := axes[ax.Name]; dup {
			return loaderr.Errorf(prefix+".name", "give each input axis a unique name", "project: duplicate input axis %q", ax.Name)
		}
		axes[ax.Name] = struct{}{}
		if ax.Negative == "" && ax.Positive == "" {
			return loaderr.Errorf(prefix, "set negative and/or positive to a declared input action", "project: input axis %q needs a negative or a positive action", ax.Name)
		}
		for _, ref := range []struct {
			field string
			name  string
		}{{"negative", ax.Negative}, {"positive", ax.Positive}} {
			if _, ok := actions[ref.name]; ref.name != "" && !ok {
				return loaderr.Errorf(prefix+"."+ref.field, "reference a declared action name from input.actions", "project: input axis %q reads undeclared action %q", ax.Name, ref.name)
			}
		}
	}
	// A motion axis may share its name with an axis: input.Actions.Axis sums both, as input.Actions.BindMotionAxis documents.
	motion := make(map[string]struct{}, len(in.MotionAxes))
	for i, m := range in.MotionAxes {
		prefix := fmt.Sprintf("input.motionAxes[%d]", i)
		if err := validateNameAt(prefix+".name", fmt.Sprintf("input motion axis %d", i), m.Name); err != nil {
			return err
		}
		if _, dup := motion[m.Name]; dup {
			return loaderr.Errorf(prefix+".name", "give each motion axis a unique name", "project: duplicate input motion axis %q", m.Name)
		}
		motion[m.Name] = struct{}{}
		if !slices.Contains(MotionAxes, m.Axis) {
			return loaderr.Errorf(prefix+".axis", "set axis to one of "+strings.Join(MotionAxes, ", "), "project: input motion axis %q axis %q must be one of %s", m.Name, m.Axis, strings.Join(MotionAxes, ", "))
		}
		if !isFinite64(m.DeadZone) || m.DeadZone < 0 || m.DeadZone >= 1 {
			return loaderr.Errorf(prefix+".deadZone", "set deadZone to a number in [0, 1)", "project: input motion axis %q deadZone %v must be in [0, 1)", m.Name, m.DeadZone)
		}
		if !isFinite64(m.Smoothing) || m.Smoothing < 0 || m.Smoothing >= 1 {
			return loaderr.Errorf(prefix+".smoothing", "set smoothing to a number in [0, 1)", "project: input motion axis %q smoothing %v must be in [0, 1)", m.Name, m.Smoothing)
		}
		if !isFinite64(m.Sensitivity) || m.Sensitivity < 0 || !isFinite64(m.MaxRange) || m.MaxRange < 0 {
			return loaderr.Errorf(prefix, "set sensitivity and maxRange to finite numbers >= 0", "project: input motion axis %q sensitivity and maxRange must be finite and non-negative", m.Name)
		}
	}
	// A gamepad axis may share its name with an axis or motion axis: input.Actions.Axis sums all of them and clamps to [-1, 1].
	gpAxes := make(map[string]struct{}, len(in.GamepadAxes))
	for i, g := range in.GamepadAxes {
		prefix := fmt.Sprintf("input.gamepadAxes[%d]", i)
		if err := validateNameAt(prefix+".name", fmt.Sprintf("input gamepad axis %d", i), g.Name); err != nil {
			return err
		}
		if _, dup := gpAxes[g.Name]; dup {
			return loaderr.Errorf(prefix+".name", "give each gamepad axis a unique name", "project: duplicate input gamepad axis %q", g.Name)
		}
		gpAxes[g.Name] = struct{}{}
		if !slices.Contains(GamepadAxes, g.Axis) {
			return loaderr.Errorf(prefix+".axis", "set axis to one of "+strings.Join(GamepadAxes, ", "), "project: input gamepad axis %q axis %q must be one of %s", g.Name, g.Axis, strings.Join(GamepadAxes, ", "))
		}
		if !isFinite64(g.DeadZone) || g.DeadZone >= 1 {
			return loaderr.Errorf(prefix+".deadZone", "set deadZone to a finite number less than 1", "project: input gamepad axis %q deadZone %v must be finite and less than 1", g.Name, g.DeadZone)
		}
		if !isFinite64(g.Sensitivity) || g.Sensitivity < 0 {
			return loaderr.Errorf(prefix+".sensitivity", "set sensitivity to a finite number >= 0", "project: input gamepad axis %q sensitivity %v must be finite and non-negative", g.Name, g.Sensitivity)
		}
	}
	return nil
}

func validateBindingAt(pathField, action string, index int, b Binding) error {
	wrap := func(subField, fix string, err error) error {
		if err == nil {
			return nil
		}
		f := pathField
		if subField != "" {
			f = pathField + "." + subField
		}
		return loaderr.Errorf(f, fix, "project: input action %q binding %d: %w", action, index, err)
	}
	switch b.Kind {
	case KindTouchRegion:
		if b.Button != "" || b.Key != "" || b.Code != "" {
			return wrap("", "remove button, key, and code from touchRegion bindings", errors.New("touchRegion takes only region"))
		}
		return wrap("region", "set region with positive width and height", validateRegion(b.Region))
	case KindMouseButton:
		if b.Region != nil || b.Key != "" || b.Code != "" {
			return wrap("", "remove region, key, and code from mouseButton bindings", errors.New("mouseButton takes only button"))
		}
		return wrap("button", "set button to one of "+strings.Join(MouseButtons, ", "), validateButton(b.Button))
	case KindMouseRegion:
		if b.Key != "" || b.Code != "" {
			return wrap("", "remove key and code from mouseRegion bindings", errors.New("mouseRegion takes only button and region"))
		}
		if err := validateButton(b.Button); err != nil {
			return wrap("button", "set button to one of "+strings.Join(MouseButtons, ", "), err)
		}
		return wrap("region", "set region with positive width and height", validateRegion(b.Region))
	case KindKey:
		if b.Button != "" || b.Code != "" || b.Region != nil {
			return wrap("", "remove button, code, and region from key bindings", errors.New("key takes only key"))
		}
		return wrap("key", "set key to a non-empty key name without spaces", validateKeyToken("key", b.Key))
	case KindKeyCode:
		if b.Button != "" || b.Key != "" || b.Region != nil {
			return wrap("", "remove button, key, and region from keyCode bindings", errors.New("keyCode takes only code"))
		}
		return wrap("code", "set code to a non-empty key code without spaces", validateKeyToken("code", b.Code))
	case KindGamepadButton:
		if b.Key != "" || b.Code != "" || b.Region != nil {
			return wrap("", "remove key, code, and region from gamepadButton bindings", errors.New("gamepadButton takes only button"))
		}
		return wrap("button", "set button to one of "+strings.Join(GamepadButtons, ", "), validateGamepadButton(b.Button))
	default:
		return wrap("kind", "set kind to one of "+strings.Join(BindingKinds, ", "), fmt.Errorf("kind %q must be one of %s", b.Kind, strings.Join(BindingKinds, ", ")))
	}
}

func validateButton(button string) error {
	if !slices.Contains(MouseButtons, button) {
		return fmt.Errorf("button %q must be one of %s", button, strings.Join(MouseButtons, ", "))
	}
	return nil
}

func validateGamepadButton(button string) error {
	if !slices.Contains(GamepadButtons, button) {
		return fmt.Errorf("button %q must be one of %s", button, strings.Join(GamepadButtons, ", "))
	}
	return nil
}

func validateKeyToken(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%s %q must have no spaces or control characters", field, value)
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
		return loaderr.Errorf("audio.master", "set audio.master to a number in [0, 1]", "project: audio master %v must be in [0, 1]", *a.Master)
	}
	buses := make(map[string]struct{}, len(a.Buses))
	for i, b := range a.Buses {
		prefix := fmt.Sprintf("audio.buses[%d]", i)
		if err := validateNameAt(prefix+".name", fmt.Sprintf("audio bus %d", i), b.Name); err != nil {
			return err
		}
		if _, dup := buses[b.Name]; dup {
			return loaderr.Errorf(prefix+".name", "give each audio bus a unique name", "project: duplicate audio bus %q", b.Name)
		}
		buses[b.Name] = struct{}{}
		if b.Volume != nil && !unit32(*b.Volume) {
			return loaderr.Errorf(prefix+".volume", "set volume to a number in [0, 1]", "project: audio bus %q volume %v must be in [0, 1]", b.Name, *b.Volume)
		}
		for j, e := range b.Effects {
			if err := validateEffectAt(fmt.Sprintf("%s.effects[%d]", prefix, j), b.Name, j, e); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateEffectAt(pathField, bus string, index int, e Effect) error {
	wrap := func(subField, fix string, err error) error {
		if err == nil {
			return nil
		}
		f := pathField
		if subField != "" {
			f = pathField + "." + subField
		}
		return loaderr.Errorf(f, fix, "project: audio bus %q effect %d: %w", bus, index, err)
	}
	set := 0
	for _, isSet := range []bool{e.LowPass != nil, e.HighPass != nil, e.Delay != nil, e.Reverb != nil, e.Gain != nil, e.Bitcrush != nil} {
		if isSet {
			set++
		}
	}
	if set != 1 {
		return wrap("", "set exactly one of lowPass, highPass, delay, reverb, gain, or bitcrush", errors.New("must set exactly one of lowPass, highPass, delay, reverb, gain and bitcrush"))
	}
	switch {
	case e.LowPass != nil:
		return wrap("lowPass", "set cutoffHz > 0 and q >= 0", validateFilter(e.LowPass))
	case e.HighPass != nil:
		return wrap("highPass", "set cutoffHz > 0 and q >= 0", validateFilter(e.HighPass))
	case e.Delay != nil:
		d := e.Delay
		if d.Time <= 0 {
			return wrap("delay.time", "set delay.time to a positive duration such as 250ms", fmt.Errorf("delay time %v must be positive", d.Time))
		}
		if !isFinite64(d.Feedback) || d.Feedback < 0 || d.Feedback > 0.99 {
			return wrap("delay.feedback", "set delay.feedback to a number in [0, 0.99]", fmt.Errorf("delay feedback %v must be in [0, 0.99]", d.Feedback))
		}
		if !unit64(d.Wet) {
			return wrap("delay.wet", "set delay.wet to a number in [0, 1]", fmt.Errorf("delay wet %v must be in [0, 1]", d.Wet))
		}
	case e.Reverb != nil:
		r := e.Reverb
		if !unit64(r.RoomSize) || !unit64(r.Damping) || !unit64(r.Wet) {
			return wrap("reverb", "set reverb roomSize, damping, and wet to numbers in [0, 1]", errors.New("reverb roomSize, damping and wet must be in [0, 1]"))
		}
	case e.Gain != nil:
		if !isFinite64(e.Gain.Factor) || e.Gain.Factor < 0 {
			return wrap("gain.factor", "set gain.factor to a finite number >= 0", fmt.Errorf("gain factor %v must be finite and non-negative", e.Gain.Factor))
		}
	case e.Bitcrush != nil:
		b := e.Bitcrush
		if b.Bits < 1 || b.Bits > 15 {
			return wrap("bitcrush.bits", "set bitcrush.bits between 1 and 15", fmt.Errorf("bitcrush bits %d must be in [1, 15]", b.Bits))
		}
		if b.Downsample < 0 {
			return wrap("bitcrush.downsample", "set bitcrush.downsample to 0 or more", fmt.Errorf("bitcrush downsample %d must not be negative", b.Downsample))
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
	scenes, err := validateEntries("autoload.scenes", "scene", a.Scenes)
	if err != nil {
		return err
	}
	if _, err := validateEntries("autoload.resources", "resource", a.Resources); err != nil {
		return err
	}
	for i, s := range a.Scenes {
		if !strings.HasSuffix(s.Path, ".yaml") && !strings.HasSuffix(s.Path, ".yml") {
			return loaderr.Errorf(fmt.Sprintf("autoload.scenes[%d].path", i), "point each autoload scene to a .yaml or .yml file", "project: autoload scene %q path %q must be a .yaml or .yml file", s.Name, s.Path)
		}
	}
	if _, ok := scenes[a.MainScene]; a.MainScene != "" && !ok {
		return loaderr.Errorf("autoload.mainScene", "set autoload.mainScene to a scene name listed in autoload.scenes", "project: autoload mainScene %q is not an autoloaded scene", a.MainScene)
	}
	return nil
}

func validateEntries(listPath, kind string, entries []Entry) (map[string]struct{}, error) {
	names := make(map[string]struct{}, len(entries))
	for i, e := range entries {
		prefix := fmt.Sprintf("%s[%d]", listPath, i)
		if err := validateNameAt(prefix+".name", fmt.Sprintf("autoload %s %d", kind, i), e.Name); err != nil {
			return nil, err
		}
		if _, dup := names[e.Name]; dup {
			return nil, loaderr.Errorf(prefix+".name", fmt.Sprintf("give each autoload %s a unique name", kind), "project: duplicate autoload %s %q", kind, e.Name)
		}
		names[e.Name] = struct{}{}
		if e.Path == "." || !fs.ValidPath(e.Path) || strings.Contains(e.Path, `\`) {
			return nil, loaderr.Errorf(prefix+".path", "use a slash-separated relative path inside the project", "project: autoload %s %q path %q must be a slash-separated path inside the project", kind, e.Name, e.Path)
		}
	}
	return names, nil
}

func validateExport(e *Export) error {
	if e.Output != "" && (e.Output == "." || !fs.ValidPath(e.Output) || strings.Contains(e.Output, `\`)) {
		return loaderr.Errorf("export.output", "set export.output to a slash-separated directory inside the project", "project: export output %q must be a slash-separated directory inside the project", e.Output)
	}
	if e.Budgets != nil {
		if e.Budgets.QualityProfile != "" {
			if err := validateNameAt("export.budgets.qualityProfile", "export budgets qualityProfile", e.Budgets.QualityProfile); err != nil {
				return err
			}
		}
		if math.IsNaN(e.Budgets.DownloadSizeMB) || math.IsInf(e.Budgets.DownloadSizeMB, 0) || e.Budgets.DownloadSizeMB < 0 {
			return loaderr.Errorf("export.budgets.downloadSizeMB", "set downloadSizeMB to a finite number >= 0", "project: export budgets downloadSizeMB %v must be finite and >= 0", e.Budgets.DownloadSizeMB)
		}
		if math.IsNaN(e.Budgets.FrameTimeP95Ms) || math.IsInf(e.Budgets.FrameTimeP95Ms, 0) || e.Budgets.FrameTimeP95Ms < 0 {
			return loaderr.Errorf("export.budgets.frameTimeP95Ms", "set frameTimeP95Ms to a finite number >= 0", "project: export budgets frameTimeP95Ms %v must be finite and >= 0", e.Budgets.FrameTimeP95Ms)
		}
	}
	platforms := make(map[string]struct{}, len(e.Presets))
	for i, p := range e.Presets {
		prefix := fmt.Sprintf("export.presets[%d]", i)
		if !slices.Contains(Platforms, p.Platform) {
			return loaderr.Errorf(prefix+".platform", "set platform to one of "+strings.Join(Platforms, ", "), "project: export platform %q must be one of %s", p.Platform, strings.Join(Platforms, ", "))
		}
		if _, dup := platforms[p.Platform]; dup {
			return loaderr.Errorf(prefix+".platform", "keep only one export preset per platform", "project: duplicate export preset for platform %q", p.Platform)
		}
		platforms[p.Platform] = struct{}{}
		seen := make(map[string]struct{}, len(p.Targets))
		for j, t := range p.Targets {
			tPath := fmt.Sprintf("%s.targets[%d]", prefix, j)
			if !slices.Contains(nsgoTargets, t) || platformOf(t) != p.Platform {
				return loaderr.Errorf(tPath, "choose targets from "+strings.Join(TargetsOf(p.Platform), ", "), "project: export platform %q target %q must be one of %s", p.Platform, t, strings.Join(TargetsOf(p.Platform), ", "))
			}
			if _, dup := seen[t]; dup {
				return loaderr.Errorf(tPath, "remove duplicate target entries", "project: export platform %q lists target %q twice", p.Platform, t)
			}
			seen[t] = struct{}{}
		}
	}
	return nil
}

func validateEditor(e *Editor) error {
	seen := make(map[string]struct{}, len(e.Plugins))
	for i, name := range e.Plugins {
		field := fmt.Sprintf("editor.plugins[%d]", i)
		if err := validateNameAt(field, fmt.Sprintf("editor plugin %d", i), name); err != nil {
			return err
		}
		if _, dup := seen[name]; dup {
			return loaderr.Errorf(field, "remove duplicate editor plugin names", "project: editor plugin %q is listed twice", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func validatePacks(f *File) error {
	names := make(map[string]struct{}, len(f.Packs))
	// owner maps each file, autoload entry and plugin a pack claims to that pack, so no two packs claim one. A file's path is folded to lower case, as a case-insensitive file system such as macOS's or Windows's sees it.
	owner := map[string]string{}
	claim := func(field, pack, key, what string) error {
		if other, ok := owner[key]; ok {
			return loaderr.Errorf(field, "ensure each file, autoload entry, and plugin is claimed by at most one pack", "project: packs %q and %q both claim %s", other, pack, what)
		}
		owner[key] = pack
		return nil
	}
	for i := range f.Packs {
		p := &f.Packs[i]
		prefix := fmt.Sprintf("packs[%d]", i)
		if err := ValidatePackName(p.Name); err != nil {
			return loaderr.Errorf(prefix+".name", "use 1 to 64 lower-case ASCII letters, digits, or '-' starting with a letter or digit", "project: pack %d: %w", i, err)
		}
		if _, dup := names[p.Name]; dup {
			return loaderr.Errorf(prefix+".name", "remove the duplicate pack entry", "project: pack %q is listed twice", p.Name)
		}
		names[p.Name] = struct{}{}
		where := fmt.Sprintf("project: pack %q", p.Name)
		if !slices.Contains(PackKinds, p.Kind) {
			return loaderr.Errorf(prefix+".kind", "set kind to one of "+strings.Join(PackKinds, ", "), "%s: kind %q must be one of %s", where, p.Kind, strings.Join(PackKinds, ", "))
		}
		if err := ValidatePackVersion(p.PackVersion); err != nil {
			return loaderr.Errorf(prefix+".packVersion", "set packVersion to a semantic version such as 1.2.0", "%s: %w", where, err)
		}
		if !IsSHA256(p.SHA256) {
			return loaderr.Errorf(prefix+".sha256", "set sha256 to 64 lower-case hexadecimal digits", "%s: sha256 %q must be 64 lower-case hexadecimal digits", where, p.SHA256)
		}
		if !isProjectPath(p.Dir) {
			return loaderr.Errorf(prefix+".dir", "set dir to a slash-separated relative path inside the project", "%s: dir %q must be a slash-separated directory inside the project", where, p.Dir)
		}
		for j, file := range p.Files {
			fPath := fmt.Sprintf("%s.files[%d]", prefix, j)
			if !isProjectPath(file) {
				return loaderr.Errorf(fPath, "use a slash-separated relative path inside the pack dir", "%s: file %q must be a slash-separated path inside its dir", where, file)
			}
			if j > 0 && p.Files[j-1] >= file {
				return loaderr.Errorf(fPath, "sort files in ascending order without duplicates", "%s: files must be sorted and unique, but %q follows %q", where, file, p.Files[j-1])
			}
			what := "file " + path.Join(p.Dir, file)
			if err := claim(fPath, p.Name, strings.ToLower(what), what); err != nil {
				return err
			}
		}
		for _, ref := range []struct {
			field string
			kind  string
			names []string
			has   func(string) bool
		}{
			{"scenes", "autoload scene", p.Scenes, func(n string) bool { return f.Autoload != nil && hasEntry(f.Autoload.Scenes, n) }},
			{"resources", "autoload resource", p.Resources, func(n string) bool { return f.Autoload != nil && hasEntry(f.Autoload.Resources, n) }},
			{"plugins", "editor plugin", p.Plugins, func(n string) bool { return f.Editor != nil && slices.Contains(f.Editor.Plugins, n) }},
		} {
			for j, n := range ref.names {
				rPath := fmt.Sprintf("%s.%s[%d]", prefix, ref.field, j)
				if !ref.has(n) {
					return loaderr.Errorf(rPath, fmt.Sprintf("list only %s entries present in game.yaml", ref.kind), "%s: added %s %q, which the project does not have", where, ref.kind, n)
				}
				what := ref.kind + " " + strconv.Quote(n)
				if err := claim(rPath, p.Name, what, what); err != nil {
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

// ValidatePackVersion reports whether v is a valid semantic version (MAJOR.MINOR.PATCH[-prerelease][+build]) without a leading "v", such as 1.2.0.
func ValidatePackVersion(v string) error {
	if strings.HasPrefix(v, "v") {
		return fmt.Errorf("packVersion %q must be a semantic version without a leading \"v\", such as 1.2.0", v)
	}
	if _, err := semver.Parse(v); err != nil {
		return fmt.Errorf("packVersion %q must be a semantic version such as 1.2.0: %w", v, err)
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

// validateNameAt refuses empty names and names with spaces or control characters, which input actions, axes, buses, autoload entries and editor plugins are looked up by.
func validateNameAt(pathField, where, name string) error {
	if name == "" {
		return loaderr.Errorf(pathField, fmt.Sprintf("set %s to a non-empty name", pathField), "project: %s has an empty name", where)
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return loaderr.Errorf(pathField, fmt.Sprintf("remove spaces and control characters from %s", pathField), "project: %s name %q must have no spaces or control characters", where, name)
	}
	return nil
}

func positive(v float32) bool { return isFinite32(v) && v > 0 }

func unit32(v float32) bool { return isFinite32(v) && v >= 0 && v <= 1 }

func unit64(v float64) bool { return isFinite64(v) && v >= 0 && v <= 1 }

func isFinite32(v float32) bool { return isFinite64(float64(v)) }

func isFinite64(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

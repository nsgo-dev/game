package debug

import "time"

// FrameHistory is the number of frames retained in a FrameStats ring buffer.
const FrameHistory = 120

// Options configures an Overlay. Every field is optional and takes a sensible default when zero.
type Options struct {
	// ToggleKey is the key binding that shows and hides the overlay; "F3" when empty.
	ToggleKey string
	// Visible sets whether the overlay starts shown.
	Visible bool
	// TargetFPS is the reference frame rate drawn on the frame-time graph; 60 when 0 or less.
	TargetFPS float64
	// LineWidth is the base stroke width, in screen points, for collision and bounds wireframes; 1 when 0 or less.
	LineWidth float32
	// Scenes, when set, supplies the active scenes and layers shown by the scene inspector. If it reads NSGo signals, the inspector updates as those signals change; otherwise it refreshes on recorded frames and RefreshScenes.
	Scenes func() []SceneSnapshot
	// Now is the clock WrapLoop uses to measure Update and Draw durations; time.Now when nil.
	Now func() time.Time
	// Console, when set, renders an interactive dev console alongside the overlay.
	Console *Console
	// Profiler, when set, displays top named timing scopes inside the overlay.
	Profiler *Profiler
	// Memory, when set, displays asset memory usage and budget status inside the overlay.
	Memory *MemoryTracker
	// GC, when set, samples GC pauses each frame and highlights GC-hitched frames in the overlay.
	GC *GCMonitor
}

// FrameSample holds the measured durations of one rendered frame.
type FrameSample struct {
	// Total is the elapsed time of the frame.
	Total time.Duration
	// Update is the time spent in fixed-step updates during the frame.
	Update time.Duration
	// Draw is the time spent drawing the frame.
	Draw time.Duration
	// GCHitch reports whether a GC pause occurred during this frame.
	GCHitch bool
}

// SceneSnapshot describes one active scene for the scene inspector.
type SceneSnapshot struct {
	// Name identifies the scene, such as "title" or "world".
	Name string
	// Layers are the scene's layers in draw order.
	Layers []LayerSnapshot
}

// LayerSnapshot describes one layer within a SceneSnapshot.
type LayerSnapshot struct {
	// Name identifies the layer, such as "terrain", "actors" or "hud".
	Name string
	// Space is the coordinate space the layer draws in, such as "world" or "screen".
	Space string
	// Objects is the number of live objects on the layer.
	Objects int
}

// ConsoleEntry is one recorded command execution in a dev Console log.
type ConsoleEntry struct {
	// Input is the raw command line that was executed.
	Input string
	// Output is the returned text or error message.
	Output string
	// IsError reports whether the command returned an error.
	IsError bool
}

// TunableInfo describes one registered tunable variable on a Console.
type TunableInfo struct {
	// Name is the tunable's registered name.
	Name string
	// Type is "float", "int", "bool" or "string".
	Type string
	// Value is the tunable's current value (float64, int, bool or string).
	Value any
	// Default is the value captured when the tunable was registered.
	Default any
	// Min is the inclusive lower bound for numeric tunables ("float" or "int"), or nil for other types.
	Min any
	// Max is the inclusive upper bound for numeric tunables ("float" or "int"), or nil for other types.
	Max any
}

// CommandInfo describes one command available on a Console.
type CommandInfo struct {
	// Name is the command name as typed on the console.
	Name string
	// Help is the command's usage or description text.
	Help string
	// Builtin reports whether the command is built into Console.
	Builtin bool
}

// ScopeSummary summarizes a named timing scope's cost for overlay display.
type ScopeSummary struct {
	// Name is the scope identifier passed to Begin or Scope.
	Name string
	// Total is the total duration spent in this scope during the most recent frame.
	Total time.Duration
	// Avg is the rolling average per-frame duration across recent frames.
	Avg time.Duration
	// Max is the longest per-frame duration observed across recent frames.
	Max time.Duration
	// Calls is the number of times this scope ran during the most recent frame.
	Calls int
}

// AssetKind classifies tracked asset memory in a MemoryTracker.
type AssetKind uint8

const (
	// AssetImage identifies standalone decoded images and textures.
	AssetImage AssetKind = iota
	// AssetAtlas identifies sprite atlases and sheets.
	AssetAtlas
	// AssetAudio identifies decoded audio buffers and sound banks.
	AssetAudio
	// AssetOther identifies any other asset category.
	AssetOther
)

// String returns a lowercase label for k.
func (k AssetKind) String() string {
	switch k {
	case AssetImage:
		return "image"
	case AssetAtlas:
		return "atlas"
	case AssetAudio:
		return "audio"
	default:
		return "other"
	}
}

// GroupMemory reports the tracked memory for one asset group.
type GroupMemory struct {
	// Group is the asset group name, such as "level1" or "ui".
	Group string
	// TotalBytes is the total tracked bytes across all assets in the group.
	TotalBytes int64
	// Budget is the configured byte budget for the group, or 0 when unbounded.
	Budget int64
	// OverBudget reports whether Budget > 0 and TotalBytes > Budget.
	OverBudget bool
	// Count is the number of tracked assets currently in the group.
	Count int
	// ByKind maps each AssetKind present in the group to its tracked byte total.
	ByKind map[AssetKind]int64
}

// EstimateImageBytes returns the uncompressed RGBA8 memory footprint (w * h * 4) for an image of size w by h, or 0 if either dimension is non-positive.
func EstimateImageBytes(w, h int) int64 {
	if w <= 0 || h <= 0 {
		return 0
	}
	return int64(w) * int64(h) * 4
}

// EstimatePCMBytes returns the uncompressed PCM memory footprint (samples * channels * bytesPerSample), or 0 if any parameter is non-positive.
func EstimatePCMBytes(samples, channels, bytesPerSample int) int64 {
	if samples <= 0 || channels <= 0 || bytesPerSample <= 0 {
		return 0
	}
	return int64(samples) * int64(channels) * int64(bytesPerSample)
}

// GCFrameSample records GC metrics sampled for one frame by a GCMonitor.
type GCFrameSample struct {
	// FrameDuration is the frame duration passed to SampleFrame.
	FrameDuration time.Duration
	// CyclesDelta is the number of GC cycles completed since the previous sample.
	CyclesDelta uint64
	// Pause is the estimated total GC pause time observed during this frame.
	Pause time.Duration
	// HeapObjectsBytes is the live heap object bytes reported by /memory/classes/heap/objects:bytes.
	HeapObjectsBytes uint64
	// Hitched reports whether a GC pause occurred during this frame.
	Hitched bool
}

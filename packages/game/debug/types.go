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
}

// FrameSample holds the measured durations of one rendered frame.
type FrameSample struct {
	// Total is the elapsed time of the frame.
	Total time.Duration
	// Update is the time spent in fixed-step updates during the frame.
	Update time.Duration
	// Draw is the time spent drawing the frame.
	Draw time.Duration
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

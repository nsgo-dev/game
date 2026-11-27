package debug

import (
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/online/netplay"
	"github.com/nsgo-dev/nsgo/core"
)

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
	// DrawStats, when set, supplies live per-frame draw statistics displayed in the overlay and exposed to MCP performance readouts.
	DrawStats func() DrawStats
	// AI, when set, supplies the chosen agent's behavior tree or state machine snapshot shown in the AI inspector panel.
	// If it reads NSGo signals, the panel updates reactively; otherwise it refreshes on recorded frames and RefreshAI.
	AI func() AISnapshot
	// Net, when set, supplies live network traffic rates, InboxStats counters, and recent decoded message types shown in the network inspector panel.
	// Callers can build NetSnapshot without per-frame allocations using NetMonitor.
	Net func() NetSnapshot
}

// DrawStats captures per-frame draw submission counts for overlay display and MCP performance readouts.
type DrawStats struct {
	// DrawCalls is the number of draw calls submitted in the frame.
	DrawCalls int
	// Sprites is the number of sprites drawn in the frame.
	Sprites int
	// Batches is the number of sprite or instanced draw batches submitted in the frame.
	Batches int
	// Triangles is the number of triangles submitted in the frame.
	Triangles uint64
	// Vertices is the number of vertices submitted in the frame.
	Vertices uint64
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
	// Cause is an optional short explanation of a frame-time spike (such as shader compilation or asset streaming) when known by the game.
	Cause string
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

// TunableChange is the result of Console.SetTunable.
type TunableChange struct {
	// Old is the tunable's value before the change.
	Old any
	// Tunable describes the tunable after the change; its Value is the value stored.
	Tunable TunableInfo
	// Clamped reports whether the requested value lay outside the tunable's range and was stored as the nearest bound.
	Clamped bool
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
	// AllocsDelta is the number of heap objects allocated during this frame (/gc/heap/allocs:objects).
	AllocsDelta uint64
	// AllocBytesDelta is the number of heap bytes allocated during this frame (/gc/heap/allocs:bytes).
	AllocBytesDelta uint64
	// Hitched reports whether a GC pause occurred during this frame.
	Hitched bool
}

// AISnapshot describes a chosen agent's live AI state for the debug overlay panel.
// Set Tree to inspect a behavior tree, Machine to inspect a flow.Machine, or both.
type AISnapshot struct {
	// Agent identifies the inspected agent (for example, "guard-1" or "boss").
	Agent string
	// Tree holds the behavior tree's pre-order node traces (for example, from ai.BTInspector.Walk or WalkInto).
	Tree []ai.BTNodeTrace
	// Machine holds a flow.Machine's current state and bounded transition history (for example, from m.Inspect()).
	Machine *flow.MachineInspection
}

// SteeringColors customizes the colors used by DrawSteering. Zero fields take high-contrast defaults.
type SteeringColors struct {
	// Agent colors the agent's collision-radius wireframe (default: 0xE2E8F0).
	Agent core.Color
	// Velocity colors the current velocity vector arrow (default: 0x22C55E).
	Velocity core.Color
	// Desired colors the desired velocity vector arrow (default: 0x38BDF8).
	Desired core.Color
	// Target colors the target reticle and sightline (default: 0xF59E0B).
	Target core.Color
	// Path colors the waypoint polyline and waypoint markers (default: 0xA78BFA).
	Path core.Color
}

// SteeringDebug holds the debug visualization inputs for one 2D steering agent.
type SteeringDebug struct {
	// Agent is the steering agent whose position, radius, and current velocity are drawn.
	Agent ai.Agent
	// DesiredVelocity is the agent's desired velocity vector (drawn when non-zero).
	DesiredVelocity core.Point
	// Target is the agent's current target position (drawn when HasTarget is true).
	Target core.Point
	// HasTarget reports whether Target should be drawn, allowing (0, 0) as a valid target.
	HasTarget bool
	// Path is an optional sequence of world-space waypoints being followed.
	Path []core.Point
	// WaypointIndex highlights Path[WaypointIndex] as the active waypoint when in [0, len(Path)); set negative to omit active highlighting.
	WaypointIndex int
	// VelocityScale scales Velocity and DesiredVelocity into world drawing units (1 when 0 or less).
	VelocityScale float32
	// Colors overrides default colors for any non-zero color field.
	Colors SteeringColors
}

// NetChannelStats reports live throughput rates for one netplay.Channel.
type NetChannelStats struct {
	// Channel is the transport delivery channel (ChannelReliableOrdered or ChannelUnreliable).
	Channel netplay.Channel
	// BytesPerSec is the combined send and receive byte rate on this channel.
	BytesPerSec float64
	// PacketsPerSec is the combined send and receive packet rate on this channel.
	PacketsPerSec float64
	// SendBytesPerSec is the outbound byte rate on this channel.
	SendBytesPerSec float64
	// RecvBytesPerSec is the inbound byte rate on this channel.
	RecvBytesPerSec float64
	// SendPacketsPerSec is the outbound packet rate on this channel.
	SendPacketsPerSec float64
	// RecvPacketsPerSec is the inbound packet rate on this channel.
	RecvPacketsPerSec float64
}

// NetPeerStats reports live throughput rates for one remote peer.
type NetPeerStats struct {
	// Peer identifies the remote peer (or netplay.PeerBroadcast for broadcast sends).
	Peer netplay.PeerID
	// BytesPerSec is the combined send and receive byte rate with this peer.
	BytesPerSec float64
	// PacketsPerSec is the combined send and receive packet rate with this peer.
	PacketsPerSec float64
	// SendBytesPerSec is the outbound byte rate to this peer.
	SendBytesPerSec float64
	// RecvBytesPerSec is the inbound byte rate from this peer.
	RecvBytesPerSec float64
	// SendPacketsPerSec is the outbound packet rate to this peer.
	SendPacketsPerSec float64
	// RecvPacketsPerSec is the inbound packet rate from this peer.
	RecvPacketsPerSec float64
}

// NetMessageEvent records one recently sent or received packet's decoded wire message type.
type NetMessageEvent struct {
	// Dir is netplay.CaptureSend ("send") or netplay.CaptureRecv ("recv").
	Dir netplay.CaptureDirection
	// Peer is the remote peer ID.
	Peer netplay.PeerID
	// Channel is the transport delivery channel.
	Channel netplay.Channel
	// Type is the decoded wire message type (such as "replication.delta", "rollback.single_input", "relay.members", or "raw").
	Type string
	// Bytes is the payload size in bytes.
	Bytes int
}

// NetSnapshot describes live network traffic rates, inbox counters, and recent message types for the debug overlay panel.
type NetSnapshot struct {
	// Inbox holds the transport's InboxStats counters.
	Inbox netplay.InboxStats
	// Channels holds per-channel throughput rates.
	Channels []NetChannelStats
	// Peers holds per-peer throughput rates.
	Peers []NetPeerStats
	// Recent holds the most recent N decoded message events, oldest to newest.
	Recent []NetMessageEvent
}

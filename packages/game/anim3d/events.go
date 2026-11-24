package anim3d

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// ClipEvent marks a named gameplay or audio event at a specific time (in seconds)
// within an animation Clip.
type ClipEvent struct {
	Time    float32        `json:"time"`
	Name    string         `json:"name"`
	Payload string         `json:"payload,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// FiredEvent records a ClipEvent emitted during animation playback along with
// the state name, clip name, and active blend weight at the time it fired.
type FiredEvent struct {
	Event  ClipEvent
	State  string
	Clip   string
	Weight float32
}

// SortClipEvents stably sorts events in place by ascending Time.
func SortClipEvents(events []ClipEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Time < events[j].Time
	})
}

// CollectClipEvents returns all events from events that trigger while advancing
// clip playback from prevTime to nextTime (both in seconds) for a clip of length
// duration (in seconds).
//
// Events fire strictly once per playback across normal steps, multi-frame skips
// (including spans exceeding duration), and loop wrap-arounds. Both wrapped
// playback timestamps (nextTime < prevTime when loop is true) and unwrapped
// monotonic timestamps (nextTime >= duration) are handled cleanly.
//
// Boundary semantics prevent double-firing at loop seams:
//   - Events at Time == 0 fire on [prev, next) (i.e. at loop start when playback
//     advances past 0 or crosses a loop boundary into the new loop).
//   - Events at 0 < Time <= duration fire on (prev, next] (i.e. on the step
//     that reaches or crosses Time, including Time == duration at loop end).
func CollectClipEvents(events []ClipEvent, prevTime, nextTime, duration float32, loop bool) []ClipEvent {
	if len(events) == 0 || duration <= 0 {
		return nil
	}

	dur := float64(duration)
	p0 := float64(prevTime)
	p1 := float64(nextTime)

	// Build a stably sorted slice of in-range events without mutating caller input.
	sorted := make([]ClipEvent, 0, len(events))
	for _, ev := range events {
		if ev.Time < 0 || float64(ev.Time) > dur {
			continue
		}
		sorted = append(sorted, ev)
	}
	if len(sorted) == 0 {
		return nil
	}
	SortClipEvents(sorted)

	if !loop {
		if p0 < 0 {
			p0 = 0
		} else if p0 > dur {
			p0 = dur
		}
		if p1 < 0 {
			p1 = 0
		} else if p1 > dur {
			p1 = dur
		}
		if p1 <= p0 {
			return nil
		}

		var fired []ClipEvent
		for _, ev := range sorted {
			et := float64(ev.Time)
			if et == 0 {
				if p0 == 0 && p1 > 0 {
					fired = append(fired, ev)
				}
			} else if et > p0 && et <= p1 {
				fired = append(fired, ev)
			}
		}
		return fired
	}

	// Looping playback.
	if p1 < p0 {
		// Caller passed wrapped timestamps across a loop boundary (e.g. 0.9 -> 0.05).
		p0 = modFloat64(p0, dur)
		p1 = modFloat64(p1, dur)
		if p1 <= p0 {
			p1 += dur
		}
	} else {
		if p1 == p0 {
			return nil
		}
		// Shift interval so p0 lies in [0, dur) while preserving exact span.
		base := math.Floor(p0/dur) * dur
		p0 -= base
		p1 -= base
	}

	if p1 <= p0 {
		return nil
	}

	maxLoop := int(math.Floor(p1 / dur))
	var fired []ClipEvent
	for k := 0; k <= maxLoop; k++ {
		loopStart := float64(k) * dur
		for _, ev := range sorted {
			et := float64(ev.Time)
			occ := loopStart + et
			if et == 0 {
				if occ >= p0 && occ < p1 {
					fired = append(fired, ev)
				}
			} else if occ > p0 && occ <= p1 {
				fired = append(fired, ev)
			}
		}
	}
	return fired
}

// collectDirected collects events between two unwrapped playback times in either direction. A forward step (next > prev) is CollectClipEvents; a backward step mirrors time (t -> duration - t) so reverse playback fires each event it crosses exactly once, in the order it crosses them, with the same seam rules as forward playback (#546).
func collectDirected(events []ClipEvent, prev, next, duration float64, loop bool) []ClipEvent {
	if next >= prev {
		return CollectClipEvents(events, float32(prev), float32(next), float32(duration), loop)
	}
	if len(events) == 0 || duration <= 0 {
		return nil
	}
	mirrored := make([]ClipEvent, 0, len(events))
	for _, ev := range events {
		if ev.Time < 0 || float64(ev.Time) > duration {
			continue
		}
		m := ev
		m.Time = float32(duration - float64(ev.Time))
		mirrored = append(mirrored, m)
	}
	fired := CollectClipEvents(mirrored, float32(duration-prev), float32(duration-next), float32(duration), loop)
	for i := range fired {
		fired[i].Time = float32(duration - float64(fired[i].Time))
	}
	return fired
}

func modFloat64(x, period float64) float64 {
	r := math.Mod(x, period)
	if r < 0 {
		r += period
	}
	if r >= period {
		r = 0
	}
	return r
}

func weightPassesThreshold(weight, minWeight float32) bool {
	if minWeight <= 0 {
		return weight > 0
	}
	return weight >= minWeight
}

// EventDispatcher collects FiredEvents and dispatches them to registered
// callbacks and an internal queue drained via Drain.
type EventDispatcher struct {
	listeners []func(FiredEvent)
	pending   []FiredEvent
}

// NewEventDispatcher creates an empty EventDispatcher.
func NewEventDispatcher() *EventDispatcher {
	return &EventDispatcher{}
}

// OnEvent registers a listener invoked synchronously whenever an event is emitted.
func (d *EventDispatcher) OnEvent(fn func(FiredEvent)) {
	if d == nil || fn == nil {
		return
	}
	d.listeners = append(d.listeners, fn)
}

// Emit appends ev to the pending queue and notifies all registered listeners.
func (d *EventDispatcher) Emit(ev FiredEvent) {
	if d == nil {
		return
	}
	d.pending = append(d.pending, ev)
	for _, fn := range d.listeners {
		fn(ev)
	}
}

// DispatchClip collects events on clip in the interval [prevTime, nextTime]
// (in seconds), wraps each as a FiredEvent with stateName and weight, emits
// them through d, and returns the fired slice.
func (d *EventDispatcher) DispatchClip(clip *Clip, stateName string, prevTime, nextTime, weight float32) []FiredEvent {
	if clip == nil || clip.Duration <= 0 || len(clip.Events) == 0 {
		return nil
	}
	events := CollectClipEvents(clip.Events, prevTime, nextTime, float32(clip.Duration.Seconds()), clip.Loop)
	if len(events) == 0 {
		return nil
	}
	out := make([]FiredEvent, len(events))
	for i, ev := range events {
		fe := FiredEvent{
			Event:  ev,
			State:  stateName,
			Clip:   clip.Name,
			Weight: weight,
		}
		out[i] = fe
		if d != nil {
			d.Emit(fe)
		}
	}
	return out
}

// Drain returns all queued FiredEvents since the last Drain and clears the queue.
func (d *EventDispatcher) Drain() []FiredEvent {
	if d == nil || len(d.pending) == 0 {
		return nil
	}
	out := make([]FiredEvent, len(d.pending))
	copy(out, d.pending)
	d.pending = d.pending[:0]
	return out
}

// Pending returns a copy of queued FiredEvents without clearing the queue.
func (d *EventDispatcher) Pending() []FiredEvent {
	if d == nil || len(d.pending) == 0 {
		return nil
	}
	out := make([]FiredEvent, len(d.pending))
	copy(out, d.pending)
	return out
}

// Clear discards all queued FiredEvents.
func (d *EventDispatcher) Clear() {
	if d == nil {
		return
	}
	d.pending = d.pending[:0]
}

// gltfDocument is the minimal subset of a glTF 2.0 JSON document needed to
// extract animation event metadata from animations[i].extras.
type gltfDocument struct {
	Animations []struct {
		Name   string          `json:"name"`
		Extras json.RawMessage `json:"extras"`
	} `json:"animations"`
}

type gltfAnimationExtras struct {
	Events     []ClipEvent `json:"events"`
	NsgoEvents []ClipEvent `json:"nsgo_events"`
}

// LoadEventsFromGLTFExtras parses animation events from a glTF 2.0 JSON document's
// animations[i].extras.events and/or animations[i].extras.nsgo_events arrays.
// Each clip's events are returned sorted by ascending Time.
// Animations without a name fall back to "animation_<index>".
//
// Its errors are *loaderr.Error values that give the line of the problem and how to fix it; a caller that read the document from a file names it with loaderr.WithFile.
func LoadEventsFromGLTFExtras(gltfJSON []byte) (map[string][]ClipEvent, error) {
	var doc gltfDocument
	if err := json.Unmarshal(gltfJSON, &doc); err != nil {
		return nil, loaderr.Locate(err, "", gltfJSON, "export the glTF again, or correct its JSON so it parses")
	}

	result := make(map[string][]ClipEvent)
	for i, anim := range doc.Animations {
		if len(anim.Extras) == 0 {
			continue
		}
		// In glTF 2.0, extras may be any JSON type; ignore non-object extras.
		var extras gltfAnimationExtras
		if err := json.Unmarshal(anim.Extras, &extras); err != nil {
			continue
		}
		var combined []ClipEvent
		if len(extras.Events) > 0 {
			combined = append(combined, extras.Events...)
		}
		if len(extras.NsgoEvents) > 0 {
			combined = append(combined, extras.NsgoEvents...)
		}
		if len(combined) == 0 {
			continue
		}
		SortClipEvents(combined)
		name := anim.Name
		if name == "" {
			name = fmt.Sprintf("animation_%d", i)
		}
		result[name] = combined
	}
	return result, nil
}

type eventsSidecarFile struct {
	Version int                    `json:"version"`
	Clips   map[string][]ClipEvent `json:"clips"`
}

// MarshalEventsSidecar serializes per-clip event lists into a formatted JSON
// sidecar document ({"version": 1, "clips": {...}}) with each clip's events
// sorted by ascending Time.
func MarshalEventsSidecar(clips map[string][]ClipEvent) ([]byte, error) {
	normalized := make(map[string][]ClipEvent, len(clips))
	for name, events := range clips {
		cp := make([]ClipEvent, len(events))
		copy(cp, events)
		SortClipEvents(cp)
		normalized[name] = cp
	}
	doc := eventsSidecarFile{
		Version: 1,
		Clips:   normalized,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("anim3d: marshal events sidecar: %w", err)
	}
	return data, nil
}

// LoadEventsSidecar parses a JSON sidecar file produced by MarshalEventsSidecar
// ({"version": 1, "clips": {...}}) or a flat {"clipName": [...]} map into
// per-clip event slices sorted by ascending Time.
//
// Its errors are *loaderr.Error values that give the line and field of the problem and how to fix it; a caller that read data from a file names it with loaderr.WithFile.
func LoadEventsSidecar(data []byte) (map[string][]ClipEvent, error) {
	const fix = `write {"version": 1, "clips": {"<clip>": [{"time": <seconds>, "name": "<event>"}]}}`
	// Probe top-level object keys to distinguish envelope format from flat map.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, loaderr.Locate(err, "", data, fix)
	}

	var clips map[string][]ClipEvent
	if _, hasClips := raw["clips"]; hasClips {
		var doc eventsSidecarFile
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, loaderr.Locate(err, "", data, fix)
		}
		clips = doc.Clips
	} else {
		if err := json.Unmarshal(data, &clips); err != nil {
			return nil, loaderr.Locate(err, "", data, fix)
		}
	}

	result := make(map[string][]ClipEvent, len(clips))
	for name, events := range clips {
		cp := make([]ClipEvent, len(events))
		copy(cp, events)
		SortClipEvents(cp)
		result[name] = cp
	}
	return result, nil
}

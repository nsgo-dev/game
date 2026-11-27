package anim3d

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// EventsSidecarVersion is the schema version of the JSON clip events sidecar
// format written by MarshalEventsSidecar and read by LoadEventsSidecar.
const EventsSidecarVersion = 1

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

const (
	sidecarFix    = `write {"version": 1, "clips": {"<clip>": [{"time": <seconds>, "name": "<event>"}]}}`
	gltfEventsFix = `write "events" or "nsgo_events" as an array of {"time": <seconds>, "name": "<event>"} objects`
)

// LoadEventsFromGLTFExtras parses animation events from a glTF 2.0 JSON document's
// animations[i].extras.events and/or animations[i].extras.nsgo_events arrays.
// Each clip's events are returned sorted by ascending Time.
// Animations without a name fall back to "animation_<index>".
//
// Per glTF 2.0, absent, null, or non-object extras and unrelated keys inside
// an object extras are ignored. When "events" or "nsgo_events" is present and
// malformed (wrong type, unknown event field, or duplicate event key), an
// error is returned instead of dropping the clip's events.
//
// Its errors are *loaderr.Error values that give the line and field (including
// the animation index) of the problem and how to fix it; a caller that read
// the document from a file names it with loaderr.WithFile.
func LoadEventsFromGLTFExtras(gltfJSON []byte) (map[string][]ClipEvent, error) {
	var doc gltfDocument
	if err := json.Unmarshal(gltfJSON, &doc); err != nil {
		return nil, loaderr.LocateJSON(err, "", gltfJSON, "export the glTF again, or correct its JSON so it parses")
	}

	result := make(map[string][]ClipEvent)
	for i, anim := range doc.Animations {
		combined, err := parseGLTFAnimationExtras(i, anim.Extras, gltfJSON)
		if err != nil {
			return nil, err
		}
		if len(combined) == 0 {
			continue
		}
		name := anim.Name
		if name == "" {
			name = fmt.Sprintf("animation_%d", i)
		}
		result[name] = combined
	}
	return result, nil
}

// parseGLTFAnimationExtras extracts and validates timed clip events from one
// animation's glTF extras payload. Non-object extras and unrelated object keys
// are ignored; malformed "events" or "nsgo_events" entries return a *loaderr.Error
// naming animations[animIndex].extras.<key>.... When docData is non-nil (the full
// glTF JSON document), Line and Column are located in docData.
func parseGLTFAnimationExtras(animIndex int, animExtras json.RawMessage, docData []byte) ([]ClipEvent, error) {
	trimmed := bytes.TrimSpace(animExtras)
	if len(trimmed) == 0 {
		return nil, nil
	}
	baseField := loaderr.Path("animations", animIndex, "extras")
	var probe any
	if err := json.Unmarshal(animExtras, &probe); err != nil {
		return nil, relocateJSONError(err, animExtras, baseField, docData, gltfEventsFix)
	}
	if _, ok := probe.(map[string]any); !ok {
		// In glTF 2.0, extras may be any JSON type (null, string, number, bool, array); ignore non-object extras.
		return nil, nil
	}

	var rawExtras map[string]json.RawMessage
	if err := json.Unmarshal(animExtras, &rawExtras); err != nil {
		return nil, relocateJSONError(err, animExtras, baseField, docData, gltfEventsFix)
	}

	var combined []ClipEvent
	for _, key := range []string{"events", "nsgo_events"} {
		raw, ok := rawExtras[key]
		if !ok {
			continue
		}
		evs, err := decodeClipEventSlice(raw, loaderr.Join(baseField, key), docData, gltfEventsFix)
		if err != nil {
			return nil, err
		}
		combined = append(combined, evs...)
	}
	if len(combined) > 0 {
		SortClipEvents(combined)
	}
	return combined, nil
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
		Version: EventsSidecarVersion,
		Clips:   normalized,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("anim3d: marshal events sidecar: %w", err)
	}
	return data, nil
}

// LoadEventsSidecar parses a versioned JSON sidecar file produced by
// MarshalEventsSidecar ({"version": 1, "clips": {...}}) into per-clip event
// slices sorted by ascending Time.
//
// It requires version == EventsSidecarVersion (1) and refuses unknown fields,
// duplicate keys, trailing data, and the legacy unversioned flat-map shape.
//
// Its errors are *loaderr.Error values that give the line and field of the
// problem and how to fix it; a caller that read data from a file names it with
// loaderr.WithFile.
func LoadEventsSidecar(data []byte) (map[string][]ClipEvent, error) {
	// Validate JSON syntax and reject trailing bytes after the root value.
	var syntaxCheck json.RawMessage
	if err := json.Unmarshal(data, &syntaxCheck); err != nil {
		return nil, loaderr.LocateJSON(err, "", data, sidecarFix)
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		line, col := jsonTokenPosition(data, 0)
		return nil, &loaderr.Error{
			Line:     line,
			Column:   col,
			Problem:  "cannot use a JSON null here",
			Expected: "an object",
			Fix:      sidecarFix,
		}
	}
	if err := refuseDuplicateJSONKeys(data, "", data, true); err != nil {
		return nil, loaderr.LocateJSON(err, "", data, loaderr.FixDuplicateKey)
	}

	var doc struct {
		Version *int            `json:"version"`
		Clips   json.RawMessage `json:"clips"`
	}
	if err := decodeStrictJSONObject(data, &doc, "", data, sidecarFix); err != nil {
		return nil, err
	}
	if doc.Version == nil {
		return nil, loaderr.LocateJSON(&loaderr.Error{
			Field:    "version",
			Problem:  "missing sidecar version",
			Expected: fmt.Sprintf("%d", EventsSidecarVersion),
			Fix:      fmt.Sprintf(`add "version": %d at the top level of the sidecar`, EventsSidecarVersion),
		}, "", data, sidecarFix)
	}
	if *doc.Version != EventsSidecarVersion {
		return nil, loaderr.LocateJSON(&loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("unsupported sidecar version %d", *doc.Version),
			Expected: fmt.Sprintf("%d", EventsSidecarVersion),
			Fix:      fmt.Sprintf(`set "version": %d (the only supported version is %d)`, EventsSidecarVersion, EventsSidecarVersion),
		}, "", data, sidecarFix)
	}

	result := make(map[string][]ClipEvent)
	if len(doc.Clips) == 0 {
		return result, nil
	}
	if bytes.Equal(bytes.TrimSpace(doc.Clips), []byte("null")) {
		return nil, loaderr.LocateJSON(&loaderr.Error{
			Field:    "clips",
			Problem:  "cannot use a JSON null here",
			Expected: "an object",
			Fix:      sidecarFix,
		}, "", data, sidecarFix)
	}
	var rawClips map[string]json.RawMessage
	if err := json.Unmarshal(doc.Clips, &rawClips); err != nil {
		return nil, relocateJSONError(err, doc.Clips, "clips", data, sidecarFix)
	}

	clipNames := make([]string, 0, len(rawClips))
	for name := range rawClips {
		clipNames = append(clipNames, name)
	}
	sort.Strings(clipNames)

	for _, name := range clipNames {
		events, err := decodeClipEventSlice(rawClips[name], loaderr.Path("clips", name), data, sidecarFix)
		if err != nil {
			return nil, err
		}
		SortClipEvents(events)
		result[name] = events
	}
	return result, nil
}

func decodeClipEventSlice(raw json.RawMessage, baseField string, docData []byte, fix string) ([]ClipEvent, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		e := &loaderr.Error{
			Field:    baseField,
			Problem:  "cannot use a JSON null here",
			Expected: "an array",
			Fix:      fix,
		}
		if docData != nil {
			return nil, loaderr.LocateJSON(e, "", docData, fix)
		}
		return nil, e
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil, relocateJSONError(err, raw, baseField, docData, fix)
	}
	events := make([]ClipEvent, len(elems))
	for i, elem := range elems {
		elemField := loaderr.Join(baseField, i)
		if bytes.Equal(bytes.TrimSpace(elem), []byte("null")) {
			e := &loaderr.Error{
				Field:    elemField,
				Problem:  "cannot use a JSON null here",
				Expected: "an object",
				Fix:      fix,
			}
			if docData != nil {
				return nil, loaderr.LocateJSON(e, "", docData, fix)
			}
			return nil, e
		}
		if err := refuseDuplicateJSONKeys(elem, elemField, docData, true); err != nil {
			if docData != nil {
				return nil, loaderr.LocateJSON(err, "", docData, loaderr.FixDuplicateKey)
			}
			return nil, err
		}
		if err := decodeStrictJSONObject(elem, &events[i], elemField, docData, fix); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func decodeStrictJSONObject(sub []byte, dst any, baseField string, docData []byte, fix string) error {
	dec := json.NewDecoder(bytes.NewReader(sub))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return relocateJSONError(err, sub, baseField, docData, fix)
	}
	return nil
}

func relocateJSONError(err error, sub []byte, baseField string, docData []byte, fix string) error {
	loc := loaderr.LocateJSON(err, "", sub, fix)
	e, ok := loaderr.As(loc)
	if !ok {
		return loc
	}
	subField := e.Field
	if subField == "" {
		if name, ok := unknownFieldName(e.Problem); ok {
			subField = loaderr.Path(name)
		}
	}
	origLine, origCol := e.Line, e.Column
	e.Field = joinFormattedPath(baseField, subField)
	if docData != nil {
		e.Line, e.Column = 0, 0
		res := loaderr.LocateJSON(e, "", docData, fix)
		if le, ok := loaderr.As(res); ok && le.Line == 0 && sameSlice(sub, docData) {
			le.Line, le.Column = origLine, origCol
		}
		return res
	}
	e.Line, e.Column = 0, 0
	return e
}

func sameSlice(a, b []byte) bool {
	return len(a) > 0 && len(a) == len(b) && &a[0] == &b[0]
}

func unknownFieldName(problem string) (string, bool) {
	rest, ok := strings.CutPrefix(problem, "unknown field ")
	if !ok {
		return "", false
	}
	name, err := strconv.Unquote(rest)
	if err != nil {
		return "", false
	}
	return name, true
}

func joinFormattedPath(base, sub string) string {
	switch {
	case base == "":
		return sub
	case sub == "":
		return base
	case sub[0] == '[':
		return base + sub
	default:
		return base + "." + sub
	}
}

// refuseDuplicateJSONKeys rejects any JSON object in sub that repeats a key.
// When foldRoot is true, the root object (which decodes into a Go struct whose
// fields encoding/json matches case-insensitively) compares keys with
// strings.EqualFold; nested objects (maps such as clips or ClipEvent.Data)
// compare keys exactly so distinct clip or data names differing only in case
// remain allowed.
func refuseDuplicateJSONKeys(sub []byte, baseField string, docData []byte, foldRoot bool) error {
	dec := json.NewDecoder(bytes.NewReader(sub))
	sameDoc := sameSlice(sub, docData)
	var path []any
	var walk func(depth int) error
	walk = func(depth int) error {
		tok, err := dec.Token()
		if err != nil {
			return nil
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			fold := foldRoot && depth == 0
			var seenExact map[string]bool
			var seenFold []string
			if !fold {
				seenExact = make(map[string]bool)
			}
			for dec.More() {
				start := dec.InputOffset()
				keyTok, err := dec.Token()
				if err != nil {
					return nil
				}
				key, _ := keyTok.(string)
				dup := false
				if fold {
					for _, prev := range seenFold {
						if strings.EqualFold(prev, key) {
							dup = true
							break
						}
					}
					if !dup {
						seenFold = append(seenFold, key)
					}
				} else {
					dup = seenExact[key]
					seenExact[key] = true
				}
				path = append(path, key)
				if dup {
					e := &loaderr.Error{
						Field:   loaderr.Join(baseField, path...),
						Problem: fmt.Sprintf("key %q appears more than once in one object", key),
						Fix:     loaderr.FixDuplicateKey,
					}
					if sameDoc {
						e.Line, e.Column = jsonTokenPosition(sub, start)
					}
					return e
				}
				if err := walk(depth + 1); err != nil {
					return err
				}
				path = path[:len(path)-1]
			}
			_, _ = dec.Token()
		case '[':
			for i := 0; dec.More(); i++ {
				path = append(path, i)
				if err := walk(depth + 1); err != nil {
					return err
				}
				path = path[:len(path)-1]
			}
			_, _ = dec.Token()
		}
		return nil
	}
	return walk(0)
}

func jsonTokenPosition(data []byte, start int64) (line, col int) {
	off := int(start)
	for off < len(data) {
		b := data[off]
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == ',' || b == ':' {
			off++
			continue
		}
		break
	}
	line, col = 1, 1
	for i := 0; i < off && i < len(data); {
		if data[i] == '\n' {
			line++
			col = 1
			i++
			continue
		}
		_, size := utf8.DecodeRune(data[i:])
		i += size
		col++
	}
	return line, col
}

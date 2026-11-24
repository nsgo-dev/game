package assets

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Group lifecycle state names reported by GroupStats.State.
const (
	StateUnloaded = "unloaded"
	StateLoading  = "loading"
	StateLoaded   = "loaded"
	StateFailed   = "failed"
)

// AssetStats describes one registered asset inside a Group and its estimated in-memory size.
type AssetStats struct {
	Name     string
	Kind     string // "atlas", "sound", "image", "bytes", "custom"
	URI      string
	ImageURI string // atlas image URI when Kind == "atlas"
	Loaded   bool
	Bytes    int64
	Frames   int // atlas frame count when loaded and Kind == "atlas"
}

// GroupStats describes a Group's current load state, assets and estimated memory footprint.
type GroupStats struct {
	Name   string
	State  string // StateUnloaded, StateLoading, StateLoaded, or StateFailed
	Loaded int
	Total  int
	Bytes  int64
	Error  string
	Assets []AssetStats
}

// Disposed reports whether the manager's owning scope has been disposed.
func (m *Manager) Disposed() bool {
	if m == nil {
		return true
	}
	return m.isDisposed()
}

// GroupNames returns the names of all registered groups in registration order.
func (m *Manager) GroupNames() []string {
	if m == nil || len(m.groupOrder) == 0 {
		return nil
	}
	return slices.Clone(m.groupOrder)
}

// LookupGroup returns the existing group named name without creating one when absent.
func (m *Manager) LookupGroup(name string) (*Group, bool) {
	if m == nil {
		return nil, false
	}
	g, ok := m.groups[name]
	return g, ok
}

// GroupStats returns a read-only stats snapshot for every registered group in registration order. Reading GroupStats does not subscribe the caller to reactive signals.
func (m *Manager) GroupStats() []GroupStats {
	if m == nil || len(m.groupOrder) == 0 {
		return nil
	}
	out := make([]GroupStats, 0, len(m.groupOrder))
	for _, name := range m.groupOrder {
		if g, ok := m.groups[name]; ok {
			out = append(out, g.Stats())
		}
	}
	return out
}

// Stats returns a read-only snapshot of g's load state, registered assets, and estimated memory usage in bytes. Calling Stats does not subscribe the caller to reactive signals.
func (g *Group) Stats() GroupStats {
	if g == nil {
		return GroupStats{State: StateUnloaded}
	}
	st := GroupStats{
		Name:   g.name,
		State:  groupStateName(g.state),
		Loaded: g.loaded,
		Total:  len(g.specs),
		Assets: make([]AssetStats, 0, len(g.specs)),
	}
	if g.state == stateFailed && g.errSig != nil {
		if err := g.errSig.GetUntracked(); err != nil {
			st.Error = err.Error()
		}
	}
	for _, spec := range g.specs {
		as := g.assetStats(spec)
		st.Bytes += as.Bytes
		st.Assets = append(st.Assets, as)
	}
	return st
}

func (g *Group) assetStats(spec assetSpec) AssetStats {
	as := AssetStats{
		Name: spec.name,
		Kind: assetKindName(spec.kind),
		URI:  specURI(spec),
	}
	if spec.kind == kindAtlas {
		as.ImageURI = spec.img.URI
	}
	switch spec.kind {
	case kindAtlas:
		if a, ok := g.atlases[spec.name]; ok && a != nil {
			as.Loaded = true
			as.Frames = len(a.Frames)
			as.Bytes = EstimateAtlasBytes(a, g.rawBytes[spec.name])
		}
	case kindSound:
		if s, ok := g.sounds[spec.name]; ok && s != nil {
			as.Loaded = true
			bytes := int64(len(spec.sound.Data))
			if d := s.Duration(); d > 0 {
				// Decoded 44.1 kHz 16-bit stereo PCM estimate.
				if pcm := int64(math.Round(d.Seconds() * 44100 * 4)); pcm > bytes {
					bytes = pcm
				}
			}
			as.Bytes = bytes
		}
	case kindImage:
		if img, ok := g.images[spec.name]; ok {
			as.Loaded = true
			as.Bytes = estimateImageBytes(img)
			if as.Bytes == 0 {
				as.Bytes = g.rawBytes[spec.name]
			}
		}
	case kindBytes:
		if b, ok := g.bytes[spec.name]; ok {
			as.Loaded = true
			as.Bytes = int64(len(b))
		}
	case kindCustom:
		if c, ok := g.custom[spec.name]; ok {
			as.Loaded = true
			as.Bytes = estimateCustomBytes(c.value, g.rawBytes[spec.name])
		}
	}
	return as
}

func groupStateName(s groupState) string {
	switch s {
	case stateLoading:
		return StateLoading
	case stateLoaded:
		return StateLoaded
	case stateFailed:
		return StateFailed
	default:
		return StateUnloaded
	}
}

func assetKindName(k assetKind) string {
	switch k {
	case kindAtlas:
		return "atlas"
	case kindSound:
		return "sound"
	case kindImage:
		return "image"
	case kindBytes:
		return "bytes"
	case kindCustom:
		return "custom"
	default:
		return "unknown"
	}
}

func specURI(spec assetSpec) string {
	switch spec.kind {
	case kindImage:
		return spec.img.URI
	case kindSound:
		return spec.sound.URI
	default:
		return spec.uri
	}
}

type memorySizer interface {
	MemoryBytes() int64
}

func estimateCustomBytes(v any, rawBytes int64) int64 {
	if sizer, ok := v.(memorySizer); ok {
		if n := sizer.MemoryBytes(); n >= 0 {
			return n
		}
	}
	switch x := v.(type) {
	case []byte:
		return int64(len(x))
	case string:
		return int64(len(x))
	}
	return rawBytes
}

// EstimateAtlasBytes estimates the memory an atlas holds: rawJSONBytes (or a per-frame estimate when it is 0) plus the image bytes, which come from the image's pixels, data or size, or from the packed frame extents at 4 bytes per pixel when the image has only a URI. game/mcp uses it for atlases registered outside a Manager.
func EstimateAtlasBytes(a *sprite.Atlas, rawJSONBytes int64) int64 {
	if a == nil {
		return 0
	}
	total := rawJSONBytes
	if total == 0 {
		total = int64(len(a.Frames)*128 + len(a.Animations)*64 + len(a.Slices)*64)
	}
	imgBytes := estimateImageBytes(a.Image)
	if imgBytes == 0 && len(a.Frames) > 0 {
		// When Image has only a URI without inline Data/Pixels/Size, derive a conservative RGBA pixel estimate from the packed frame extents.
		var maxX, maxY float32
		for _, f := range a.Frames {
			if right := f.X + f.W; right > maxX {
				maxX = right
			}
			if bottom := f.Y + f.H; bottom > maxY {
				maxY = bottom
			}
		}
		if maxX > 0 && maxY > 0 {
			d := float64(a.Image.Density())
			if d <= 0 {
				d = 1
			}
			w := int64(math.Ceil(float64(maxX) * d))
			h := int64(math.Ceil(float64(maxY) * d))
			imgBytes = w * h * 4
		}
	}
	return total + imgBytes
}

func estimateImageBytes(img core.ImageSource) int64 {
	if img.Pixels != nil {
		return int64(len(img.Pixels.Pix))
	}
	if len(img.Data) > 0 {
		return int64(len(img.Data))
	}
	if img.Size.Width > 0 && img.Size.Height > 0 {
		d := float64(img.Density())
		if d <= 0 {
			d = 1
		}
		w := int64(math.Ceil(float64(img.Size.Width) * d))
		h := int64(math.Ceil(float64(img.Size.Height) * d))
		return w * h * 4
	}
	return 0
}

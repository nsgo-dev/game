package random

import (
	"math"
	"math/bits"
)

const (
	// FNV-1a 64-bit constants for deterministic stream derivation.
	fnvOffset64 uint64 = 14695981039346656037
	fnvPrime64  uint64 = 1099511628211
)

// splitMix64 advances x by one SplitMix64 step and returns the mixed output.
func splitMix64(x *uint64) uint64 {
	*x += 0x9e3779b97f4a7c15
	z := *x
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// initState derives the four-word Xoshiro256** state from a 64-bit seed.
// Because SplitMix64 is bijective, four consecutive outputs are never all zero.
func initState(seed uint64) [4]uint64 {
	x := seed
	return [4]uint64{
		splitMix64(&x),
		splitMix64(&x),
		splitMix64(&x),
		splitMix64(&x),
	}
}

// deriveSeed hashes a parent seed and a purpose name into an independent child seed.
func deriveSeed(base uint64, purpose string) uint64 {
	h := fnvOffset64
	for i := range 8 {
		h ^= uint64(byte(base >> (i * 8)))
		h *= fnvPrime64
	}
	// Separate the seed bytes from the purpose bytes.
	h ^= 0xff
	h *= fnvPrime64
	for i := range len(purpose) {
		h ^= uint64(purpose[i])
		h *= fnvPrime64
	}
	return splitMix64(&h)
}

// State is a JSON-marshalable snapshot of a Stream, suitable for save files and replays.
type State struct {
	// Seed is the seed that initialized the stream.
	Seed uint64 `json:"seed"`
	// S is the internal 256-bit Xoshiro256** generator state.
	S [4]uint64 `json:"s"`
}

// Stream is a deterministic pseudo-random number generator backed by Xoshiro256**
// seeded through SplitMix64.
//
// Its output is identical across Go versions, operating systems and architectures.
// Its zero value behaves as New(0). A Stream is not safe for concurrent use by
// multiple goroutines; fork a separate stream for each independent consumer.
type Stream struct {
	seed uint64
	s    [4]uint64
}

// New returns a new Stream seeded with seed.
func New(seed uint64) *Stream {
	return &Stream{
		seed: seed,
		s:    initState(seed),
	}
}

// Seed returns the seed that initialized s.
func (s *Stream) Seed() uint64 {
	return s.seed
}

// Reset rewinds s to the beginning of its seed's sequence.
func (s *Stream) Reset() {
	s.s = initState(s.seed)
}

// Fork derives an independent, deterministic child stream for purpose
// (such as "loot", "particles", "ai" or "level-1") from s's seed.
//
// Fork does not advance s or perturb sibling streams, so forking names in any
// order always produces the same child streams.
func (s *Stream) Fork(purpose string) *Stream {
	return New(deriveSeed(s.seed, purpose))
}

// State returns a snapshot of s that can be serialized and passed to SetState
// to resume the exact same future sequence.
func (s *Stream) State() State {
	if s.s == [4]uint64{} {
		s.s = initState(s.seed)
	}
	return State{Seed: s.seed, S: s.s}
}

// SetState restores s from a snapshot returned by State.
// If st.S is all zero, the generator state is re-derived from st.Seed.
func (s *Stream) SetState(st State) {
	s.seed = st.Seed
	if st.S == [4]uint64{} {
		s.s = initState(st.Seed)
		return
	}
	s.s = st.S
}

// Uint64 returns a uniformly distributed pseudo-random uint64.
func (s *Stream) Uint64() uint64 {
	if s.s == [4]uint64{} {
		s.s = initState(s.seed)
	}
	result := bits.RotateLeft64(s.s[1]*5, 7) * 9
	t := s.s[1] << 17

	s.s[2] ^= s.s[0]
	s.s[3] ^= s.s[1]
	s.s[1] ^= s.s[2]
	s.s[0] ^= s.s[3]

	s.s[2] ^= t
	s.s[3] = bits.RotateLeft64(s.s[3], 45)
	return result
}

// Uint32 returns a uniformly distributed pseudo-random uint32 taken from the
// upper 32 bits of Uint64.
func (s *Stream) Uint32() uint32 {
	return uint32(s.Uint64() >> 32)
}

// uint64n returns a uniformly distributed uint64 in [0, n) using Lemire's
// unbiased rejection method with 64-bit multiplication, independent of platform
// word size and Go version.
func (s *Stream) uint64n(n uint64) uint64 {
	hi, lo := bits.Mul64(s.Uint64(), n)
	if lo < n {
		thresh := -n % n
		for lo < thresh {
			hi, lo = bits.Mul64(s.Uint64(), n)
		}
	}
	return hi
}

// Intn returns a uniformly distributed integer in [0, n).
// It panics if n <= 0.
func (s *Stream) Intn(n int) int {
	if n <= 0 {
		panic("random: Intn argument must be > 0")
	}
	return int(s.uint64n(uint64(n)))
}

// IntRange returns a uniformly distributed integer in [min, max], inclusive.
// It returns min when min == max and panics if min > max.
func (s *Stream) IntRange(min, max int) int {
	if min > max {
		panic("random: IntRange min > max")
	}
	if min == max {
		return min
	}
	span := uint64(max) - uint64(min)
	if span == ^uint64(0) {
		return int(s.Uint64())
	}
	return int(uint64(min) + s.uint64n(span+1))
}

// Float64 returns a uniformly distributed float64 in [0, 1) with 53 bits of precision.
func (s *Stream) Float64() float64 {
	return float64(s.Uint64()>>11) * 0x1p-53
}

// Float32 returns a uniformly distributed float32 in [0, 1) with 24 bits of precision.
func (s *Stream) Float32() float32 {
	return float32(s.Uint64()>>40) * 0x1p-24
}

// Range returns a uniformly distributed float64 in [min, max).
// It returns min when min == max and panics if min > max or either bound is NaN.
func (s *Stream) Range(min, max float64) float64 {
	if !(min <= max) {
		panic("random: Range requires min <= max")
	}
	if min == max {
		return min
	}
	v := min + s.Float64()*(max-min)
	if v >= max {
		v = math.Nextafter(max, min)
	}
	return v
}

// Range32 returns a uniformly distributed float32 in [min, max).
// It returns min when min == max and panics if min > max or either bound is NaN.
func (s *Stream) Range32(min, max float32) float32 {
	if !(min <= max) {
		panic("random: Range32 requires min <= max")
	}
	if min == max {
		return min
	}
	v := min + s.Float32()*(max-min)
	if v >= max {
		v = math.Nextafter32(max, min)
	}
	return v
}

// Bool returns a uniformly distributed boolean.
func (s *Stream) Bool() bool {
	return s.Uint64()>>63 != 0
}

// Chance reports whether an event with probability p in [0, 1] occurs.
// It returns false when p <= 0 or p is NaN, and true when p >= 1.
func (s *Stream) Chance(p float64) bool {
	return s.Float64() < p
}

// WeightedIndex returns an index into weights chosen with probability proportional
// to its weight. It returns -1 when weights is empty or every weight is 0, and
// never returns an index whose weight is 0.
//
// It panics if any weight is negative, NaN or infinite, or if the sum overflows.
func (s *Stream) WeightedIndex(weights []float64) int {
	var total float64
	for _, w := range weights {
		if w < 0 || math.IsNaN(w) || math.IsInf(w, 0) {
			panic("random: WeightedIndex weights must be finite and non-negative")
		}
		total += w
	}
	if math.IsInf(total, 0) {
		panic("random: WeightedIndex total weight overflow")
	}
	if total <= 0 {
		return -1
	}

	target := s.Float64() * total
	var acc float64
	lastPositive := -1
	for i, w := range weights {
		if w == 0 {
			continue
		}
		lastPositive = i
		acc += w
		if target < acc {
			return i
		}
	}
	return lastPositive
}

// Pick returns a uniformly chosen element of items.
// It panics if items is empty.
func Pick[T any](s *Stream, items []T) T {
	if len(items) == 0 {
		panic("random: Pick on empty slice")
	}
	return items[s.Intn(len(items))]
}

// Shuffle randomly permutes items in place using the Fisher-Yates algorithm.
func Shuffle[T any](s *Stream, items []T) {
	for i := len(items) - 1; i > 0; i-- {
		j := s.Intn(i + 1)
		items[i], items[j] = items[j], items[i]
	}
}

// Streams manages named child streams derived from a single master seed.
//
// Each name always resolves to the same independent stream regardless of the
// order in which streams are requested or advanced.
type Streams struct {
	root    *Stream
	streams map[string]*Stream
}

// NewStreams returns a Streams manager rooted at masterSeed.
func NewStreams(masterSeed uint64) *Streams {
	return &Streams{
		root:    New(masterSeed),
		streams: make(map[string]*Stream),
	}
}

// Seed returns the master seed.
func (m *Streams) Seed() uint64 {
	return m.root.Seed()
}

// Stream returns the named child stream, creating it on first use by forking
// from the master seed.
func (m *Streams) Stream(name string) *Stream {
	if s, ok := m.streams[name]; ok {
		return s
	}
	s := m.root.Fork(name)
	m.streams[name] = s
	return s
}

// Reset rewinds the root stream and every stream already obtained from m back
// to its initial state.
func (m *Streams) Reset() {
	m.root.Reset()
	for _, s := range m.streams {
		s.Reset()
	}
}

// States returns a JSON-marshalable snapshot of every stream currently held by m.
func (m *Streams) States() map[string]State {
	out := make(map[string]State, len(m.streams))
	for name, s := range m.streams {
		out[name] = s.State()
	}
	return out
}

// Restore restores m's streams from a snapshot returned by States.
// Streams present in states are created if not yet requested; existing streams
// missing from states are reset to their initial state.
func (m *Streams) Restore(states map[string]State) {
	for name, s := range m.streams {
		if st, ok := states[name]; ok {
			s.SetState(st)
		} else {
			s.Reset()
		}
	}
	for name, st := range states {
		if _, ok := m.streams[name]; !ok {
			s := m.root.Fork(name)
			s.SetState(st)
			m.streams[name] = s
		}
	}
}

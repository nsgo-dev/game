package ai

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// EngagementRing configures one concentric ring of engagement slots around a target.
type EngagementRing struct {
	// Radius is the distance from the target center to each slot on this ring.
	Radius float32
	// Slots is the number of evenly spaced slots around this ring.
	Slots int
	// AngleOffset rotates this ring's slots in radians (0 places slot 0 along +X).
	AngleOffset float64
}

// EngagementSlot is a concrete world-space engagement position around a target.
type EngagementSlot struct {
	// Ring is the zero-based ring index (0 is the first/innermost configured ring).
	Ring int
	// Index is the zero-based slot index within Ring.
	Index int
	// FlatIndex is the zero-based slot index across all rings.
	FlatIndex int
	// Pos is the slot's world position around the target.
	Pos core.Point
}

// SlotAgent describes an agent competing for an engagement slot around a target.
type SlotAgent struct {
	// ID uniquely identifies the agent for deterministic tie-breaking and cross-frame stability.
	ID uint64
	// Pos is the agent's current world position.
	Pos core.Point
}

// SlotAssignment pairs an agent with its assigned engagement slot.
type SlotAssignment struct {
	// AgentID is the agent's ID.
	AgentID uint64
	// Ring is the assigned ring index, or -1 when Assigned is false.
	Ring int
	// Slot is the assigned slot index within Ring, or -1 when Assigned is false.
	Slot int
	// Pos is the assigned slot's world position (zero when Assigned is false).
	Pos core.Point
	// Assigned reports whether the agent received a slot.
	Assigned bool
}

// SlotAssigner assigns agents to concentric engagement rings around a target using optimal
// minimum-cost bipartite matching (the Hungarian algorithm).
//
// Inner rings fill completely before outer rings are used, straight-line paths between agents
// and their assigned slots do not cross (by the triangle inequality on Euclidean distance),
// and assignments are strictly deterministic regardless of input slice order.
//
// Set StayBias > 0 to give each agent a cost bonus (in world distance units) for retaining its
// previous slot as the target moves, suppressing oscillation across slot boundaries.
//
// SlotAssigner reuses internal solver buffers across calls and is not safe for concurrent use.
type SlotAssigner struct {
	// StayBias subtracts this many world units from the assignment cost of an agent's
	// previously held slot so agents do not swap slots unnecessarily as the target moves.
	// Values <= 0 disable hysteresis.
	StayBias float64

	rings      []EngagementRing
	totalSlots int
	prevSlot   map[uint64]int
	lastAssign map[uint64]SlotAssignment

	// Reusable scratch buffers for zero/low-allocation stepping.
	slotsBuf      []EngagementSlot
	agentsBuf     []SlotAgent
	costBuf       []float64
	assignBuf     []int
	slotAssignBuf []int
	hung          hungarianScratch
}

// NewSlotAssigner builds a SlotAssigner for the given concentric rings.
// Rings are filled in the order given (typically innermost first); rings with Slots <= 0 or
// non-finite/negative Radius are ignored.
func NewSlotAssigner(rings ...EngagementRing) *SlotAssigner {
	valid := make([]EngagementRing, 0, len(rings))
	total := 0
	for _, r := range rings {
		if r.Slots <= 0 || r.Radius < 0 || math.IsNaN(float64(r.Radius)) || math.IsInf(float64(r.Radius), 0) {
			continue
		}
		if math.IsNaN(r.AngleOffset) || math.IsInf(r.AngleOffset, 0) {
			r.AngleOffset = 0
		}
		valid = append(valid, r)
		total += r.Slots
	}
	return &SlotAssigner{
		rings:      valid,
		totalSlots: total,
		prevSlot:   make(map[uint64]int),
		lastAssign: make(map[uint64]SlotAssignment),
	}
}

// Rings returns a copy of the assigner's configured engagement rings.
func (a *SlotAssigner) Rings() []EngagementRing {
	return slices.Clone(a.rings)
}

// TotalSlots returns the total number of slots across all configured rings.
func (a *SlotAssigner) TotalSlots() int { return a.totalSlots }

// Slots computes the world-space engagement slots around target and appends them to dst[:0].
func (a *SlotAssigner) Slots(dst []EngagementSlot, target core.Point) []EngagementSlot {
	dst = slices.Grow(dst[:0], a.totalSlots)
	flat := 0
	for ri, r := range a.rings {
		step := 2 * math.Pi / float64(r.Slots)
		rad := float64(r.Radius)
		for si := range r.Slots {
			ang := r.AngleOffset + float64(si)*step
			sn, cs := math.Sincos(ang)
			pos := core.Point{
				X: target.X + core.Points(rad*cs),
				Y: target.Y + core.Points(rad*sn),
			}
			dst = append(dst, EngagementSlot{
				Ring:      ri,
				Index:     si,
				FlatIndex: flat,
				Pos:       pos,
			})
			flat++
		}
	}
	return dst
}

// Assign matches agents to engagement slots around target, appending the assignments to dst[:0]
// sorted by ascending AgentID.
//
// Each agent should have a unique ID; duplicate IDs keep the entry with the earliest position
// after deterministic sorting. Agents no longer present in agents are pruned from stability memory.
func (a *SlotAssigner) Assign(dst []SlotAssignment, target core.Point, agents []SlotAgent) []SlotAssignment {
	dst = dst[:0]
	if len(agents) == 0 {
		clear(a.prevSlot)
		clear(a.lastAssign)
		return dst
	}

	// Canonicalize agents by ascending ID so results never depend on caller ordering.
	a.agentsBuf = append(a.agentsBuf[:0], agents...)
	slices.SortStableFunc(a.agentsBuf, func(x, y SlotAgent) int {
		return cmp.Compare(x.ID, y.ID)
	})
	// Deduplicate by ID.
	n := 0
	for _, ag := range a.agentsBuf {
		if n > 0 && a.agentsBuf[n-1].ID == ag.ID {
			continue
		}
		a.agentsBuf[n] = ag
		n++
	}
	a.agentsBuf = a.agentsBuf[:n]

	if a.totalSlots == 0 {
		clear(a.prevSlot)
		clear(a.lastAssign)
		dst = slices.Grow(dst, n)
		for _, ag := range a.agentsBuf {
			res := SlotAssignment{AgentID: ag.ID, Ring: -1, Slot: -1}
			dst = append(dst, res)
			if a.lastAssign != nil {
				a.lastAssign[ag.ID] = res
			}
		}
		return dst
	}

	a.slotsBuf = a.Slots(a.slotsBuf, target)
	m := len(a.slotsBuf)

	stayBias := a.StayBias
	if stayBias < 0 || math.IsNaN(stayBias) || math.IsInf(stayBias, 0) {
		stayBias = 0
	}

	// Compute pairwise Euclidean distances and max finite distance to calibrate ring tier penalties.
	maxDist := 0.0
	for i := range n {
		ap := toVec2(a.agentsBuf[i].Pos)
		for j := range m {
			d := dist(ap, toVec2(a.slotsBuf[j].Pos))
			if !math.IsNaN(d) && !math.IsInf(d, 0) && d > maxDist {
				maxDist = d
			}
		}
	}
	// Ring penalty step guarantees filling inner rings always beats leaving an inner slot empty
	// for any distance or stay-bias difference across all agents, while keeping all ring swaps
	// invariant so uncrossing any two paths still strictly reduces Euclidean distance.
	ringStep := float64(min(n, m))*(maxDist+stayBias) + 1.0

	if a.assignBuf == nil || cap(a.assignBuf) < n {
		a.assignBuf = make([]int, n)
	} else {
		a.assignBuf = a.assignBuf[:n]
	}
	for i := range n {
		a.assignBuf[i] = -1
	}

	if n <= m {
		// Rows = agents (n), Cols = slots (m).
		a.costBuf = growFloat64(a.costBuf, n*m)
		for i := range n {
			ap := toVec2(a.agentsBuf[i].Pos)
			prev, hasPrev := a.prevSlot[a.agentsBuf[i].ID]
			rowOff := i * m
			for j := range m {
				d := safeDist(ap, toVec2(a.slotsBuf[j].Pos), maxDist+1.0)
				c := d + float64(a.slotsBuf[j].Ring)*ringStep
				if hasPrev && prev == j && stayBias > 0 {
					c -= stayBias
				}
				a.costBuf[rowOff+j] = c
			}
		}
		a.hung.solve(n, m, a.costBuf, a.assignBuf)
	} else {
		// More agents than slots: transpose so Rows = slots (m), Cols = agents (n).
		// Every slot gets an agent; remaining n-m agents are unassigned.
		a.costBuf = growFloat64(a.costBuf, m*n)
		for j := range m {
			sp := toVec2(a.slotsBuf[j].Pos)
			rowOff := j * n
			for i := range n {
				d := safeDist(toVec2(a.agentsBuf[i].Pos), sp, maxDist+1.0)
				c := d
				if prev, hasPrev := a.prevSlot[a.agentsBuf[i].ID]; hasPrev && prev == j && stayBias > 0 {
					c -= stayBias
				}
				a.costBuf[rowOff+i] = c
			}
		}
		a.slotAssignBuf = growZeroInt(a.slotAssignBuf, m)
		for j := range m {
			a.slotAssignBuf[j] = -1
		}
		a.hung.solve(m, n, a.costBuf, a.slotAssignBuf)
		for j, agIdx := range a.slotAssignBuf {
			if agIdx >= 0 && agIdx < n {
				a.assignBuf[agIdx] = j
			}
		}
	}

	if a.prevSlot == nil {
		a.prevSlot = make(map[uint64]int, n)
	} else {
		clear(a.prevSlot)
	}
	if a.lastAssign == nil {
		a.lastAssign = make(map[uint64]SlotAssignment, n)
	} else {
		clear(a.lastAssign)
	}

	dst = slices.Grow(dst, n)
	for i, ag := range a.agentsBuf {
		slotIdx := a.assignBuf[i]
		var res SlotAssignment
		if slotIdx >= 0 && slotIdx < m {
			sl := a.slotsBuf[slotIdx]
			res = SlotAssignment{
				AgentID:  ag.ID,
				Ring:     sl.Ring,
				Slot:     sl.Index,
				Pos:      sl.Pos,
				Assigned: true,
			}
			a.prevSlot[ag.ID] = slotIdx
		} else {
			res = SlotAssignment{
				AgentID:  ag.ID,
				Ring:     -1,
				Slot:     -1,
				Assigned: false,
			}
		}
		a.lastAssign[ag.ID] = res
		dst = append(dst, res)
	}
	return dst
}

// Assignment returns the most recent SlotAssignment computed for agentID.
func (a *SlotAssigner) Assignment(agentID uint64) (SlotAssignment, bool) {
	res, ok := a.lastAssign[agentID]
	return res, ok
}

// Release removes agentID from the assigner's stability memory and last-assignment cache.
func (a *SlotAssigner) Release(agentID uint64) {
	delete(a.prevSlot, agentID)
	delete(a.lastAssign, agentID)
}

// Reset clears all stability memory and cached assignments.
func (a *SlotAssigner) Reset() {
	clear(a.prevSlot)
	clear(a.lastAssign)
}

func safeDist(a, b vec2, fallback float64) float64 {
	d := dist(a, b)
	if math.IsNaN(d) || math.IsInf(d, 0) {
		return fallback
	}
	return d
}

// hungarianScratch holds reusable buffers for the O(n^2 m) Hungarian (Jonker-Volgenant/Kuhn-Munkres)
// minimum-weight bipartite matching algorithm with 0 < n <= m.
type hungarianScratch struct {
	u, v, minv []float64
	p, way     []int
	used       []bool
}

func (h *hungarianScratch) solve(n, m int, cost []float64, assign []int) {
	if n <= 0 || m <= 0 {
		return
	}
	h.u = growZeroFloat64(h.u, n+1)
	h.v = growZeroFloat64(h.v, m+1)
	h.minv = growFloat64(h.minv, m+1)
	h.p = growZeroInt(h.p, m+1)
	h.way = growZeroInt(h.way, m+1)
	h.used = growBool(h.used, m+1)

	for i := 1; i <= n; i++ {
		h.p[0] = i
		j0 := 0
		for j := 0; j <= m; j++ {
			h.minv[j] = math.Inf(1)
			h.used[j] = false
		}
		for {
			h.used[j0] = true
			i0 := h.p[j0]
			delta := math.Inf(1)
			j1 := 0
			rowOff := (i0 - 1) * m
			for j := 1; j <= m; j++ {
				if h.used[j] {
					continue
				}
				cur := cost[rowOff+j-1] - h.u[i0] - h.v[j]
				if cur < h.minv[j] {
					h.minv[j] = cur
					h.way[j] = j0
				}
				if h.minv[j] < delta {
					delta = h.minv[j]
					j1 = j
				}
			}
			if j1 == 0 {
				break
			}
			for j := 0; j <= m; j++ {
				if h.used[j] {
					h.u[h.p[j]] += delta
					h.v[j] -= delta
				} else {
					h.minv[j] -= delta
				}
			}
			j0 = j1
			if h.p[j0] == 0 {
				break
			}
		}
		if j0 == 0 {
			continue
		}
		for {
			j1 := h.way[j0]
			h.p[j0] = h.p[j1]
			j0 = j1
			if j0 == 0 {
				break
			}
		}
	}

	for j := 1; j <= m; j++ {
		if h.p[j] > 0 {
			assign[h.p[j]-1] = j - 1
		}
	}
}

func growFloat64(s []float64, n int) []float64 {
	if cap(s) < n {
		return make([]float64, n)
	}
	return s[:n]
}

func growZeroFloat64(s []float64, n int) []float64 {
	if cap(s) < n {
		return make([]float64, n)
	}
	s = s[:n]
	clear(s)
	return s
}

func growZeroInt(s []int, n int) []int {
	if cap(s) < n {
		return make([]int, n)
	}
	s = s[:n]
	clear(s)
	return s
}

func growBool(s []bool, n int) []bool {
	if cap(s) < n {
		return make([]bool, n)
	}
	return s[:n]
}

// SquadState is a shared key-value state store for a Squad, readable and writable by squad
// members and their behavior trees. Its zero value is ready to use.
type SquadState struct {
	values map[string]any
}

// Set stores value under key.
func (s *SquadState) Set(key string, value any) {
	if s.values == nil {
		s.values = make(map[string]any)
	}
	s.values[key] = value
}

// Get retrieves the value stored under key.
func (s *SquadState) Get(key string) (any, bool) {
	v, ok := s.values[key]
	return v, ok
}

// Has reports whether key is present in s.
func (s *SquadState) Has(key string) bool {
	_, ok := s.values[key]
	return ok
}

// Delete removes key from s.
func (s *SquadState) Delete(key string) {
	delete(s.values, key)
}

// Clear removes all entries from s.
func (s *SquadState) Clear() {
	clear(s.values)
}

// Len returns the number of keys stored in s.
func (s *SquadState) Len() int { return len(s.values) }

// Keys appends all keys in s to dst[:0] in deterministic ascending lexicographical order.
func (s *SquadState) Keys(dst []string) []string {
	dst = slices.Grow(dst[:0], len(s.values))
	for k := range s.values {
		dst = append(dst, k)
	}
	slices.Sort(dst)
	return dst
}

// SetBool stores a bool value under key.
func (s *SquadState) SetBool(key string, v bool) { s.Set(key, v) }

// Bool retrieves a bool value stored under key.
func (s *SquadState) Bool(key string) (bool, bool) { return SquadStateGet[bool](s, key) }

// SetInt stores an int value under key.
func (s *SquadState) SetInt(key string, v int) { s.Set(key, v) }

// Int retrieves an int value stored under key.
func (s *SquadState) Int(key string) (int, bool) { return SquadStateGet[int](s, key) }

// SetFloat stores a float64 value under key.
func (s *SquadState) SetFloat(key string, v float64) { s.Set(key, v) }

// Float retrieves a float64 value stored under key.
func (s *SquadState) Float(key string) (float64, bool) { return SquadStateGet[float64](s, key) }

// SetString stores a string value under key.
func (s *SquadState) SetString(key string, v string) { s.Set(key, v) }

// String retrieves a string value stored under key.
func (s *SquadState) String(key string) (string, bool) { return SquadStateGet[string](s, key) }

// SetPoint stores a core.Point value under key.
func (s *SquadState) SetPoint(key string, v core.Point) { s.Set(key, v) }

// Point retrieves a core.Point value stored under key.
func (s *SquadState) Point(key string) (core.Point, bool) { return SquadStateGet[core.Point](s, key) }

// SetID stores a uint64 identifier under key.
func (s *SquadState) SetID(key string, v uint64) { s.Set(key, v) }

// ID retrieves a uint64 identifier stored under key.
func (s *SquadState) ID(key string) (uint64, bool) { return SquadStateGet[uint64](s, key) }

// SquadStateGet retrieves a typed value of type T from s, returning (zero, false) when s is nil,
// the key is missing, or the stored value has a different type.
func SquadStateGet[T any](s *SquadState, key string) (T, bool) {
	var zero T
	if s == nil {
		return zero, false
	}
	v, ok := s.values[key]
	if !ok {
		return zero, false
	}
	typed, ok := v.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}

// SquadOrder is a squad-level directive readable by members' behavior trees.
type SquadOrder struct {
	// Kind names the directive (for example, "engage", "flank", "suppress", "hold", "retreat").
	// An empty Kind indicates no active order.
	Kind string
	// TargetID identifies an optional target entity for the order.
	TargetID uint64
	// Position specifies an optional world-space coordinate (such as a rally or flank point).
	Position core.Point
	// Revision increments each time Squad.SetOrder or Squad.ClearOrder updates the order,
	// letting behavior trees detect freshly issued orders even with the same Kind.
	Revision uint64
}

// SquadRoleSpec describes a tactical role to fill within a Squad and how each member scores for it.
type SquadRoleSpec struct {
	// Name identifies the role (for example, "flanker", "suppressor", "rusher").
	Name string
	// Count is the maximum number of squad members to assign to this role.
	// Values <= 0 assign no members; counts larger than the squad size are clamped to the squad size.
	Count int
	// Score evaluates how well agentID fits this role (higher is better; negative finite scores
	// such as -distance are supported). Return a non-finite value (NaN or ±Inf) to mark agentID
	// ineligible for this role. A nil Score treats all members as equally eligible with score 0.
	Score func(agentID uint64) float64
}

// Squad coordinates a group of AI agents with a deterministic member roster, shared SquadState,
// score-driven tactical role assignments, and squad-level orders readable by behavior trees.
//
// Squad is not safe for concurrent use; drive it on game.Loop's fixed steps.
type Squad struct {
	id       uint64
	members  []uint64 // kept sorted ascending by AgentID
	roles    map[uint64]string
	order    SquadOrder
	orderRev uint64
	state    SquadState

	// Reusable scratch for AssignRoles.
	hung      hungarianScratch
	costBuf   []float64
	slotBuf   []string
	assignBuf []int
}

// NewSquad returns a Squad with identifier id and optional initial member IDs.
func NewSquad(id uint64, members ...uint64) *Squad {
	s := &Squad{id: id}
	for _, m := range members {
		s.AddMember(m)
	}
	return s
}

// ID returns the squad's identifier.
func (s *Squad) ID() uint64 { return s.id }

// AddMember adds agentID to the squad roster, maintaining ascending ID order.
// It reports whether agentID was newly added.
func (s *Squad) AddMember(agentID uint64) bool {
	idx, found := slices.BinarySearch(s.members, agentID)
	if found {
		return false
	}
	s.members = slices.Insert(s.members, idx, agentID)
	return true
}

// RemoveMember removes agentID from the squad roster and clears its assigned role.
// It reports whether agentID was a member.
func (s *Squad) RemoveMember(agentID uint64) bool {
	idx, found := slices.BinarySearch(s.members, agentID)
	if !found {
		return false
	}
	s.members = slices.Delete(s.members, idx, idx+1)
	delete(s.roles, agentID)
	return true
}

// HasMember reports whether agentID belongs to the squad.
func (s *Squad) HasMember(agentID uint64) bool {
	_, found := slices.BinarySearch(s.members, agentID)
	return found
}

// MemberCount returns the number of members in the squad.
func (s *Squad) MemberCount() int { return len(s.members) }

// Members appends all member IDs to dst[:0] in deterministic ascending order.
func (s *Squad) Members(dst []uint64) []uint64 {
	return append(dst[:0], s.members...)
}

// State returns the squad's shared key-value state.
func (s *Squad) State() *SquadState { return &s.state }

// Order returns the squad's current directive.
func (s *Squad) Order() SquadOrder { return s.order }

// SetOrder updates the squad's directive, increments Revision, and returns the stored order.
func (s *Squad) SetOrder(order SquadOrder) SquadOrder {
	s.orderRev++
	order.Revision = s.orderRev
	s.order = order
	return s.order
}

// ClearOrder clears the squad's directive and increments Revision.
func (s *Squad) ClearOrder() {
	s.orderRev++
	s.order = SquadOrder{Revision: s.orderRev}
}

// Role returns the tactical role currently assigned to agentID, or "" if unassigned or not a member.
func (s *Squad) Role(agentID uint64) string {
	return s.roles[agentID]
}

// HasRole reports whether agentID is a member currently assigned role.
func (s *Squad) HasRole(agentID uint64, role string) bool {
	if role == "" {
		return false
	}
	return s.roles[agentID] == role
}

// SetRole manually assigns role to agentID if agentID is a member (passing "" unassigns it).
// It reports whether agentID is in the squad.
func (s *Squad) SetRole(agentID uint64, role string) bool {
	if !s.HasMember(agentID) {
		return false
	}
	if role == "" {
		delete(s.roles, agentID)
		return true
	}
	if s.roles == nil {
		s.roles = make(map[uint64]string)
	}
	s.roles[agentID] = role
	return true
}

// ClearRoles removes all role assignments from the squad.
func (s *Squad) ClearRoles() {
	clear(s.roles)
}

// MembersWithRole appends all member IDs holding role to dst[:0] in deterministic ascending order.
func (s *Squad) MembersWithRole(dst []uint64, role string) []uint64 {
	dst = dst[:0]
	if role == "" {
		return dst
	}
	for _, id := range s.members {
		if s.roles[id] == role {
			dst = append(dst, id)
		}
	}
	return dst
}

// AssignRoles assigns tactical roles to current squad members using maximum-weight bipartite
// matching driven by each spec's Score function, replacing previous role assignments.
//
// AssignRoles maximizes the number of filled eligible role slots first and total score second.
// Ineligible pairs (Score returning NaN or ±Inf) are never assigned. Ties break deterministically
// by spec declaration order and ascending member ID. Returns the number of members assigned a role.
func (s *Squad) AssignRoles(specs ...SquadRoleSpec) int {
	clear(s.roles)
	n := len(s.members)
	if n == 0 || len(specs) == 0 {
		return 0
	}

	// Expand specs into role slots (clamping each spec's Count to n).
	s.slotBuf = s.slotBuf[:0]
	type specSpan struct {
		specIdx int
		start   int
		count   int
	}
	spans := make([]specSpan, 0, len(specs))
	for si, sp := range specs {
		if sp.Name == "" || sp.Count <= 0 {
			continue
		}
		cnt := min(sp.Count, n)
		start := len(s.slotBuf)
		for range cnt {
			s.slotBuf = append(s.slotBuf, sp.Name)
		}
		spans = append(spans, specSpan{specIdx: si, start: start, count: cnt})
	}
	numSlots := len(s.slotBuf)
	if numSlots == 0 {
		return 0
	}

	// Evaluate scores once per (member, spec) in deterministic order.
	numSpecs := len(spans)
	scores := make([]float64, n*numSpecs)
	eligible := make([]bool, n*numSpecs)
	minScore := math.Inf(1)
	maxScore := math.Inf(-1)
	anyEligible := false
	for i, id := range s.members {
		for k, sp := range spans {
			fn := specs[sp.specIdx].Score
			v := 0.0
			if fn != nil {
				v = fn(id)
			}
			idx := i*numSpecs + k
			if math.IsNaN(v) || math.IsInf(v, 0) {
				eligible[idx] = false
				continue
			}
			eligible[idx] = true
			scores[idx] = v
			anyEligible = true
			if v < minScore {
				minScore = v
			}
			if v > maxScore {
				maxScore = v
			}
		}
	}
	if !anyEligible {
		return 0
	}

	// Build an n x (numSlots + n) cost matrix where the final n columns are 0-cost "unassigned"
	// dummy columns. Normalize eligible scores into [0, 1] (using half-scaling if maxScore-minScore
	// overflows float64) and set bigBonus = n + 2 > n so filling an eligible role always beats
	// leaving a member unassigned while maximizing total score among equal-cardinality matchings.
	cols := numSlots + n
	diff := maxScore - minScore
	useHalfScale := math.IsInf(diff, 0)
	halfDiff := 0.0
	if useHalfScale {
		halfDiff = maxScore*0.5 - minScore*0.5
	}
	bigBonus := float64(n) + 2.0

	s.costBuf = growFloat64(s.costBuf, n*cols)
	for i := range n {
		rowOff := i * cols
		for k, sp := range spans {
			idx := i*numSpecs + k
			var c float64
			if eligible[idx] {
				norm := 0.0
				if useHalfScale && halfDiff > 0 {
					norm = (scores[idx]*0.5 - minScore*0.5) / halfDiff
				} else if diff > 0 {
					norm = (scores[idx] - minScore) / diff
				}
				c = -(bigBonus + norm)
			} else {
				c = bigBonus
			}
			for j := sp.start; j < sp.start+sp.count; j++ {
				s.costBuf[rowOff+j] = c
			}
		}
		// Dummy unassigned columns have cost 0.
		for j := numSlots; j < cols; j++ {
			s.costBuf[rowOff+j] = 0
		}
	}

	s.assignBuf = growZeroInt(s.assignBuf, n)
	for i := range n {
		s.assignBuf[i] = -1
	}
	s.hung.solve(n, cols, s.costBuf, s.assignBuf)

	if s.roles == nil {
		s.roles = make(map[uint64]string, n)
	}
	assigned := 0
	for i, col := range s.assignBuf {
		if col >= 0 && col < numSlots {
			s.roles[s.members[i]] = s.slotBuf[col]
			assigned++
		}
	}
	return assigned
}

// SquadHasOrder returns a Condition behavior tree node that succeeds when squad has an active
// order matching kind (or any non-empty order when kind is "").
func SquadHasOrder(squad *Squad, kind string) Node {
	return Condition(func() bool {
		if squad == nil {
			return false
		}
		cur := squad.Order().Kind
		if kind == "" {
			return cur != ""
		}
		return cur == kind
	})
}

// SquadHasRole returns a Condition behavior tree node that succeeds when agentID holds role in squad.
func SquadHasRole(squad *Squad, agentID uint64, role string) Node {
	return Condition(func() bool {
		return squad != nil && squad.HasRole(agentID, role)
	})
}

// SquadStateBool returns a Condition behavior tree node that succeeds when squad's shared state
// holds a true bool value under key.
func SquadStateBool(squad *Squad, key string) Node {
	return Condition(func() bool {
		if squad == nil {
			return false
		}
		v, ok := squad.State().Bool(key)
		return ok && v
	})
}

// SquadOrderAction returns an Action behavior tree node that reads squad's current SquadOrder
// and invokes fn(order, dt). It returns StatusFailure when squad or fn is nil or no order is active.
func SquadOrderAction(squad *Squad, fn func(order SquadOrder, dt time.Duration) Status) Node {
	return Action(func(dt time.Duration) Status {
		if squad == nil || fn == nil {
			return StatusFailure
		}
		ord := squad.Order()
		if ord.Kind == "" {
			return StatusFailure
		}
		return fn(ord, dt)
	})
}

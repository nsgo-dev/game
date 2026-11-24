package net

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// EntityID uniquely identifies a replicated entity in a ServerWorld / ClientWorld.
type EntityID uint32

// ComponentState holds the replicated transform, velocity, flags, and custom
// properties of an entity.
type ComponentState struct {
	Kind     uint16
	X        float32
	Y        float32
	VX       float32
	VY       float32
	Rotation float32
	Flags    uint32
	Props    []byte
}

// Clone returns a deep copy of ComponentState (duplicating Props).
func (c ComponentState) Clone() ComponentState {
	if len(c.Props) > 0 {
		cp := make([]byte, len(c.Props))
		copy(cp, c.Props)
		c.Props = cp
	} else {
		c.Props = nil
	}
	return c
}

// EntityState pairs an EntityID with its replicated ComponentState.
type EntityState struct {
	ID EntityID
	ComponentState
}

// Clone returns a deep copy of EntityState.
func (e EntityState) Clone() EntityState {
	e.ComponentState = e.ComponentState.Clone()
	return e
}

const (
	replMsgSnapshotFull  byte = 1
	replMsgSnapshotDelta byte = 2
	replMsgSnapshotAck   byte = 3
	// replMsgResync asks the server to send a full snapshot because the client
	// no longer holds the baseline a delta needs.
	replMsgResync byte = 4
	// replMsgSnapshotFullPart carries one part of a full snapshot too large for one packet: tick, part index, part count, then the entity count and entities of a full snapshot.
	replMsgSnapshotFullPart byte = 5
)

const (
	// DefaultMaxSnapshotSize is the largest snapshot payload a ServerWorld passes to Transport.Send unless SetMaxSnapshotSize changes it. It fits every transport in this package: UDP datagrams (65000 bytes), relay messages (MaxRelayPayload) and WebSocket frames.
	DefaultMaxSnapshotSize = 60000
	// minSnapshotSize is the smallest size SetMaxSnapshotSize accepts.
	minSnapshotSize = 256
	// snapshotPartHeader is the most bytes a part's header takes: the message type and four uvarints of at most 5 bytes each.
	snapshotPartHeader = 1 + 4*binary.MaxVarintLen32
	// maxSnapshotParts bounds how many parts one full snapshot may be split into, on both ends.
	maxSnapshotParts = 4096
)

// tickAfter reports whether tick a comes after tick b in serial-number order (RFC 1982). A uint32 tick counter wraps after 2^32 ticks, about 2.27 years at 60 Hz; compared as plain integers, every snapshot after the wrap would look older than the last one and be dropped (#546). Ticks more than 2^31 apart compare the wrong way, far beyond any history replication keeps.
func tickAfter(a, b uint32) bool {
	return int32(a-b) > 0
}

const (
	fieldKind     uint8 = 1 << 0
	fieldX        uint8 = 1 << 1
	fieldY        uint8 = 1 << 2
	fieldVX       uint8 = 1 << 3
	fieldVY       uint8 = 1 << 4
	fieldRotation uint8 = 1 << 5
	fieldFlags    uint8 = 1 << 6
	fieldProps    uint8 = 1 << 7
)

const defaultSnapshotHistory = 128

// ReplicationStats reports cumulative bandwidth and snapshot metrics for ServerWorld.
type ReplicationStats struct {
	BytesSent          uint64
	PacketsSent        uint64
	SnapshotsSent      uint64
	FullSnapshotsSent  uint64
	DeltaSnapshotsSent uint64
	AvgBytesPerTick    float64
}

type worldSnapshot struct {
	tick uint32
	// ext is tick extended past 32 bits, counting wraparounds, so interpolation can order snapshots on either side of a wrap.
	ext      int64
	entities map[EntityID]EntityState
}

type peerReplState struct {
	ackedTick uint32
	hasAck    bool
}

// ServerWorld owns authoritative entity state and replicates compact full and
// bitmask-delta snapshots to connected peers over a Transport.
type ServerWorld struct {
	transport Transport
	nextID    EntityID
	entities  map[EntityID]EntityState

	historyCap   int
	historyOrder []uint32
	history      map[uint32]map[EntityID]EntityState
	peers        map[PeerID]*peerReplState

	stats     ReplicationStats
	stepTicks uint64
	maxSize   int
}

// NewServerWorld creates a new authoritative replication world backed by transport.
func NewServerWorld(transport Transport) *ServerWorld {
	return &ServerWorld{
		transport:  transport,
		nextID:     1,
		entities:   make(map[EntityID]EntityState),
		historyCap: defaultSnapshotHistory,
		history:    make(map[uint32]map[EntityID]EntityState),
		peers:      make(map[PeerID]*peerReplState),
		maxSize:    DefaultMaxSnapshotSize,
	}
}

// SetMaxSnapshotSize sets the largest payload Step passes to Transport.Send. A full snapshot larger than n is split into parts of at most n bytes, which ClientWorld reassembles before applying, and a delta larger than n is replaced by a full snapshot split the same way. Without the split, a world that outgrew the transport's packet limit could never reach a new peer again (#546). Zero restores DefaultMaxSnapshotSize, and a value below 256 is raised to 256.
func (w *ServerWorld) SetMaxSnapshotSize(n int) {
	if n == 0 {
		n = DefaultMaxSnapshotSize
	}
	w.maxSize = max(n, minSnapshotSize)
}

// Spawn creates a new entity with the given initial state and returns its assigned EntityID.
func (w *ServerWorld) Spawn(state EntityState) EntityID {
	id := w.nextID
	w.nextID++
	cp := state.Clone()
	cp.ID = id
	w.entities[id] = cp
	return id
}

// Update replaces the state of an existing entity. Returns false if id does not exist.
func (w *ServerWorld) Update(id EntityID, state EntityState) bool {
	if _, ok := w.entities[id]; !ok {
		return false
	}
	cp := state.Clone()
	cp.ID = id
	w.entities[id] = cp
	return true
}

// Despawn removes an entity from the authoritative world. Returns false if id does not exist.
func (w *ServerWorld) Despawn(id EntityID) bool {
	if _, ok := w.entities[id]; !ok {
		return false
	}
	delete(w.entities, id)
	return true
}

// Entity returns a copy of the authoritative state for id.
func (w *ServerWorld) Entity(id EntityID) (EntityState, bool) {
	e, ok := w.entities[id]
	if !ok {
		return EntityState{}, false
	}
	return e.Clone(), true
}

// Entities returns all active entities sorted by EntityID.
func (w *ServerWorld) Entities() []EntityState {
	ids := make([]EntityID, 0, len(w.entities))
	for id := range w.entities {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]EntityState, 0, len(ids))
	for _, id := range ids {
		out = append(out, w.entities[id].Clone())
	}
	return out
}

// Stats returns cumulative replication bandwidth and snapshot statistics.
func (w *ServerWorld) Stats() ReplicationStats {
	s := w.stats
	if w.stepTicks > 0 {
		s.AvgBytesPerTick = float64(s.BytesSent) / float64(w.stepTicks)
	}
	return s
}

// Step processes incoming client acknowledgments, records the world snapshot for tick,
// and transmits either a full snapshot (for new peers) or a bitmask delta snapshot
// (for acknowledged peers) over the Transport.
func (w *ServerWorld) Step(tick uint32) error {
	w.drainAcks()
	w.recordSnapshot(tick)

	connected := w.transport.Peers()
	if len(connected) == 0 {
		return nil
	}

	// Prune disconnected peer tracking.
	connSet := make(map[PeerID]struct{}, len(connected))
	for _, p := range connected {
		connSet[p] = struct{}{}
	}
	for p := range w.peers {
		if _, ok := connSet[p]; !ok {
			delete(w.peers, p)
		}
	}

	w.stepTicks++
	var firstErr error
	var fullParts [][]byte
	var fullErr error

	for _, peerID := range connected {
		ps := w.peers[peerID]
		if ps == nil {
			ps = &peerReplState{}
			w.peers[peerID] = ps
		}

		var parts [][]byte
		isDelta := false
		if ps.hasAck && tickAfter(tick, ps.ackedTick) {
			if base, ok := w.history[ps.ackedTick]; ok {
				// A delta that outgrew the packet limit falls back to a split full snapshot.
				if delta := encodeDeltaSnapshot(tick, ps.ackedTick, base, w.entities); len(delta) <= w.maxSize {
					parts = [][]byte{delta}
					isDelta = true
				}
			}
		}
		if !isDelta {
			if fullParts == nil && fullErr == nil {
				fullParts, fullErr = w.encodeFull(tick)
			}
			if fullErr != nil {
				if firstErr == nil {
					firstErr = fullErr
				}
				continue
			}
			parts = fullParts
		}

		sent := true
		for _, payload := range parts {
			if err := w.transport.Send(peerID, ChannelUnreliable, payload); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				sent = false
				break
			}
			w.stats.BytesSent += uint64(len(payload))
			w.stats.PacketsSent++
		}
		if !sent {
			continue
		}
		w.stats.SnapshotsSent++
		if isDelta {
			w.stats.DeltaSnapshotsSent++
		} else {
			w.stats.FullSnapshotsSent++
		}
	}
	return firstErr
}

// encodeFull encodes the full snapshot for tick as one payload when it fits the size limit, and as parts otherwise.
func (w *ServerWorld) encodeFull(tick uint32) ([][]byte, error) {
	maxSize := w.maxSize
	if maxSize == 0 {
		maxSize = DefaultMaxSnapshotSize
	}
	if full := encodeFullSnapshot(tick, w.entities); len(full) <= maxSize {
		return [][]byte{full}, nil
	}
	return encodeFullSnapshotParts(tick, w.entities, maxSize)
}

func (w *ServerWorld) drainAcks() {
	for {
		pkt, ok := w.transport.Recv()
		if !ok {
			return
		}
		if len(pkt.Payload) < 1 {
			continue
		}
		switch pkt.Payload[0] {
		case replMsgSnapshotAck:
			if len(pkt.Payload) < 2 {
				continue
			}
			ackTick, n := binary.Uvarint(pkt.Payload[1:])
			if n <= 0 {
				continue
			}
			t := uint32(ackTick)
			if _, exists := w.history[t]; !exists {
				continue
			}
			ps := w.peers[pkt.From]
			if ps == nil {
				ps = &peerReplState{}
				w.peers[pkt.From] = ps
			}
			if !ps.hasAck || tickAfter(t, ps.ackedTick) {
				ps.ackedTick = t
				ps.hasAck = true
			}
		case replMsgResync:
			// The peer cannot apply deltas from its last ack any more (its
			// baseline was pruned or a snapshot was lost): send it a full one.
			ps := w.peers[pkt.From]
			if ps == nil {
				ps = &peerReplState{}
				w.peers[pkt.From] = ps
			}
			ps.hasAck = false
		}
	}
}

func (w *ServerWorld) recordSnapshot(tick uint32) {
	snap := make(map[EntityID]EntityState, len(w.entities))
	for id, e := range w.entities {
		snap[id] = e.Clone()
	}
	if _, exists := w.history[tick]; !exists {
		w.historyOrder = append(w.historyOrder, tick)
	}
	w.history[tick] = snap

	for len(w.historyOrder) > w.historyCap {
		oldest := w.historyOrder[0]
		w.historyOrder = w.historyOrder[1:]
		delete(w.history, oldest)
	}
}

func diffComponentMask(base, cur ComponentState) uint8 {
	var mask uint8
	if cur.Kind != base.Kind {
		mask |= fieldKind
	}
	if math.Float32bits(cur.X) != math.Float32bits(base.X) {
		mask |= fieldX
	}
	if math.Float32bits(cur.Y) != math.Float32bits(base.Y) {
		mask |= fieldY
	}
	if math.Float32bits(cur.VX) != math.Float32bits(base.VX) {
		mask |= fieldVX
	}
	if math.Float32bits(cur.VY) != math.Float32bits(base.VY) {
		mask |= fieldVY
	}
	if math.Float32bits(cur.Rotation) != math.Float32bits(base.Rotation) {
		mask |= fieldRotation
	}
	if cur.Flags != base.Flags {
		mask |= fieldFlags
	}
	if !bytes.Equal(cur.Props, base.Props) {
		mask |= fieldProps
	}
	return mask
}

func appendEntityFields(dst []byte, id EntityID, mask uint8, c ComponentState) []byte {
	dst = binary.AppendUvarint(dst, uint64(id))
	dst = append(dst, mask)
	if mask&fieldKind != 0 {
		dst = binary.AppendUvarint(dst, uint64(c.Kind))
	}
	if mask&fieldX != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, math.Float32bits(c.X))
	}
	if mask&fieldY != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, math.Float32bits(c.Y))
	}
	if mask&fieldVX != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, math.Float32bits(c.VX))
	}
	if mask&fieldVY != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, math.Float32bits(c.VY))
	}
	if mask&fieldRotation != 0 {
		dst = binary.LittleEndian.AppendUint32(dst, math.Float32bits(c.Rotation))
	}
	if mask&fieldFlags != 0 {
		dst = binary.AppendUvarint(dst, uint64(c.Flags))
	}
	if mask&fieldProps != 0 {
		dst = binary.AppendUvarint(dst, uint64(len(c.Props)))
		dst = append(dst, c.Props...)
	}
	return dst
}

func readEntityFields(src []byte, base ComponentState) (EntityID, ComponentState, []byte, bool) {
	idVal, n := binary.Uvarint(src)
	if n <= 0 || len(src) <= n {
		return 0, ComponentState{}, nil, false
	}
	src = src[n:]
	mask := src[0]
	src = src[1:]

	cur := base.Clone()
	if mask&fieldKind != 0 {
		v, m := binary.Uvarint(src)
		if m <= 0 {
			return 0, ComponentState{}, nil, false
		}
		cur.Kind = uint16(v)
		src = src[m:]
	}
	if mask&fieldX != 0 {
		if len(src) < 4 {
			return 0, ComponentState{}, nil, false
		}
		cur.X = math.Float32frombits(binary.LittleEndian.Uint32(src[:4]))
		src = src[4:]
	}
	if mask&fieldY != 0 {
		if len(src) < 4 {
			return 0, ComponentState{}, nil, false
		}
		cur.Y = math.Float32frombits(binary.LittleEndian.Uint32(src[:4]))
		src = src[4:]
	}
	if mask&fieldVX != 0 {
		if len(src) < 4 {
			return 0, ComponentState{}, nil, false
		}
		cur.VX = math.Float32frombits(binary.LittleEndian.Uint32(src[:4]))
		src = src[4:]
	}
	if mask&fieldVY != 0 {
		if len(src) < 4 {
			return 0, ComponentState{}, nil, false
		}
		cur.VY = math.Float32frombits(binary.LittleEndian.Uint32(src[:4]))
		src = src[4:]
	}
	if mask&fieldRotation != 0 {
		if len(src) < 4 {
			return 0, ComponentState{}, nil, false
		}
		cur.Rotation = math.Float32frombits(binary.LittleEndian.Uint32(src[:4]))
		src = src[4:]
	}
	if mask&fieldFlags != 0 {
		v, m := binary.Uvarint(src)
		if m <= 0 {
			return 0, ComponentState{}, nil, false
		}
		cur.Flags = uint32(v)
		src = src[m:]
	}
	if mask&fieldProps != 0 {
		plen, m := binary.Uvarint(src)
		if m <= 0 || uint64(len(src)-m) < plen {
			return 0, ComponentState{}, nil, false
		}
		src = src[m:]
		if plen > 0 {
			cp := make([]byte, plen)
			copy(cp, src[:plen])
			cur.Props = cp
		} else {
			cur.Props = nil
		}
		src = src[plen:]
	}
	return EntityID(idVal), cur, src, true
}

func encodeFullSnapshot(tick uint32, entities map[EntityID]EntityState) []byte {
	ids := make([]EntityID, 0, len(entities))
	for id := range entities {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	buf := make([]byte, 0, 16+len(ids)*24)
	buf = append(buf, replMsgSnapshotFull)
	buf = binary.AppendUvarint(buf, uint64(tick))
	buf = binary.AppendUvarint(buf, uint64(len(ids)))
	var zero ComponentState
	for _, id := range ids {
		e := entities[id]
		mask := diffComponentMask(zero, e.ComponentState)
		buf = appendEntityFields(buf, id, mask, e.ComponentState)
	}
	return buf
}

// encodeFullSnapshotParts splits a full snapshot into parts of at most maxSize bytes, each holding whole entities in ascending ID order. An entity that does not fit in a part on its own, or a snapshot that needs more than maxSnapshotParts parts, returns ErrPayloadTooLarge.
func encodeFullSnapshotParts(tick uint32, entities map[EntityID]EntityState, maxSize int) ([][]byte, error) {
	ids := make([]EntityID, 0, len(entities))
	for id := range entities {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	budget := maxSize - snapshotPartHeader
	type group struct {
		body  []byte
		count int
	}
	var groups []group
	var cur group
	var zero ComponentState
	var scratch []byte
	for _, id := range ids {
		e := entities[id]
		scratch = appendEntityFields(scratch[:0], id, diffComponentMask(zero, e.ComponentState), e.ComponentState)
		if len(scratch) > budget {
			return nil, fmt.Errorf("%w: entity %d needs %d bytes, more than a %d-byte snapshot part holds", ErrPayloadTooLarge, id, len(scratch), maxSize)
		}
		if len(cur.body)+len(scratch) > budget {
			groups = append(groups, cur)
			cur = group{}
		}
		cur.body = append(cur.body, scratch...)
		cur.count++
	}
	if cur.count > 0 {
		groups = append(groups, cur)
	}
	if len(groups) > maxSnapshotParts {
		return nil, fmt.Errorf("%w: full snapshot needs %d parts, more than %d", ErrPayloadTooLarge, len(groups), maxSnapshotParts)
	}

	parts := make([][]byte, len(groups))
	for i, g := range groups {
		buf := make([]byte, 0, snapshotPartHeader+len(g.body))
		buf = append(buf, replMsgSnapshotFullPart)
		buf = binary.AppendUvarint(buf, uint64(tick))
		buf = binary.AppendUvarint(buf, uint64(i))
		buf = binary.AppendUvarint(buf, uint64(len(groups)))
		buf = binary.AppendUvarint(buf, uint64(g.count))
		parts[i] = append(buf, g.body...)
	}
	return parts, nil
}

func encodeDeltaSnapshot(tick, baselineTick uint32, baseline, current map[EntityID]EntityState) []byte {
	var despawns []EntityID
	for id := range baseline {
		if _, ok := current[id]; !ok {
			despawns = append(despawns, id)
		}
	}
	slices.Sort(despawns)

	type encodedUpdate struct {
		id   EntityID
		mask uint8
		comp ComponentState
	}
	var updates []encodedUpdate
	var zero ComponentState
	for id, cur := range current {
		base, existed := baseline[id]
		if !existed {
			mask := diffComponentMask(zero, cur.ComponentState)
			updates = append(updates, encodedUpdate{id: id, mask: mask, comp: cur.ComponentState})
			continue
		}
		mask := diffComponentMask(base.ComponentState, cur.ComponentState)
		if mask != 0 {
			updates = append(updates, encodedUpdate{id: id, mask: mask, comp: cur.ComponentState})
		}
	}
	slices.SortFunc(updates, func(a, b encodedUpdate) int {
		if a.id < b.id {
			return -1
		}
		if a.id > b.id {
			return 1
		}
		return 0
	})

	buf := make([]byte, 0, 16+len(despawns)*2+len(updates)*16)
	buf = append(buf, replMsgSnapshotDelta)
	buf = binary.AppendUvarint(buf, uint64(tick))
	buf = binary.AppendUvarint(buf, uint64(baselineTick))
	buf = binary.AppendUvarint(buf, uint64(len(despawns)))
	for _, id := range despawns {
		buf = binary.AppendUvarint(buf, uint64(id))
	}
	buf = binary.AppendUvarint(buf, uint64(len(updates)))
	for _, u := range updates {
		buf = appendEntityFields(buf, u.id, u.mask, u.comp)
	}
	return buf
}

func encodeSnapshotAck(tick uint32) []byte {
	buf := make([]byte, 0, 8)
	buf = append(buf, replMsgSnapshotAck)
	buf = binary.AppendUvarint(buf, uint64(tick))
	return buf
}

type pendingPrediction struct {
	targetTick uint32
	input      any
	apply      func(*EntityState, any)
}

type predictedTracker struct {
	state          EntityState
	lastTargetTick uint32
	pending        []pendingPrediction
}

// ClientWorld receives authoritative snapshots from a ServerWorld, maintains a
// timed snapshot ring buffer for smooth interpolation, fires spawn/despawn hooks,
// and supports client-side prediction with automatic server reconciliation.
type ClientWorld struct {
	transport Transport
	serverID  PeerID

	// OnSpawn is invoked when a new entity first appears in an authoritative snapshot.
	OnSpawn func(EntityState)
	// OnDespawn is invoked when an entity disappears from an authoritative snapshot.
	OnDespawn func(EntityID)

	historyCap int
	snapshots  []worldSnapshot // ordered ascending by tick
	byTick     map[uint32]map[EntityID]EntityState
	hasLatest  bool
	latestTick uint32

	predicted          map[EntityID]*predictedTracker
	reconciliations    int
	lastReconcileError float32
	// lastResyncTick is the tick of the last resync request, to space them out.
	lastResyncTick uint32
	hasResync      bool
	// assembly collects the parts of a split full snapshot.
	assembly *snapshotAssembly
}

// snapshotAssembly is a full snapshot whose parts are still arriving.
type snapshotAssembly struct {
	tick     uint32
	got      []bool
	received int
	entities map[EntityID]EntityState
}

// NewClientWorld creates a replication client connected to serverID via transport.
func NewClientWorld(transport Transport, serverID PeerID) *ClientWorld {
	return &ClientWorld{
		transport:  transport,
		serverID:   serverID,
		historyCap: defaultSnapshotHistory,
		byTick:     make(map[uint32]map[EntityID]EntityState),
		predicted:  make(map[EntityID]*predictedTracker),
	}
}

// LatestTick returns the highest authoritative snapshot tick received so far.
func (c *ClientWorld) LatestTick() (uint32, bool) {
	return c.latestTick, c.hasLatest
}

// Reset forgets every snapshot, partial snapshot and prediction, as when the client joins a new match or the server restarts its tick count, and reports every entity the client knew through OnDespawn, in EntityID order. Snapshot ticks are compared in serial-number order, so without a Reset a client whose server went back to tick 0 drops every snapshot as stale until the new ticks pass the old ones. Reconciliations and LastReconcileError keep counting.
func (c *ClientWorld) Reset() {
	var known []EntityID
	if c.hasLatest {
		for id := range c.byTick[c.latestTick] {
			known = append(known, id)
		}
	}
	c.snapshots = nil
	c.byTick = make(map[uint32]map[EntityID]EntityState)
	c.hasLatest, c.latestTick = false, 0
	c.predicted = make(map[EntityID]*predictedTracker)
	c.lastResyncTick, c.hasResync = 0, false
	c.assembly = nil
	if c.OnDespawn != nil {
		slices.Sort(known)
		for _, id := range known {
			c.OnDespawn(id)
		}
	}
}

// Entity returns the latest authoritative state of id.
func (c *ClientWorld) Entity(id EntityID) (EntityState, bool) {
	if !c.hasLatest {
		return EntityState{}, false
	}
	latest := c.byTick[c.latestTick]
	e, ok := latest[id]
	if !ok {
		return EntityState{}, false
	}
	return e.Clone(), true
}

// Poll drains all pending snapshots from Transport, updates the ring buffer,
// triggers OnSpawn/OnDespawn callbacks, sends acknowledgments back to the server,
// and reconciles locally predicted entities. Returns the number of snapshots applied.
func (c *ClientWorld) Poll() int {
	applied := 0
	for {
		pkt, ok := c.transport.Recv()
		if !ok {
			break
		}
		if len(pkt.Payload) == 0 {
			continue
		}
		// Only the configured server is authoritative; anyone else cannot
		// inject snapshots.
		if pkt.From != c.serverID {
			continue
		}
		switch pkt.Payload[0] {
		case replMsgSnapshotFull:
			if c.applyFull(pkt.Payload[1:]) {
				applied++
			}
		case replMsgSnapshotDelta:
			if c.applyDelta(pkt.Payload[1:]) {
				applied++
			}
		case replMsgSnapshotFullPart:
			if c.applyFullPart(pkt.Payload[1:]) {
				applied++
			}
		}
	}
	return applied
}

// resyncIntervalTicks spaces out resync requests so a burst of undeliverable
// deltas sends only one request.
const resyncIntervalTicks = 30

// requestResync asks the server for a full snapshot, at most once every
// resyncIntervalTicks ticks.
func (c *ClientWorld) requestResync(tick uint32) {
	if since := tick - c.lastResyncTick; c.hasResync && int32(since) >= 0 && since < resyncIntervalTicks {
		return
	}
	c.lastResyncTick, c.hasResync = tick, true
	_ = c.transport.Send(c.serverID, ChannelUnreliable, []byte{replMsgResync})
}

// MaxEntitiesPerSnapshot bounds a full snapshot's entity count. The map is
// allocated before each entity is read, so a corrupt 3.8MB packet claiming a
// million entities would reserve hundreds of megabytes before failing (#546).
const MaxEntitiesPerSnapshot = 1 << 16

func (c *ClientWorld) applyFull(src []byte) bool {
	tickVal, n := binary.Uvarint(src)
	if n <= 0 {
		return false
	}
	src = src[n:]
	tick := uint32(tickVal)
	if c.hasLatest && !tickAfter(tick, c.latestTick) {
		return false
	}
	entities, ok := readFullEntities(src, MaxEntitiesPerSnapshot)
	if !ok {
		return false
	}
	c.commitSnapshot(tick, entities)
	return true
}

// readFullEntities parses a full snapshot's entity count and entities, refusing more than limit entities.
func readFullEntities(src []byte, limit int) (map[EntityID]EntityState, bool) {
	count, n := binary.Uvarint(src)
	if n <= 0 {
		return nil, false
	}
	src = src[n:]
	// Every entity needs at least one byte of id and one of mask, so a count beyond the payload is a corrupt packet, not a reason to allocate a huge map.
	if count > uint64(len(src)/2) || count > uint64(limit) {
		return nil, false
	}

	entities := make(map[EntityID]EntityState, count)
	var zero ComponentState
	for range count {
		id, comp, rest, ok := readEntityFields(src, zero)
		if !ok {
			return nil, false
		}
		src = rest
		entities[id] = EntityState{ID: id, ComponentState: comp}
	}
	return entities, true
}

// applyFullPart adds one part of a split full snapshot and applies the snapshot once every part has arrived. Parts of an older tick than the one being assembled are ignored, and a newer tick starts over, so a lost part costs one snapshot rather than stalling the client.
func (c *ClientWorld) applyFullPart(src []byte) bool {
	var hdr [3]uint64 // tick, index, total
	for i := range hdr {
		v, n := binary.Uvarint(src)
		if n <= 0 {
			return false
		}
		hdr[i] = v
		src = src[n:]
	}
	tick, index, total := uint32(hdr[0]), hdr[1], hdr[2]
	if total == 0 || total > maxSnapshotParts || index >= total || hdr[0] > math.MaxUint32 {
		return false
	}
	if c.hasLatest && !tickAfter(tick, c.latestTick) {
		return false
	}
	a := c.assembly
	if a != nil && a.tick != tick {
		if tickAfter(a.tick, tick) {
			return false
		}
		a = nil
	}
	if a == nil {
		a = &snapshotAssembly{tick: tick, got: make([]bool, total), entities: make(map[EntityID]EntityState)}
		c.assembly = a
	}
	if uint64(len(a.got)) != total {
		// Parts of one tick that disagree on the count are corrupt; drop the whole snapshot.
		c.assembly = nil
		return false
	}
	if a.got[index] {
		return false
	}
	part, ok := readFullEntities(src, MaxEntitiesPerSnapshot-len(a.entities))
	if !ok {
		return false
	}
	for id, e := range part {
		a.entities[id] = e
	}
	a.got[index] = true
	a.received++
	if a.received < len(a.got) {
		return false
	}
	c.assembly = nil
	c.commitSnapshot(tick, a.entities)
	return true
}

func (c *ClientWorld) applyDelta(src []byte) bool {
	tickVal, n := binary.Uvarint(src)
	if n <= 0 {
		return false
	}
	src = src[n:]
	tick := uint32(tickVal)
	if c.hasLatest && !tickAfter(tick, c.latestTick) {
		return false
	}

	baseTickVal, n := binary.Uvarint(src)
	if n <= 0 {
		return false
	}
	src = src[n:]
	baseTick := uint32(baseTickVal)

	base, ok := c.byTick[baseTick]
	if !ok {
		// Without the baseline this delta can never apply. Ask the server for a
		// full snapshot instead of dropping every later delta silently.
		c.requestResync(tick)
		return false
	}

	numDespawns, n := binary.Uvarint(src)
	if n <= 0 {
		return false
	}
	src = src[n:]
	if numDespawns > uint64(len(src)) {
		return false
	}

	despawns := make(map[EntityID]struct{}, numDespawns)
	for range numDespawns {
		idVal, m := binary.Uvarint(src)
		if m <= 0 {
			return false
		}
		src = src[m:]
		despawns[EntityID(idVal)] = struct{}{}
	}

	entities := make(map[EntityID]EntityState, len(base))
	for id, e := range base {
		if _, gone := despawns[id]; !gone {
			entities[id] = e.Clone()
		}
	}

	numUpdates, n := binary.Uvarint(src)
	if n <= 0 {
		return false
	}
	src = src[n:]

	var zero ComponentState
	for range numUpdates {
		// Peek entity ID first to locate baseline component state.
		idVal, m := binary.Uvarint(src)
		if m <= 0 {
			return false
		}
		id := EntityID(idVal)
		baseComp := zero
		if existing, exists := entities[id]; exists {
			baseComp = existing.ComponentState
		}
		parsedID, comp, rest, ok := readEntityFields(src, baseComp)
		if !ok {
			return false
		}
		src = rest
		entities[parsedID] = EntityState{ID: parsedID, ComponentState: comp}
	}

	c.commitSnapshot(tick, entities)
	return true
}

func (c *ClientWorld) commitSnapshot(tick uint32, entities map[EntityID]EntityState) {
	var prev map[EntityID]EntityState
	if c.hasLatest {
		prev = c.byTick[c.latestTick]
	}

	ext := int64(tick)
	if n := len(c.snapshots); n > 0 {
		last := c.snapshots[n-1]
		ext = last.ext + int64(int32(tick-last.tick))
	}
	c.snapshots = append(c.snapshots, worldSnapshot{tick: tick, ext: ext, entities: entities})
	c.byTick[tick] = entities
	if c.assembly != nil && !tickAfter(c.assembly.tick, tick) {
		c.assembly = nil
	}
	c.latestTick = tick
	c.hasLatest = true

	for len(c.snapshots) > c.historyCap {
		oldest := c.snapshots[0]
		c.snapshots = c.snapshots[1:]
		delete(c.byTick, oldest.tick)
	}

	// Fire OnSpawn / OnDespawn in deterministic EntityID order.
	if c.OnSpawn != nil {
		var spawned []EntityID
		for id := range entities {
			if _, existed := prev[id]; !existed {
				spawned = append(spawned, id)
			}
		}
		slices.Sort(spawned)
		for _, id := range spawned {
			c.OnSpawn(entities[id].Clone())
		}
	}
	if c.OnDespawn != nil && prev != nil {
		var despawned []EntityID
		for id := range prev {
			if _, exists := entities[id]; !exists {
				despawned = append(despawned, id)
			}
		}
		slices.Sort(despawned)
		for _, id := range despawned {
			c.OnDespawn(id)
		}
	}

	// Reconcile any locally predicted entities against the new authoritative state.
	c.reconcile(tick, entities)

	// Acknowledge receipt to the server so subsequent snapshots use delta compression.
	_ = c.transport.Send(c.serverID, ChannelUnreliable, encodeSnapshotAck(tick))
}

// Predict applies a local input immediately to localEntity and buffers it for
// reconciliation when authoritative snapshots arrive.
func (c *ClientWorld) Predict(localEntity EntityID, input any, apply func(*EntityState, any)) {
	if apply == nil {
		return
	}
	tr := c.predicted[localEntity]
	if tr == nil {
		var base EntityState
		if auth, ok := c.Entity(localEntity); ok {
			base = auth
		} else {
			base = EntityState{ID: localEntity}
		}
		tr = &predictedTracker{
			state:          base,
			lastTargetTick: c.latestTick,
		}
		c.predicted[localEntity] = tr
	}

	targetTick := tr.lastTargetTick + 1
	if c.hasLatest && !tickAfter(targetTick, c.latestTick) {
		targetTick = c.latestTick + 1
	}
	tr.lastTargetTick = targetTick
	tr.pending = append(tr.pending, pendingPrediction{
		targetTick: targetTick,
		input:      input,
		apply:      apply,
	})
	apply(&tr.state, input)
}

// PredictedState returns the locally predicted state for id if active,
// falling back to the latest authoritative state.
func (c *ClientWorld) PredictedState(id EntityID) (EntityState, bool) {
	if tr, ok := c.predicted[id]; ok {
		return tr.state.Clone(), true
	}
	return c.Entity(id)
}

// PendingPredictions returns the number of unacknowledged predicted inputs buffered for id.
func (c *ClientWorld) PendingPredictions(id EntityID) int {
	if tr, ok := c.predicted[id]; ok {
		return len(tr.pending)
	}
	return 0
}

// Reconciliations returns the total number of client-side prediction reconciliations performed.
func (c *ClientWorld) Reconciliations() int {
	return c.reconciliations
}

// LastReconcileError returns the Euclidean position delta between the predicted
// state and reconciled state from the most recent reconciliation.
func (c *ClientWorld) LastReconcileError() float32 {
	return c.lastReconcileError
}

func (c *ClientWorld) reconcile(authTick uint32, entities map[EntityID]EntityState) {
	for id, tr := range c.predicted {
		auth, exists := entities[id]
		if !exists {
			delete(c.predicted, id)
			continue
		}
		prevPosX, prevPosY := tr.state.X, tr.state.Y

		// Discard predicted inputs whose targetTick is already covered by authTick.
		kept := tr.pending[:0]
		for _, p := range tr.pending {
			if tickAfter(p.targetTick, authTick) {
				kept = append(kept, p)
			}
		}
		tr.pending = kept

		// Re-simulate remaining unacknowledged inputs from the authoritative baseline.
		reconciled := auth.Clone()
		for _, p := range tr.pending {
			p.apply(&reconciled, p.input)
		}
		dx := reconciled.X - prevPosX
		dy := reconciled.Y - prevPosY
		c.lastReconcileError = float32(math.Hypot(float64(dx), float64(dy)))
		tr.state = reconciled
		c.reconciliations++
	}
}

// Interpolate returns a sorted slice of entities interpolated at renderTick
// across the buffered authoritative snapshots (smoothly lerping X, Y, VX, VY,
// and shortest-arc Rotation).
//
// renderTick is read relative to the latest snapshot's tick, modulo 2^32, so a render time computed from uint32 ticks keeps working across a tick counter wraparound.
func (c *ClientWorld) Interpolate(renderTick float64) []EntityState {
	n := len(c.snapshots)
	if n == 0 {
		return nil
	}
	last := c.snapshots[n-1]
	// Place renderTick on the extended timeline: its distance from the latest tick, folded into [-2^31, 2^31].
	delta := math.Remainder(renderTick-float64(last.tick), 1<<32)
	if math.IsNaN(delta) {
		return snapshotToSortedSlice(last.entities)
	}
	at := float64(last.ext) + delta
	if at <= float64(c.snapshots[0].ext) {
		return snapshotToSortedSlice(c.snapshots[0].entities)
	}
	if at >= float64(last.ext) {
		return snapshotToSortedSlice(last.entities)
	}

	// Find bracketing snapshots s0 <= at <= s1.
	idx := 0
	for idx+1 < n && float64(c.snapshots[idx+1].ext) < at {
		idx++
	}
	s0 := c.snapshots[idx]
	s1 := c.snapshots[idx+1]
	if s0.ext == s1.ext {
		return snapshotToSortedSlice(s1.entities)
	}

	alpha := float32((at - float64(s0.ext)) / float64(s1.ext-s0.ext))

	ids := make([]EntityID, 0, len(s1.entities))
	for id := range s1.entities {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	out := make([]EntityState, 0, len(ids))
	for _, id := range ids {
		e1 := s1.entities[id]
		e0, existed := s0.entities[id]
		if !existed {
			out = append(out, e1.Clone())
			continue
		}
		interp := e1.Clone()
		if alpha < 0.5 {
			interp.Kind = e0.Kind
			interp.Flags = e0.Flags
			if len(e0.Props) > 0 {
				cp := make([]byte, len(e0.Props))
				copy(cp, e0.Props)
				interp.Props = cp
			} else {
				interp.Props = nil
			}
		}
		interp.X = lerp32(e0.X, e1.X, alpha)
		interp.Y = lerp32(e0.Y, e1.Y, alpha)
		interp.VX = lerp32(e0.VX, e1.VX, alpha)
		interp.VY = lerp32(e0.VY, e1.VY, alpha)
		interp.Rotation = lerpAngle32(e0.Rotation, e1.Rotation, alpha)
		out = append(out, interp)
	}
	return out
}

func snapshotToSortedSlice(entities map[EntityID]EntityState) []EntityState {
	ids := make([]EntityID, 0, len(entities))
	for id := range entities {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]EntityState, 0, len(ids))
	for _, id := range ids {
		out = append(out, entities[id].Clone())
	}
	return out
}

func lerp32(a, b, t float32) float32 {
	return a + (b-a)*t
}

func lerpAngle32(a, b, t float32) float32 {
	// Normalize in one step rather than by repeated subtraction, which would
	// spin almost forever on a huge or non-finite rotation.
	diff := math.Remainder(float64(b-a), 2*math.Pi)
	if math.IsNaN(diff) || math.IsInf(diff, 0) {
		return a
	}
	return a + float32(diff)*t
}

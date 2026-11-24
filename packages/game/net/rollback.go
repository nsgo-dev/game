package net

import (
	"cmp"
	"encoding/binary"
	"errors"
	"hash/fnv"
	"slices"
)

// ErrRollbackWindowExceeded is returned when a late remote input arrives for a
// tick older than the retained MaxRollback snapshot window.
var ErrRollbackWindowExceeded = errors.New("net: rollback window exceeded")

// InputFrame is a deterministic per-tick player input sample.
type InputFrame struct {
	Tick uint32
	Peer PeerID
	Bits uint64
	Axes [2]int16
}

func (f InputFrame) sameInput(other InputFrame) bool {
	return f.Bits == other.Bits && f.Axes == other.Axes
}

// SimState is the deterministic simulation interface driven by LockstepSession
// and RollbackSession.
type SimState interface {
	// Step advances the simulation by one fixed tick using inputs sorted by PeerID.
	Step(inputs []InputFrame)
	// Save serializes the complete deterministic simulation state.
	Save() []byte
	// Load restores the complete simulation state from a snapshot returned by Save.
	Load(snapshot []byte) error
	// Checksum returns a deterministic 64-bit hash of the current simulation state.
	Checksum() uint64
}

// DesyncEvent describes a detected state checksum divergence between peers.
type DesyncEvent struct {
	Tick           uint32
	LocalPeer      PeerID
	RemotePeer     PeerID
	LocalChecksum  uint64
	RemoteChecksum uint64
}

// ChecksumBytes computes a deterministic 64-bit FNV-1a checksum over data.
func ChecksumBytes(data []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(data)
	return h.Sum64()
}

const (
	syncMsgInputSingle byte = 10
	syncMsgInputBundle byte = 11
	syncMsgChecksum    byte = 12
)

const inputFrameWireSize = 20 // tick(4) + peer(4) + bits(8) + axes[0](2) + axes[1](2)

func appendInputFrame(dst []byte, in InputFrame) []byte {
	dst = binary.BigEndian.AppendUint32(dst, in.Tick)
	dst = binary.BigEndian.AppendUint32(dst, uint32(in.Peer))
	dst = binary.BigEndian.AppendUint64(dst, in.Bits)
	dst = binary.BigEndian.AppendUint16(dst, uint16(in.Axes[0]))
	dst = binary.BigEndian.AppendUint16(dst, uint16(in.Axes[1]))
	return dst
}

func decodeInputFrame(src []byte) (InputFrame, bool) {
	if len(src) < inputFrameWireSize {
		return InputFrame{}, false
	}
	return InputFrame{
		Tick: binary.BigEndian.Uint32(src[0:4]),
		Peer: PeerID(binary.BigEndian.Uint32(src[4:8])),
		Bits: binary.BigEndian.Uint64(src[8:16]),
		Axes: [2]int16{
			int16(binary.BigEndian.Uint16(src[16:18])),
			int16(binary.BigEndian.Uint16(src[18:20])),
		},
	}, true
}

func encodeSingleInput(in InputFrame) []byte {
	buf := make([]byte, 0, 1+inputFrameWireSize)
	buf = append(buf, syncMsgInputSingle)
	return appendInputFrame(buf, in)
}

func encodeInputBundle(inputs []InputFrame) []byte {
	buf := make([]byte, 0, 2+len(inputs)*inputFrameWireSize)
	buf = append(buf, syncMsgInputBundle, byte(len(inputs)))
	for _, in := range inputs {
		buf = appendInputFrame(buf, in)
	}
	return buf
}

func encodeChecksumMsg(tick uint32, peer PeerID, sum uint64) []byte {
	buf := make([]byte, 1+4+4+8)
	buf[0] = syncMsgChecksum
	binary.BigEndian.PutUint32(buf[1:5], tick)
	binary.BigEndian.PutUint32(buf[5:9], uint32(peer))
	binary.BigEndian.PutUint64(buf[9:17], sum)
	return buf
}

func decodeChecksumMsg(src []byte) (tick uint32, peer PeerID, sum uint64, ok bool) {
	if len(src) < 16 {
		return 0, 0, 0, false
	}
	return binary.BigEndian.Uint32(src[0:4]),
		PeerID(binary.BigEndian.Uint32(src[4:8])),
		binary.BigEndian.Uint64(src[8:16]),
		true
}

func normalizePeers(transport Transport, peers []PeerID) []PeerID {
	seen := make(map[PeerID]struct{})
	out := make([]PeerID, 0, len(peers)+1)
	add := func(id PeerID) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	if len(peers) == 0 && transport != nil {
		add(transport.LocalID())
		for _, p := range transport.Peers() {
			add(p)
		}
	} else {
		for _, p := range peers {
			add(p)
		}
		if transport != nil {
			add(transport.LocalID())
		}
	}
	slices.Sort(out)
	return out
}

type desyncKey struct {
	tick   uint32
	remote PeerID
}

// LockstepConfig configures a strict deterministic LockstepSession.
type LockstepConfig struct {
	Transport        Transport
	Sim              SimState
	Peers            []PeerID
	ChecksumInterval int
	OnDesync         func(DesyncEvent)
	// Host is the peer that admits and removes peers while the match runs, with AddPeer and RemovePeer. Zero means the lowest ID of Peers, leaving out the local peer when Join is set; a joining session that finds no other peer there, or that names itself, takes the lowest other ID the transport reports on each Poll until its join state arrives. Set it explicitly when joining over a relay, whose peer list may still be empty or include other newcomers. Every peer of a match must name the same host, and the host stays for the whole match.
	Host PeerID
	// Join starts the session as a peer joining a match already running. It takes no part until the host's join state arrives with the tick it joins at, the peers and the simulation's state, which it loads into Sim; until then Step does nothing and Active reports false. Peers then only serves to find the host when Host is zero.
	Join bool
}

// LockstepSession advances a deterministic simulation tick only once every
// participating peer's input for that tick has arrived, exchanging periodic
// state checksums to detect desyncs.
//
// Peers may join and leave while the match runs: the host admits one with AddPeer and removes one with RemovePeer, and every peer applies the change at the same tick.
type LockstepSession struct {
	match
	sim              SimState
	checksumInterval uint32
	onDesync         func(DesyncEvent)

	tick            uint32
	nextInput       uint32 // one past the latest tick of a local input sent
	inputs          map[uint32]map[PeerID]InputFrame
	history         map[uint32]map[PeerID]InputFrame // inputs of the ticks run, kept for lockstepChecksumHistory ticks
	localChecksums  map[uint32]uint64
	remoteChecksums map[uint32]map[PeerID]uint64
	reportedDesync  map[desyncKey]struct{}
	desyncs         []DesyncEvent
}

// NewLockstepSession creates a strict lockstep session.
func NewLockstepSession(cfg LockstepConfig) *LockstepSession {
	var interval uint32
	if cfg.ChecksumInterval > 0 {
		interval = uint32(cfg.ChecksumInterval)
	}
	return &LockstepSession{
		match:            newMatch(cfg.Transport, normalizePeers(cfg.Transport, cfg.Peers), cfg.Host, cfg.Join),
		sim:              cfg.Sim,
		checksumInterval: interval,
		onDesync:         cfg.OnDesync,
		inputs:           make(map[uint32]map[PeerID]InputFrame),
		history:          make(map[uint32]map[PeerID]InputFrame),
		localChecksums:   make(map[uint32]uint64),
		remoteChecksums:  make(map[uint32]map[PeerID]uint64),
		reportedDesync:   make(map[desyncKey]struct{}),
	}
}

// Tick returns the number of ticks simulated so far (also the next tick to execute). A joining peer's count starts at its join tick.
func (s *LockstepSession) Tick() uint32 {
	return s.tick
}

// Members returns the sorted IDs of the peers taking part in the current tick; it is empty while a joining peer waits for its join state.
func (s *LockstepSession) Members() []PeerID {
	return s.activeIDs(s.tick)
}

// Active reports whether the local peer takes part in the current tick: false while a joining peer waits for its join state, and from the tick the host removed the local peer at.
func (s *LockstepSession) Active() bool {
	return s.joined && s.activeAt(s.localID, s.tick)
}

// Desyncs returns a copy of all DesyncEvents observed so far.
func (s *LockstepSession) Desyncs() []DesyncEvent {
	out := make([]DesyncEvent, len(s.desyncs))
	copy(out, s.desyncs)
	return out
}

// SubmitInput records the local peer's input for in.Tick and sends it to every other peer taking part in that tick. An input for a tick already run or more than 256 ticks ahead, or for a tick the local peer takes no part in, is ignored, as is any input once the host has removed the local peer: the host's leave change carries the inputs every peer uses. A send the transport cannot make right now, to a peer that is reconnecting or that a relay has not announced yet, is kept and retried by Poll; once too many wait for one peer, SubmitInput returns ErrSessionBacklog.
func (s *LockstepSession) SubmitInput(in InputFrame) error {
	in.Peer = s.localID
	if !s.joined || !s.accepts(in) || in.Tick < s.tick || in.Tick > s.tick+maxLockstepLeadTicks {
		return nil
	}
	s.recordInput(in)
	s.nextInput = max(s.nextInput, in.Tick+1)
	payload := encodeSingleInput(in)
	var firstErr error
	for _, mm := range s.members {
		if mm.id == s.localID || !mm.activeAt(in.Tick) {
			continue
		}
		if err := s.send(mm.id, payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return errors.Join(firstErr, s.takeBacklog())
}

// AddPeer admits a peer to the running match and returns the tick it joins at. Only the host may call it, typically when its transport reports the peer (PeerJoined, or a new member in a RelayTransport's Peers). The tick is the host's next own input tick, so no peer has run it yet; the other peers learn of the change before the host's input for that tick, and inputs before it are not expected from the new peer. Once the host's simulation stands at the start of the join tick, Step sends the new peer the state, which must be no larger than MaxJoinStateSize, the tick and the peers.
//
// Adding a peer that already takes part returns its join tick. It returns ErrNotHost on any other peer, ErrPeerRejoin for a peer that has left, ErrUnknownPeer for ID 0 and ErrPayloadTooLarge when the state is larger than MaxJoinStateSize.
func (s *LockstepSession) AddPeer(id PeerID) (uint32, error) {
	if tick, done, err := s.checkAdd(id); err != nil || done {
		return tick, err
	}
	if len(s.sim.Save()) > MaxJoinStateSize {
		return 0, ErrPayloadTooLarge
	}
	c := memberChange{kind: memberJoin, peer: id, tick: max(s.tick, s.nextInput)}
	s.applyChange(c)
	s.announce(c, s.tick)
	s.pending = append(s.pending, pendingJoin{peer: id, tick: c.tick})
	s.sendDueJoinStates()
	return c.tick, nil
}

// RemovePeer removes a peer from the running match and returns the tick from which no peer waits for it. Only the host may call it, typically once its transport reports the peer gone (PeerLeft, or a RelayTransport's Peers no longer listing it). The tick is the host's next own input tick or the peer's join tick, whichever is later. The change carries the inputs the host holds from the peer for the ticks before it and a zero input for each tick the host never received, so every peer runs those ticks alike; a peer still sending when removed may have reached another peer with an input the host never saw, so remove a peer after it has stopped sending.
//
// Removing a peer that is already leaving, or that left, returns its leave tick. It returns ErrNotHost on any other peer, ErrHostLeave for the host and ErrUnknownPeer for a peer not in the match.
func (s *LockstepSession) RemovePeer(id PeerID) (uint32, error) {
	mm, tick, done, err := s.checkRemove(id)
	if err != nil || done {
		return tick, err
	}
	leave := max(s.tick, s.nextInput, mm.from)
	first := max(mm.from, s.tick-min(s.tick, lockstepChecksumHistory))
	c := memberChange{kind: memberLeave, peer: id, tick: leave, inputs: leaveInputs(id, first, s.tick, leave, func(t uint32) (InputFrame, bool) {
		if in, ok := s.inputs[t][id]; ok {
			return in, true
		}
		in, ok := s.history[t][id]
		return in, ok
	})}
	s.applyChange(c)
	s.announce(c, s.tick)
	return leave, nil
}

// Poll drains all pending input, checksum and membership messages from the Transport, first sending again the messages kept for peers the transport could not reach.
func (s *LockstepSession) Poll() {
	s.resolveHost()
	s.flush()
	for {
		pkt, ok := s.transport.Recv()
		if !ok {
			return
		}
		if len(pkt.Payload) == 0 {
			continue
		}
		switch pkt.Payload[0] {
		case syncMsgInputSingle:
			if in, valid := decodeInputFrame(pkt.Payload[1:]); valid && pkt.From == in.Peer {
				s.receiveInput(in)
			}
		case syncMsgInputBundle:
			if len(pkt.Payload) < 2 {
				continue
			}
			count := int(pkt.Payload[1])
			src := pkt.Payload[2:]
			for range count {
				in, valid := decodeInputFrame(src)
				if !valid {
					break
				}
				src = src[inputFrameWireSize:]
				if pkt.From == in.Peer {
					s.receiveInput(in)
				}
			}
		case syncMsgChecksum:
			if tick, peer, sum, valid := decodeChecksumMsg(pkt.Payload[1:]); valid && pkt.From == peer && s.member(peer) != nil {
				s.recordRemoteChecksum(tick, peer, sum)
			}
		case syncMsgMembership:
			if c, valid := decodeMemberChange(pkt.Payload); valid && s.joined && pkt.From == s.host {
				s.applyChange(c)
			}
		case syncMsgJoinState:
			if !s.joined && pkt.From == s.host {
				if blob, done := s.assembly.add(pkt.Payload); done {
					s.join(blob)
				}
			}
		}
	}
}

// Step polls incoming packets and advances the simulation by one tick if every
// peer's input for the current tick is present. Returns true when a tick executed.
func (s *LockstepSession) Step() bool {
	s.Poll()
	if !s.Active() {
		return false
	}
	ids := s.activeIDs(s.tick)
	tickInputs := s.inputs[s.tick]
	ordered := make([]InputFrame, 0, len(ids))
	for _, id := range ids {
		in, ok := tickInputs[id]
		if !ok {
			return false
		}
		ordered = append(ordered, in)
	}

	executedTick := s.tick
	s.sim.Step(ordered)
	s.history[executedTick] = tickInputs
	delete(s.inputs, executedTick)
	s.tick++
	s.pruneChecksums(executedTick)

	if s.checksumInterval > 0 && (executedTick+1)%s.checksumInterval == 0 {
		sum := s.sim.Checksum()
		s.localChecksums[executedTick] = sum
		msg := encodeChecksumMsg(executedTick, s.localID, sum)
		for _, mm := range s.members {
			if mm.id != s.localID && mm.activeAt(executedTick) {
				_ = s.send(mm.id, msg)
			}
		}
		s.checkDesyncAt(executedTick)
	}
	s.sendDueJoinStates()
	s.pruneMembers(s.tick)
	return true
}

// receiveInput takes a remote peer's own input: kept aside while the peer is not a member yet, recorded when the session accepts it.
func (s *LockstepSession) receiveInput(in InputFrame) {
	switch {
	case in.Peer == s.localID:
	case s.member(in.Peer) == nil:
		s.stray(in)
	case s.accepts(in):
		s.recordInput(in)
	}
}

// applyChange applies a membership change, the host's own or one it announced.
func (s *LockstepSession) applyChange(c memberChange) {
	switch c.kind {
	case memberJoin:
		if s.member(c.peer) != nil {
			return
		}
		s.addMember(c.peer, c.tick)
		for _, in := range s.takeStrays(c.peer) {
			if s.accepts(in) {
				s.recordInput(in)
			}
		}
		// Local inputs sent before this peer was known never reached it.
		var own []InputFrame
		for t, m := range s.inputs {
			if in, ok := m[s.localID]; ok && t >= c.tick {
				own = append(own, in)
			}
		}
		slices.SortFunc(own, func(a, b InputFrame) int { return cmp.Compare(a.Tick, b.Tick) })
		if c.peer != s.localID {
			s.sendInputs(c.peer, own)
		}
	case memberLeave:
		mm := s.member(c.peer)
		if mm == nil || mm.until != noLeave || c.tick < mm.from {
			return
		}
		mm.until = c.tick
		for t, m := range s.inputs {
			if t >= c.tick {
				delete(m, c.peer)
			}
		}
		// The host's view of the leaving peer's inputs is final. Ticks more than 256 ahead are kept too: a peer may trail the host and still need them.
		for _, in := range c.inputs {
			if in.Tick >= mm.from && in.Tick >= s.tick {
				s.storeInput(in)
			}
		}
	}
}

// join loads the host's join state and starts taking part at its tick.
func (s *LockstepSession) join(blob []byte) {
	js, ok := decodeJoinState(blob)
	if !ok || !s.acceptJoinState(js) || s.sim.Load(js.state) != nil {
		return
	}
	s.members = js.members
	s.joined = true
	s.start = js.tick
	s.tick = js.tick
	s.nextInput = js.tick
	clear(s.inputs)
	// The inputs of members already leaving come from the host's declaration, which the changes announcing their leave carried before this peer joined.
	for _, in := range js.inputs {
		s.storeInput(in)
	}
	for _, mm := range s.members {
		for _, in := range s.takeStrays(mm.id) {
			if s.accepts(in) && in.Tick <= s.tick+maxLockstepLeadTicks {
				s.recordInput(in)
			}
		}
	}
}

// sendDueJoinStates sends each pending join its state once the host's simulation stands at the start of its join tick. A state grown past MaxJoinStateSize turns the join into a leave.
func (s *LockstepSession) sendDueJoinStates() {
	for _, p := range s.takePending(func(p pendingJoin) bool { return p.tick <= s.tick }) {
		if mm := s.member(p.peer); mm == nil || mm.until <= p.tick {
			continue
		}
		js, ok := s.joinStateFor(p, s.sim.Save(), func(id PeerID, t uint32) (InputFrame, bool) {
			if in, ok := s.inputs[t][id]; ok {
				return in, true
			}
			in, ok := s.history[t][id]
			return in, ok
		})
		if !ok {
			_, _ = s.RemovePeer(p.peer)
			continue
		}
		s.sendJoinState(p.peer, js)
	}
}

// maxLockstepLeadTicks bounds how far ahead of the current tick an input may be
// recorded, so a peer cannot grow the input map without bound.
const maxLockstepLeadTicks = 256

// lockstepChecksumHistory is how many ticks of checksums a lockstep session keeps
// for desync reports.
const lockstepChecksumHistory = 256

func (s *LockstepSession) recordInput(in InputFrame) {
	if in.Tick < s.tick || in.Tick > s.tick+maxLockstepLeadTicks {
		return
	}
	s.storeInput(in)
}

func (s *LockstepSession) storeInput(in InputFrame) {
	m := s.inputs[in.Tick]
	if m == nil {
		m = make(map[PeerID]InputFrame)
		s.inputs[in.Tick] = m
	}
	m[in.Peer] = in
}

// pruneChecksums drops checksum, desync and input history older than the retention window so a long session does not accumulate it forever.
func (s *LockstepSession) pruneChecksums(executedTick uint32) {
	if executedTick < lockstepChecksumHistory {
		return
	}
	floor := executedTick - lockstepChecksumHistory
	for t := range s.localChecksums {
		if t < floor {
			delete(s.localChecksums, t)
		}
	}
	for t := range s.remoteChecksums {
		if t < floor {
			delete(s.remoteChecksums, t)
		}
	}
	for k := range s.reportedDesync {
		if k.tick < floor {
			delete(s.reportedDesync, k)
		}
	}
	for t := range s.history {
		if t < floor {
			delete(s.history, t)
		}
	}
}

func (s *LockstepSession) recordRemoteChecksum(tick uint32, peer PeerID, sum uint64) {
	if peer == s.localID || s.member(peer) == nil {
		return
	}
	m := s.remoteChecksums[tick]
	if m == nil {
		m = make(map[PeerID]uint64)
		s.remoteChecksums[tick] = m
	}
	m[peer] = sum
	s.checkDesyncAt(tick)
}

func (s *LockstepSession) checkDesyncAt(tick uint32) {
	localSum, ok := s.localChecksums[tick]
	if !ok {
		return
	}
	remotes := s.remoteChecksums[tick]
	for peer, remoteSum := range remotes {
		if remoteSum == localSum {
			continue
		}
		key := desyncKey{tick: tick, remote: peer}
		if _, already := s.reportedDesync[key]; already {
			continue
		}
		s.reportedDesync[key] = struct{}{}
		ev := DesyncEvent{
			Tick:           tick,
			LocalPeer:      s.localID,
			RemotePeer:     peer,
			LocalChecksum:  localSum,
			RemoteChecksum: remoteSum,
		}
		s.desyncs = append(s.desyncs, ev)
		if s.onDesync != nil {
			s.onDesync(ev)
		}
	}
}

// RollbackConfig configures a GGPO-style RollbackSession.
type RollbackConfig struct {
	Transport        Transport
	Sim              SimState
	Peers            []PeerID
	MaxRollback      int
	InputDelay       int
	ChecksumInterval int
	OnDesync         func(DesyncEvent)
	// Host is the peer that admits and removes peers while the match runs, with AddPeer and RemovePeer. Zero means the lowest ID of Peers, leaving out the local peer when Join is set; a joining session that finds no other peer there, or that names itself, takes the lowest other ID the transport reports on each Poll until its join state arrives. Set it explicitly when joining over a relay, whose peer list may still be empty or include other newcomers. Every peer of a match must name the same host, and the host stays for the whole match.
	Host PeerID
	// Join starts the session as a peer joining a match already running. It takes no part until the host's join state arrives with the tick it joins at, the peers and the simulation's state, which it loads into Sim; until then Step only polls and Active reports false. Peers then only serves to find the host when Host is zero.
	Join bool
}

// RollbackStats tracks runtime rollback and prediction metrics.
type RollbackStats struct {
	Rollbacks         int
	MaxRollbackDepth  int
	ResimulatedFrames int
	Predictions       int
	Mispredictions    int
	Desyncs           int
}

// RollbackSession runs a GGPO-style optimistic deterministic simulation:
// missing remote inputs are predicted by repeating the last confirmed input,
// pre-step snapshots are kept in a ring window, late divergent inputs trigger
// rollback + deterministic re-simulation, and confirmed ticks exchange periodic
// state checksums for immediate desync detection.
//
// Peers may join and leave while the match runs: the host admits one with AddPeer and removes one with RemovePeer, and every peer applies the change at the same tick, rolling back when it has already simulated past it.
type RollbackSession struct {
	match
	sim              SimState
	maxRollback      int
	inputDelay       int
	checksumInterval uint32
	onDesync         func(DesyncEvent)

	currentTick   uint32
	confirmedTick int64 // highest tick with all peers' confirmed inputs and already simulated (-1 = none)

	confirmedInputs map[uint32]map[PeerID]InputFrame
	usedInputs      map[uint32]map[PeerID]InputFrame
	localHistory    []InputFrame
	snapshots       map[uint32][]byte

	computedChecksums map[uint32]uint64
	remoteChecksums   map[uint32]map[PeerID]uint64
	sentChecksum      map[uint32]bool
	reportedDesync    map[desyncKey]struct{}
	desyncs           []DesyncEvent
	stats             RollbackStats
}

// NewRollbackSession creates a GGPO-style rollback session.
func NewRollbackSession(cfg RollbackConfig) *RollbackSession {
	maxRB := cfg.MaxRollback
	if maxRB <= 0 {
		maxRB = 8
	}
	delay := cfg.InputDelay
	if delay < 0 {
		delay = 0
	}
	var interval uint32
	if cfg.ChecksumInterval > 0 {
		interval = uint32(cfg.ChecksumInterval)
	}

	s := &RollbackSession{
		match:             newMatch(cfg.Transport, normalizePeers(cfg.Transport, cfg.Peers), cfg.Host, cfg.Join),
		sim:               cfg.Sim,
		maxRollback:       maxRB,
		inputDelay:        delay,
		checksumInterval:  interval,
		onDesync:          cfg.OnDesync,
		confirmedTick:     -1,
		confirmedInputs:   make(map[uint32]map[PeerID]InputFrame),
		usedInputs:        make(map[uint32]map[PeerID]InputFrame),
		snapshots:         make(map[uint32][]byte),
		computedChecksums: make(map[uint32]uint64),
		remoteChecksums:   make(map[uint32]map[PeerID]uint64),
		sentChecksum:      make(map[uint32]bool),
		reportedDesync:    make(map[desyncKey]struct{}),
	}
	for _, mm := range s.members {
		s.preconfirm(mm)
	}
	return s
}

// preconfirm confirms zero inputs for a member's first InputDelay ticks, which its first input, applied InputDelay ticks late, never covers.
func (s *RollbackSession) preconfirm(mm matchMember) {
	for i := range uint32(s.inputDelay) {
		t := mm.from + i
		if t < mm.from || !mm.activeAt(t) {
			return
		}
		s.storeConfirmed(InputFrame{Tick: t, Peer: mm.id})
	}
}

// Tick returns the current simulation tick (number of ticks executed so far). A joining peer's count starts at its join tick.
func (s *RollbackSession) Tick() uint32 {
	return s.currentTick
}

// ConfirmedTick returns the highest tick for which all peers' inputs are confirmed,
// or -1 if no tick is fully confirmed yet.
func (s *RollbackSession) ConfirmedTick() int64 {
	return s.confirmedTick
}

// Members returns the sorted IDs of the peers taking part in the current tick; it is empty while a joining peer waits for its join state.
func (s *RollbackSession) Members() []PeerID {
	return s.activeIDs(s.currentTick)
}

// Active reports whether the local peer takes part in the current tick: false while a joining peer waits for its join state, and from the tick the host removed the local peer at.
func (s *RollbackSession) Active() bool {
	return s.joined && s.activeAt(s.localID, s.currentTick)
}

// Stats returns a snapshot of the rollback session's metrics.
func (s *RollbackSession) Stats() RollbackStats {
	return s.stats
}

// Desyncs returns a copy of all detected DesyncEvents.
func (s *RollbackSession) Desyncs() []DesyncEvent {
	out := make([]DesyncEvent, len(s.desyncs))
	copy(out, s.desyncs)
	return out
}

// AddPeer admits a peer to the running match and returns the tick it joins at. Only the host may call it, typically when its transport reports the peer (PeerJoined, or a new member in a RelayTransport's Peers). The tick is the one the host's next local input applies to (Tick plus InputDelay), so no peer has confirmed it yet; a peer that has already simulated past it rolls back. The new peer's inputs for its first InputDelay ticks are zero on every peer. Once the host has confirmed every tick before the join tick, it sends the new peer the state at the start of that tick, which must be no larger than MaxJoinStateSize, the tick and the peers.
//
// Adding a peer that already takes part returns its join tick. It returns ErrNotHost on any other peer, ErrPeerRejoin for a peer that has left, ErrUnknownPeer for ID 0 and ErrPayloadTooLarge when the state is larger than MaxJoinStateSize.
func (s *RollbackSession) AddPeer(id PeerID) (uint32, error) {
	if tick, done, err := s.checkAdd(id); err != nil || done {
		return tick, err
	}
	if len(s.sim.Save()) > MaxJoinStateSize {
		return 0, ErrPayloadTooLarge
	}
	c := memberChange{kind: memberJoin, peer: id, tick: s.currentTick + uint32(s.inputDelay)}
	s.applyChange(c)
	s.announce(c, s.currentTick)
	s.pending = append(s.pending, pendingJoin{peer: id, tick: c.tick})
	return c.tick, s.sendDueJoinStates()
}

// RemovePeer removes a peer from the running match and returns the tick from which no peer waits for or predicts it. Only the host may call it, typically once its transport reports the peer gone (PeerLeft, or a RelayTransport's Peers no longer listing it). The tick is the one the host's next local input applies to, or the peer's join tick, whichever is later. The change carries the inputs the host holds from the peer and a zero input for each unconfirmed tick the host never received, so every peer confirms those ticks alike, rolling back where it predicted otherwise; a peer still sending when removed may have reached another peer with an input the host never saw, so remove a peer after it has stopped sending.
//
// Removing a peer that is already leaving, or that left, returns its leave tick. It returns ErrNotHost on any other peer, ErrHostLeave for the host, ErrUnknownPeer for a peer not in the match, and ErrRollbackWindowExceeded when the host would have to roll back further than MaxRollback or the change would carry more inputs than fit one message.
func (s *RollbackSession) RemovePeer(id PeerID) (uint32, error) {
	mm, tick, done, err := s.checkRemove(id)
	if err != nil || done {
		return tick, err
	}
	return s.removeAt(mm, max(s.currentTick+uint32(s.inputDelay), mm.from))
}

// dropJoin removes a joining peer whose join state cannot be sent, so no peer waits for or predicts it. Its leave tick is the one RemovePeer picks when the change fits one message. Otherwise it is the first tick after the peer's preconfirmed zero inputs, or the first tick the host has not confirmed if that is later: the peer never sent an input, so no peer can have confirmed a tick from there on, and the change carries at most InputDelay inputs.
func (s *RollbackSession) dropJoin(p pendingJoin) error {
	mm := s.member(p.peer)
	leave := max(s.currentTick+uint32(s.inputDelay), mm.from)
	if declareFrom := uint32(s.confirmedTick + 1); leave-declareFrom > maxLeaveInputs {
		leave = max(mm.from+uint32(s.inputDelay), declareFrom)
	}
	_, err := s.removeAt(mm, leave)
	return err
}

// removeAt announces that mm leaves at tick leave, which no peer can have confirmed yet, with the inputs the host holds from it and a zero input for each unconfirmed tick it never received.
func (s *RollbackSession) removeAt(mm *matchMember, leave uint32) (uint32, error) {
	id := mm.id
	declareFrom := uint32(s.confirmedTick + 1)
	if leave > declareFrom && leave-declareFrom > maxLeaveInputs {
		return 0, ErrRollbackWindowExceeded
	}
	first := declareFrom
	for t := range s.confirmedInputs {
		first = min(first, t)
	}
	first = max(first, mm.from)
	inputs := leaveInputs(id, first, declareFrom, leave, func(t uint32) (InputFrame, bool) {
		in, ok := s.confirmedInputs[t][id]
		return in, ok
	})
	if len(inputs) > maxLeaveInputs {
		// Drop the oldest held inputs, which only a peer far behind could still need; every unconfirmed tick fits.
		inputs = inputs[len(inputs)-maxLeaveInputs:]
	}
	c := memberChange{kind: memberLeave, peer: id, tick: leave, inputs: inputs}
	mispredicted, at := s.applyChange(c)
	s.announce(c, s.currentTick)
	if mispredicted {
		if err := s.rollbackTo(at); err != nil {
			return leave, err
		}
	}
	return leave, nil
}

// Step submits the local input (applied at currentTick + InputDelay), drains incoming packets (executing any needed rollbacks), simulates the next tick, and exchanges checksums for newly confirmed ticks. While the local peer takes no part (see Active) it only polls. It returns ErrSessionBacklog, after simulating the tick, when a remote peer has become unreachable for too long (see Poll).
func (s *RollbackSession) Step(localInput InputFrame) error {
	if !s.Active() {
		return s.Poll()
	}
	targetTick := s.currentTick + uint32(s.inputDelay)
	localInput.Peer = s.localID
	localInput.Tick = targetTick

	if s.accepts(localInput) {
		s.storeConfirmed(localInput)
		s.broadcastLocalInput(localInput)
	}

	if err := s.poll(); err != nil {
		return err
	}
	if !s.Active() {
		return s.takeBacklog()
	}

	// Save pre-step snapshot before simulating currentTick.
	snap := s.sim.Save()
	snapCopy := make([]byte, len(snap))
	copy(snapCopy, snap)
	s.snapshots[s.currentTick] = snapCopy

	// Build deterministic peer-sorted inputs, predicting any missing remote inputs.
	inputs, used := s.buildTickInputs(s.currentTick, true)
	s.usedInputs[s.currentTick] = used

	s.sim.Step(inputs)
	s.computedChecksums[s.currentTick] = s.sim.Checksum()
	s.currentTick++

	s.advanceConfirmedAndChecksums()
	if err := s.sendDueJoinStates(); err != nil {
		return err
	}
	s.pruneHistory()
	s.pruneMembers(s.currentTick)
	return s.takeBacklog()
}

// Poll drains incoming remote inputs, checksums and membership messages from the Transport, rolling back and re-simulating if any late remote input or membership change diverges from what was previously simulated. It first sends again the messages kept for peers the transport could not reach, and returns ErrSessionBacklog once for a peer that more such messages wait for than a session keeps: that peer misses the messages sent to it from then on.
func (s *RollbackSession) Poll() error {
	return errors.Join(s.poll(), s.takeBacklog())
}

func (s *RollbackSession) poll() error {
	s.resolveHost()
	s.flush()
	earliestMispredict := uint32(0)
	hasMispredict := false
	mark := func(mis bool, tick uint32) {
		if mis && (!hasMispredict || tick < earliestMispredict) {
			earliestMispredict = tick
			hasMispredict = true
		}
	}

	for {
		pkt, ok := s.transport.Recv()
		if !ok {
			break
		}
		if len(pkt.Payload) == 0 {
			continue
		}
		switch pkt.Payload[0] {
		case syncMsgInputSingle:
			if in, valid := decodeInputFrame(pkt.Payload[1:]); valid && pkt.From == in.Peer {
				mark(s.receiveInput(in))
			}
		case syncMsgInputBundle:
			if len(pkt.Payload) < 2 {
				continue
			}
			count := int(pkt.Payload[1])
			src := pkt.Payload[2:]
			for range count {
				in, valid := decodeInputFrame(src)
				if !valid {
					break
				}
				src = src[inputFrameWireSize:]
				if pkt.From == in.Peer {
					mark(s.receiveInput(in))
				}
			}
		case syncMsgChecksum:
			if tick, peer, sum, valid := decodeChecksumMsg(pkt.Payload[1:]); valid && pkt.From == peer && s.member(peer) != nil {
				s.recordRemoteChecksum(tick, peer, sum)
			}
		case syncMsgMembership:
			if c, valid := decodeMemberChange(pkt.Payload); valid && s.joined && pkt.From == s.host {
				mark(s.applyChange(c))
			}
		case syncMsgJoinState:
			if !s.joined && pkt.From == s.host {
				if blob, done := s.assembly.add(pkt.Payload); done {
					s.join(blob)
				}
			}
		}
	}

	if hasMispredict {
		if err := s.rollbackTo(earliestMispredict); err != nil {
			return err
		}
	}

	s.advanceConfirmedAndChecksums()
	if err := s.sendDueJoinStates(); err != nil {
		return err
	}
	s.pruneHistory()
	s.pruneMembers(s.currentTick)
	return nil
}

func (s *RollbackSession) broadcastLocalInput(in InputFrame) {
	s.localHistory = append(s.localHistory, in)
	redundantMax := min(8, max(1, s.maxRollback))
	if len(s.localHistory) > redundantMax {
		s.localHistory = s.localHistory[len(s.localHistory)-redundantMax:]
	}
	payload := encodeInputBundle(s.localHistory)
	first, last := s.localHistory[0].Tick, s.localHistory[len(s.localHistory)-1].Tick
	for _, mm := range s.members {
		if mm.id != s.localID && last >= mm.from && first < mm.until {
			_ = s.send(mm.id, payload)
		}
	}
}

func (s *RollbackSession) storeConfirmed(in InputFrame) {
	m := s.confirmedInputs[in.Tick]
	if m == nil {
		m = make(map[PeerID]InputFrame)
		s.confirmedInputs[in.Tick] = m
	}
	m[in.Peer] = in
}

// maxRollbackLeadTicks bounds how far ahead of the current tick a remote input
// may be stored, so a peer cannot grow the future-input maps without limit.
const maxRollbackLeadTicks = 512

// receiveInput takes a remote peer's own input: kept aside while the peer is not a member yet, confirmed when the session accepts it. It reports a tick already simulated with a different input.
func (s *RollbackSession) receiveInput(in InputFrame) (mispredicted bool, tick uint32) {
	switch {
	case in.Peer == s.localID:
		return false, 0
	case s.member(in.Peer) == nil:
		s.stray(in)
		return false, 0
	case !s.accepts(in) || in.Tick > s.currentTick+maxRollbackLeadTicks:
		return false, 0
	}
	return s.confirmInput(in)
}

// confirmInput stores in as confirmed and reports whether a tick already simulated used a different input for its peer, or none.
func (s *RollbackSession) confirmInput(in InputFrame) (mispredicted bool, tick uint32) {
	// Ignore duplicate already-confirmed inputs.
	if existing, ok := s.confirmedInputs[in.Tick][in.Peer]; ok && existing.sameInput(in) {
		return false, 0
	}
	s.storeConfirmed(in)

	if in.Tick >= s.currentTick {
		return false, 0
	}
	used, ok := s.usedInputs[in.Tick][in.Peer]
	if !ok || !used.sameInput(in) {
		s.stats.Mispredictions++
		return true, in.Tick
	}
	return false, 0
}

// applyChange applies a membership change, the host's own or one it announced, and reports the earliest tick already simulated that it changes.
func (s *RollbackSession) applyChange(c memberChange) (mispredicted bool, tick uint32) {
	mark := func(mis bool, t uint32) {
		if mis && t < s.currentTick && (!mispredicted || t < tick) {
			mispredicted, tick = true, t
		}
	}
	switch c.kind {
	case memberJoin:
		if s.member(c.peer) != nil {
			return false, 0
		}
		s.addMember(c.peer, c.tick)
		s.preconfirm(*s.member(c.peer))
		mark(true, c.tick)
		for _, in := range s.takeStrays(c.peer) {
			if s.accepts(in) && in.Tick <= s.currentTick+maxRollbackLeadTicks {
				mark(s.confirmInput(in))
			}
		}
		// Local inputs sent before this peer was known never reached it.
		var own []InputFrame
		for t, m := range s.confirmedInputs {
			if in, ok := m[s.localID]; ok && t >= c.tick {
				own = append(own, in)
			}
		}
		slices.SortFunc(own, func(a, b InputFrame) int { return cmp.Compare(a.Tick, b.Tick) })
		if c.peer != s.localID {
			s.sendInputs(c.peer, own)
		}
	case memberLeave:
		mm := s.member(c.peer)
		if mm == nil || mm.until != noLeave || c.tick < mm.from {
			return false, 0
		}
		mm.until = c.tick
		for t, m := range s.confirmedInputs {
			if t >= c.tick {
				delete(m, c.peer)
			}
		}
		mark(true, c.tick)
		// The host's view of the leaving peer's inputs is final. Ticks this peer has confirmed already hold the same inputs.
		for _, in := range c.inputs {
			if in.Tick >= mm.from && int64(in.Tick) > s.confirmedTick {
				mark(s.confirmInput(in))
			}
		}
	}
	return mispredicted, tick
}

// join loads the host's join state and starts taking part at its tick.
func (s *RollbackSession) join(blob []byte) {
	js, ok := decodeJoinState(blob)
	if !ok || !s.acceptJoinState(js) || s.sim.Load(js.state) != nil {
		return
	}
	s.members = js.members
	s.joined = true
	s.start = js.tick
	s.currentTick = js.tick
	s.confirmedTick = int64(js.tick) - 1
	clear(s.confirmedInputs)
	clear(s.usedInputs)
	clear(s.snapshots)
	s.localHistory = nil
	for _, mm := range s.members {
		s.preconfirm(mm)
	}
	// The inputs of members already leaving come from the host's declaration, which the changes announcing their leave carried before this peer joined.
	for _, in := range js.inputs {
		s.storeConfirmed(in)
	}
	for _, mm := range s.members {
		for _, in := range s.takeStrays(mm.id) {
			if s.accepts(in) && in.Tick <= s.currentTick+maxRollbackLeadTicks {
				s.confirmInput(in)
			}
		}
	}
}

// sendDueJoinStates sends each pending join its state once every tick before its join tick is confirmed, so the state is final. A state grown past MaxJoinStateSize turns the join into a leave.
func (s *RollbackSession) sendDueJoinStates() error {
	for _, p := range s.takePending(func(p pendingJoin) bool { return s.confirmedTick+1 >= int64(p.tick) }) {
		if mm := s.member(p.peer); mm == nil || mm.until <= p.tick {
			continue
		}
		state, ok := s.snapshots[p.tick]
		if p.tick == s.currentTick {
			state, ok = s.sim.Save(), true
		}
		var js joinState
		if ok {
			js, ok = s.joinStateFor(p, state, func(id PeerID, t uint32) (InputFrame, bool) {
				in, ok := s.confirmedInputs[t][id]
				return in, ok
			})
		}
		if !ok {
			if err := s.dropJoin(p); err != nil {
				return err
			}
			continue
		}
		s.sendJoinState(p.peer, js)
	}
	return nil
}

func (s *RollbackSession) predictInput(peer PeerID, tick uint32) InputFrame {
	// Repeat the most recent confirmed input for peer strictly before tick.
	bestTick := int64(-1)
	var best InputFrame
	for t, m := range s.confirmedInputs {
		if t >= tick {
			continue
		}
		if in, ok := m[peer]; ok && int64(t) > bestTick {
			bestTick = int64(t)
			best = in
		}
	}
	if bestTick >= 0 {
		best.Tick = tick
		best.Peer = peer
		return best
	}
	return InputFrame{Tick: tick, Peer: peer}
}

func (s *RollbackSession) buildTickInputs(tick uint32, countPredictions bool) ([]InputFrame, map[PeerID]InputFrame) {
	ids := s.activeIDs(tick)
	ordered := make([]InputFrame, 0, len(ids))
	used := make(map[PeerID]InputFrame, len(ids))
	confirmed := s.confirmedInputs[tick]
	for _, p := range ids {
		in, ok := confirmed[p]
		if !ok {
			in = s.predictInput(p, tick)
			if countPredictions {
				s.stats.Predictions++
			}
		}
		ordered = append(ordered, in)
		used[p] = in
	}
	return ordered, used
}

func (s *RollbackSession) rollbackTo(targetTick uint32) error {
	if targetTick >= s.currentTick {
		return nil
	}
	depth := int(s.currentTick - targetTick)
	if depth > s.maxRollback {
		return ErrRollbackWindowExceeded
	}
	snap, ok := s.snapshots[targetTick]
	if !ok {
		return ErrRollbackWindowExceeded
	}
	if err := s.sim.Load(snap); err != nil {
		return err
	}

	s.stats.Rollbacks++
	if depth > s.stats.MaxRollbackDepth {
		s.stats.MaxRollbackDepth = depth
	}

	for t := targetTick; t < s.currentTick; t++ {
		if t > targetTick {
			saved := s.sim.Save()
			cp := make([]byte, len(saved))
			copy(cp, saved)
			s.snapshots[t] = cp
		}
		inputs, used := s.buildTickInputs(t, false)
		s.usedInputs[t] = used
		s.sim.Step(inputs)
		s.computedChecksums[t] = s.sim.Checksum()
		s.stats.ResimulatedFrames++
	}
	return nil
}

func (s *RollbackSession) advanceConfirmedAndChecksums() {
	for {
		next := s.confirmedTick + 1
		if next >= int64(s.currentTick) {
			break
		}
		t := uint32(next)
		m := s.confirmedInputs[t]
		allPresent := true
		for _, p := range s.activeIDs(t) {
			if _, ok := m[p]; !ok {
				allPresent = false
				break
			}
		}
		if !allPresent {
			break
		}
		s.confirmedTick = next

		if s.checksumInterval > 0 && (t+1)%s.checksumInterval == 0 {
			if !s.sentChecksum[t] {
				s.sentChecksum[t] = true
				sum := s.computedChecksums[t]
				msg := encodeChecksumMsg(t, s.localID, sum)
				for _, mm := range s.members {
					if mm.id != s.localID && mm.activeAt(t) {
						_ = s.send(mm.id, msg)
					}
				}
			}
			s.checkDesyncAt(t)
		}
	}
}

func (s *RollbackSession) recordRemoteChecksum(tick uint32, peer PeerID, sum uint64) {
	if peer == s.localID || s.member(peer) == nil || tick > s.currentTick+maxRollbackLeadTicks {
		return
	}
	m := s.remoteChecksums[tick]
	if m == nil {
		m = make(map[PeerID]uint64)
		s.remoteChecksums[tick] = m
	}
	m[peer] = sum
	if int64(tick) <= s.confirmedTick {
		s.checkDesyncAt(tick)
	}
}

func (s *RollbackSession) checkDesyncAt(tick uint32) {
	if int64(tick) > s.confirmedTick {
		return
	}
	localSum, ok := s.computedChecksums[tick]
	if !ok {
		return
	}
	for peer, remoteSum := range s.remoteChecksums[tick] {
		if remoteSum == localSum {
			continue
		}
		key := desyncKey{tick: tick, remote: peer}
		if _, already := s.reportedDesync[key]; already {
			continue
		}
		s.reportedDesync[key] = struct{}{}
		ev := DesyncEvent{
			Tick:           tick,
			LocalPeer:      s.localID,
			RemotePeer:     peer,
			LocalChecksum:  localSum,
			RemoteChecksum: remoteSum,
		}
		s.desyncs = append(s.desyncs, ev)
		s.stats.Desyncs++
		if s.onDesync != nil {
			s.onDesync(ev)
		}
	}
}

func (s *RollbackSession) pruneHistory() {
	// Retain snapshots and inputs needed for up to maxRollback ticks and
	// at least one confirmed tick behind confirmedTick for prediction lookup.
	keepFrom := int64(s.currentTick) - int64(s.maxRollback) - 2
	if s.confirmedTick-1 < keepFrom {
		keepFrom = s.confirmedTick - 1
	}
	if keepFrom <= 0 {
		return
	}
	floor := uint32(keepFrom)
	for t := range s.snapshots {
		if t < floor {
			delete(s.snapshots, t)
		}
	}
	for t := range s.usedInputs {
		if t < floor {
			delete(s.usedInputs, t)
		}
	}
	// Keep one extra tick of confirmedInputs so predictInput always has a prior sample.
	if floor > 2 {
		inputFloor := floor - 2
		for t := range s.confirmedInputs {
			if t < inputFloor {
				delete(s.confirmedInputs, t)
			}
		}
	}
	// Keep checksums for a wider window (256 ticks) for delayed verification.
	if s.currentTick > 256 {
		sumFloor := s.currentTick - 256
		for t := range s.computedChecksums {
			if t < sumFloor {
				delete(s.computedChecksums, t)
				delete(s.remoteChecksums, t)
				delete(s.sentChecksum, t)
			}
		}
	}
}

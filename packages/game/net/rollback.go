package net

import (
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
}

// LockstepSession advances a deterministic simulation tick only once every
// participating peer's input for that tick has arrived, exchanging periodic
// state checksums to detect desyncs.
type LockstepSession struct {
	transport        Transport
	sim              SimState
	localID          PeerID
	peers            []PeerID
	checksumInterval uint32
	onDesync         func(DesyncEvent)

	tick            uint32
	inputs          map[uint32]map[PeerID]InputFrame
	localChecksums  map[uint32]uint64
	remoteChecksums map[uint32]map[PeerID]uint64
	reportedDesync  map[desyncKey]struct{}
	desyncs         []DesyncEvent
}

// NewLockstepSession creates a strict lockstep session.
func NewLockstepSession(cfg LockstepConfig) *LockstepSession {
	var localID PeerID
	if cfg.Transport != nil {
		localID = cfg.Transport.LocalID()
	}
	var interval uint32
	if cfg.ChecksumInterval > 0 {
		interval = uint32(cfg.ChecksumInterval)
	}
	return &LockstepSession{
		transport:        cfg.Transport,
		sim:              cfg.Sim,
		localID:          localID,
		peers:            normalizePeers(cfg.Transport, cfg.Peers),
		checksumInterval: interval,
		onDesync:         cfg.OnDesync,
		inputs:           make(map[uint32]map[PeerID]InputFrame),
		localChecksums:   make(map[uint32]uint64),
		remoteChecksums:  make(map[uint32]map[PeerID]uint64),
		reportedDesync:   make(map[desyncKey]struct{}),
	}
}

// Tick returns the number of ticks simulated so far (also the next tick to execute).
func (s *LockstepSession) Tick() uint32 {
	return s.tick
}

// Desyncs returns a copy of all DesyncEvents observed so far.
func (s *LockstepSession) Desyncs() []DesyncEvent {
	out := make([]DesyncEvent, len(s.desyncs))
	copy(out, s.desyncs)
	return out
}

// SubmitInput records the local peer's input for in.Tick and broadcasts it to all remote peers.
func (s *LockstepSession) SubmitInput(in InputFrame) error {
	in.Peer = s.localID
	s.recordInput(in)
	payload := encodeSingleInput(in)
	var firstErr error
	for _, p := range s.peers {
		if p == s.localID {
			continue
		}
		if err := s.transport.Send(p, ChannelReliableOrdered, payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Poll drains all pending input and checksum messages from the Transport.
func (s *LockstepSession) Poll() {
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
			if in, valid := decodeInputFrame(pkt.Payload[1:]); valid {
				s.recordInput(in)
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
				s.recordInput(in)
			}
		case syncMsgChecksum:
			if tick, peer, sum, valid := decodeChecksumMsg(pkt.Payload[1:]); valid {
				s.recordRemoteChecksum(tick, peer, sum)
			}
		}
	}
}

// Step polls incoming packets and advances the simulation by one tick if every
// peer's input for the current tick is present. Returns true when a tick executed.
func (s *LockstepSession) Step() bool {
	s.Poll()
	tickInputs := s.inputs[s.tick]
	if len(tickInputs) < len(s.peers) {
		return false
	}
	ordered := make([]InputFrame, 0, len(s.peers))
	for _, p := range s.peers {
		in, ok := tickInputs[p]
		if !ok {
			return false
		}
		ordered = append(ordered, in)
	}

	executedTick := s.tick
	s.sim.Step(ordered)
	delete(s.inputs, executedTick)
	s.tick++

	if s.checksumInterval > 0 && (executedTick+1)%s.checksumInterval == 0 {
		sum := s.sim.Checksum()
		s.localChecksums[executedTick] = sum
		msg := encodeChecksumMsg(executedTick, s.localID, sum)
		for _, p := range s.peers {
			if p == s.localID {
				continue
			}
			_ = s.transport.Send(p, ChannelReliableOrdered, msg)
		}
		s.checkDesyncAt(executedTick)
	}
	return true
}

func (s *LockstepSession) recordInput(in InputFrame) {
	if in.Tick < s.tick {
		return
	}
	m := s.inputs[in.Tick]
	if m == nil {
		m = make(map[PeerID]InputFrame, len(s.peers))
		s.inputs[in.Tick] = m
	}
	m[in.Peer] = in
}

func (s *LockstepSession) recordRemoteChecksum(tick uint32, peer PeerID, sum uint64) {
	if peer == s.localID {
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
type RollbackSession struct {
	transport        Transport
	sim              SimState
	localID          PeerID
	peers            []PeerID
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
	var localID PeerID
	if cfg.Transport != nil {
		localID = cfg.Transport.LocalID()
	}
	peers := normalizePeers(cfg.Transport, cfg.Peers)

	s := &RollbackSession{
		transport:         cfg.Transport,
		sim:               cfg.Sim,
		localID:           localID,
		peers:             peers,
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

	// Pre-confirm zero inputs for initial delay frames [0, inputDelay).
	for t := uint32(0); t < uint32(delay); t++ {
		m := make(map[PeerID]InputFrame, len(peers))
		for _, p := range peers {
			m[p] = InputFrame{Tick: t, Peer: p}
		}
		s.confirmedInputs[t] = m
	}

	return s
}

// Tick returns the current simulation tick (number of ticks executed so far).
func (s *RollbackSession) Tick() uint32 {
	return s.currentTick
}

// ConfirmedTick returns the highest tick for which all peers' inputs are confirmed,
// or -1 if no tick is fully confirmed yet.
func (s *RollbackSession) ConfirmedTick() int64 {
	return s.confirmedTick
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

// Step submits the local input (applied at currentTick + InputDelay), drains
// incoming packets (executing any needed rollbacks), simulates the next tick,
// and exchanges checksums for newly confirmed ticks.
func (s *RollbackSession) Step(localInput InputFrame) error {
	targetTick := s.currentTick + uint32(s.inputDelay)
	localInput.Peer = s.localID
	localInput.Tick = targetTick

	s.storeConfirmed(localInput)
	s.broadcastLocalInput(localInput)

	if err := s.Poll(); err != nil {
		return err
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
	s.pruneHistory()
	return nil
}

// Poll drains incoming remote inputs and checksums from the Transport,
// rolling back and re-simulating if any late remote input diverges from what
// was previously predicted.
func (s *RollbackSession) Poll() error {
	earliestMispredict := uint32(0)
	hasMispredict := false

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
			if in, valid := decodeInputFrame(pkt.Payload[1:]); valid {
				if mis, tick := s.ingestRemoteInput(in); mis {
					if !hasMispredict || tick < earliestMispredict {
						earliestMispredict = tick
						hasMispredict = true
					}
				}
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
				if mis, tick := s.ingestRemoteInput(in); mis {
					if !hasMispredict || tick < earliestMispredict {
						earliestMispredict = tick
						hasMispredict = true
					}
				}
			}
		case syncMsgChecksum:
			if tick, peer, sum, valid := decodeChecksumMsg(pkt.Payload[1:]); valid {
				s.recordRemoteChecksum(tick, peer, sum)
			}
		}
	}

	if hasMispredict {
		if err := s.rollbackTo(earliestMispredict); err != nil {
			return err
		}
	}

	s.advanceConfirmedAndChecksums()
	s.pruneHistory()
	return nil
}

func (s *RollbackSession) broadcastLocalInput(in InputFrame) {
	s.localHistory = append(s.localHistory, in)
	redundantMax := min(8, max(1, s.maxRollback))
	if len(s.localHistory) > redundantMax {
		s.localHistory = s.localHistory[len(s.localHistory)-redundantMax:]
	}
	payload := encodeInputBundle(s.localHistory)
	for _, p := range s.peers {
		if p == s.localID {
			continue
		}
		_ = s.transport.Send(p, ChannelReliableOrdered, payload)
	}
}

func (s *RollbackSession) storeConfirmed(in InputFrame) {
	m := s.confirmedInputs[in.Tick]
	if m == nil {
		m = make(map[PeerID]InputFrame, len(s.peers))
		s.confirmedInputs[in.Tick] = m
	}
	m[in.Peer] = in
}

func (s *RollbackSession) ingestRemoteInput(in InputFrame) (mispredicted bool, tick uint32) {
	if in.Peer == s.localID {
		return false, 0
	}
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
	ordered := make([]InputFrame, 0, len(s.peers))
	used := make(map[PeerID]InputFrame, len(s.peers))
	confirmed := s.confirmedInputs[tick]
	for _, p := range s.peers {
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
		if len(m) < len(s.peers) {
			break
		}
		allPresent := true
		for _, p := range s.peers {
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
				for _, p := range s.peers {
					if p == s.localID {
						continue
					}
					_ = s.transport.Send(p, ChannelReliableOrdered, msg)
				}
			}
			s.checkDesyncAt(t)
		}
	}
}

func (s *RollbackSession) recordRemoteChecksum(tick uint32, peer PeerID, sum uint64) {
	if peer == s.localID {
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

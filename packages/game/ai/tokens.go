package ai

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// TokenPoolConfig configures a TokenPool for a single target.
type TokenPoolConfig struct {
	// Capacity is the maximum number of attack tokens that may be held at once.
	// Values <= 0 allow no tokens to be granted.
	Capacity int
	// Cooldown is the duration a returned token must wait before it can be acquired again.
	// Tokens transferred directly by preemption do not enter cooldown.
	Cooldown time.Duration
	// PreemptMargin is the minimum priority advantage a waiting requester must have over the
	// lowest-priority holder to take (preempt) its token. When <= 0, any strictly higher
	// priority (requester > holder) may preempt.
	PreemptMargin float64
	// MaxHoldDuration automatically releases a held token once it has been held for this
	// duration, returning it to cooldown. Values <= 0 disable automatic timeout.
	MaxHoldDuration time.Duration
}

func sanitizeTokenPoolConfig(cfg TokenPoolConfig) TokenPoolConfig {
	if cfg.Capacity < 0 {
		cfg.Capacity = 0
	}
	if cfg.Cooldown < 0 {
		cfg.Cooldown = 0
	}
	if cfg.PreemptMargin < 0 || math.IsNaN(cfg.PreemptMargin) {
		cfg.PreemptMargin = 0
	}
	if cfg.MaxHoldDuration < 0 {
		cfg.MaxHoldDuration = 0
	}
	return cfg
}

// TokenHolder describes an agent that currently holds an attack token.
type TokenHolder struct {
	// AgentID uniquely identifies the holding agent.
	AgentID uint64
	// Priority is the agent's current attack priority used for preemption comparisons.
	Priority float64
	// Held is how long the agent has held this token.
	Held time.Duration
}

type tokenRequest struct {
	agentID  uint64
	priority float64
}

// TokenPool arbitrates a fixed number of attack tokens for one target.
//
// Agents request tokens with a priority via Request, and Step advances cooldown and timeout
// durations while resolving pending requests deterministically (highest priority first,
// breaking ties by lowest agent ID regardless of submission order). Higher-priority
// requesters can preempt lower-priority holders when no free tokens remain. Returned tokens
// wait for Cooldown before becoming available again.
//
// TokenPool is not safe for concurrent use; drive it on game.Loop's fixed steps.
type TokenPool struct {
	cfg       TokenPoolConfig
	available int
	holders   []TokenHolder
	pending   []tokenRequest
	cooldowns []time.Duration
}

// NewTokenPool returns a TokenPool initialized with cfg.
func NewTokenPool(cfg TokenPoolConfig) *TokenPool {
	cfg = sanitizeTokenPoolConfig(cfg)
	return &TokenPool{
		cfg:       cfg,
		available: cfg.Capacity,
	}
}

// Config returns the pool's effective configuration.
func (p *TokenPool) Config() TokenPoolConfig { return p.cfg }

// Capacity returns the maximum number of tokens in the pool.
func (p *TokenPool) Capacity() int { return p.cfg.Capacity }

// Available returns the number of tokens immediately free to grant on the next Step.
func (p *TokenPool) Available() int { return p.available }

// CoolingDown returns the number of returned tokens currently waiting on cooldown.
func (p *TokenPool) CoolingDown() int { return len(p.cooldowns) }

// HeldCount returns the number of tokens currently held by agents.
func (p *TokenPool) HeldCount() int { return len(p.holders) }

// PendingCount returns the number of waiting token requests.
func (p *TokenPool) PendingCount() int { return len(p.pending) }

// HasToken reports whether agentID currently holds an attack token from p.
func (p *TokenPool) HasToken(agentID uint64) bool {
	return p.holderIndex(agentID) >= 0
}

// Holder returns the TokenHolder record for agentID if it currently holds a token.
func (p *TokenPool) Holder(agentID uint64) (TokenHolder, bool) {
	if i := p.holderIndex(agentID); i >= 0 {
		return p.holders[i], true
	}
	return TokenHolder{}, false
}

// Holders appends all active holders to dst[:0], sorted by ascending AgentID.
func (p *TokenPool) Holders(dst []TokenHolder) []TokenHolder {
	dst = append(dst[:0], p.holders...)
	slices.SortFunc(dst, func(a, b TokenHolder) int {
		return cmp.Compare(a.AgentID, b.AgentID)
	})
	return dst
}

// Request submits or updates an attack token request for agentID at priority.
//
// If agentID already holds a token, Request refreshes its priority for future preemption
// comparisons without consuming a second token. Requests with a NaN priority are ignored.
// Pending requests persist until granted, cancelled via Cancel or Remove, or cleared by Reset.
func (p *TokenPool) Request(agentID uint64, priority float64) {
	if math.IsNaN(priority) {
		return
	}
	if i := p.holderIndex(agentID); i >= 0 {
		p.holders[i].Priority = priority
		return
	}
	if i := p.pendingIndex(agentID); i >= 0 {
		p.pending[i].priority = priority
		return
	}
	p.pending = append(p.pending, tokenRequest{
		agentID:  agentID,
		priority: priority,
	})
}

// Cancel withdraws a pending request for agentID without releasing a held token.
// It reports whether a pending request existed.
func (p *TokenPool) Cancel(agentID uint64) bool {
	i := p.pendingIndex(agentID)
	if i < 0 {
		return false
	}
	p.removePendingAt(i)
	return true
}

// Release returns agentID's held token to the pool (starting Cooldown if configured) and
// withdraws any pending request for agentID. It reports whether agentID held a token.
func (p *TokenPool) Release(agentID uint64) bool {
	p.Cancel(agentID)
	i := p.holderIndex(agentID)
	if i < 0 {
		return false
	}
	p.returnTokenAt(i)
	return true
}

// Remove releases any token held by agentID and cancels any pending request for agentID.
// Call Remove when an agent dies, despawns, or disengages from the target.
// It reports whether agentID held a token or had a pending request.
func (p *TokenPool) Remove(agentID uint64) bool {
	canceled := p.Cancel(agentID)
	released := false
	if i := p.holderIndex(agentID); i >= 0 {
		p.returnTokenAt(i)
		released = true
	}
	return canceled || released
}

// Step advances cooldown and hold timers by dt and deterministically resolves pending requests.
//
// Within a step:
//  1. Cooling-down tokens advance; any token whose remaining cooldown reaches <= 0 becomes available.
//  2. Active holders' hold durations advance; any holder reaching MaxHoldDuration returns its token
//     (entering Cooldown with its full duration when Cooldown > 0, or becoming available immediately).
//  3. Pending requests are sorted by descending priority, breaking ties by ascending AgentID.
//  4. Requests first consume available tokens, then preempt the lowest-priority holder (breaking
//     holder ties by highest AgentID) when the requester's priority exceeds the holder's by
//     PreemptMargin (or strictly exceeds it when PreemptMargin == 0).
func (p *TokenPool) Step(dt time.Duration) {
	if dt < 0 {
		dt = 0
	}

	// 1. Advance existing token cooldowns before expiring holders so newly expired tokens
	// receive their full Cooldown duration instead of losing dt on the same step.
	if dt > 0 && len(p.cooldowns) > 0 {
		n := 0
		for _, rem := range p.cooldowns {
			rem -= dt
			if rem <= 0 {
				p.available++
				continue
			}
			p.cooldowns[n] = rem
			n++
		}
		p.cooldowns = p.cooldowns[:n]
	}

	// 2. Advance active hold durations and expire timed-out holders.
	if dt > 0 && len(p.holders) > 0 {
		n := 0
		for _, h := range p.holders {
			h.Held += dt
			if p.cfg.MaxHoldDuration > 0 && h.Held >= p.cfg.MaxHoldDuration {
				p.returnTokenValue()
				continue
			}
			p.holders[n] = h
			n++
		}
		p.holders = p.holders[:n]
	}

	if len(p.pending) == 0 || p.cfg.Capacity <= 0 {
		return
	}

	// 3. Sort pending requests deterministically: higher priority first, then lower AgentID.
	slices.SortFunc(p.pending, func(a, b tokenRequest) int {
		if a.priority > b.priority {
			return -1
		}
		if a.priority < b.priority {
			return 1
		}
		return cmp.Compare(a.agentID, b.agentID)
	})

	// 4. Grant available tokens, then evaluate preemption.
	kept := 0
	for i := 0; i < len(p.pending); i++ {
		req := p.pending[i]
		if p.available > 0 {
			p.available--
			p.holders = append(p.holders, TokenHolder{
				AgentID:  req.agentID,
				Priority: req.priority,
			})
			continue
		}
		weakIdx := p.weakestHolderIndex()
		if weakIdx >= 0 && p.canPreempt(req.priority, p.holders[weakIdx].Priority) {
			p.holders[weakIdx] = TokenHolder{
				AgentID:  req.agentID,
				Priority: req.priority,
			}
			continue
		}
		// Remaining pending requests have <= req.priority, so none can preempt either.
		copy(p.pending[kept:], p.pending[i:])
		kept += len(p.pending) - i
		break
	}
	p.pending = p.pending[:kept]
}

// Reset clears all active holders, pending requests, and cooldown timers, restoring Available() to Capacity().
func (p *TokenPool) Reset() {
	p.available = p.cfg.Capacity
	p.holders = p.holders[:0]
	p.pending = p.pending[:0]
	p.cooldowns = p.cooldowns[:0]
}

func (p *TokenPool) canPreempt(reqPriority, holderPriority float64) bool {
	if reqPriority <= holderPriority {
		return false
	}
	if p.cfg.PreemptMargin > 0 {
		return reqPriority-holderPriority >= p.cfg.PreemptMargin
	}
	return true
}

func (p *TokenPool) holderIndex(agentID uint64) int {
	for i, h := range p.holders {
		if h.AgentID == agentID {
			return i
		}
	}
	return -1
}

func (p *TokenPool) pendingIndex(agentID uint64) int {
	for i, r := range p.pending {
		if r.agentID == agentID {
			return i
		}
	}
	return -1
}

func (p *TokenPool) removePendingAt(i int) {
	last := len(p.pending) - 1
	p.pending[i] = p.pending[last]
	p.pending = p.pending[:last]
}

func (p *TokenPool) returnTokenAt(i int) {
	last := len(p.holders) - 1
	p.holders[i] = p.holders[last]
	p.holders = p.holders[:last]
	p.returnTokenValue()
}

func (p *TokenPool) returnTokenValue() {
	if p.cfg.Cooldown > 0 {
		p.cooldowns = append(p.cooldowns, p.cfg.Cooldown)
	} else {
		p.available++
	}
}

// weakestHolderIndex finds the holder with the lowest Priority, breaking ties by highest AgentID
// so lower AgentIDs are consistently favored.
func (p *TokenPool) weakestHolderIndex() int {
	if len(p.holders) == 0 {
		return -1
	}
	best := 0
	for i := 1; i < len(p.holders); i++ {
		h := p.holders[i]
		w := p.holders[best]
		if h.Priority < w.Priority || (h.Priority == w.Priority && h.AgentID > w.AgentID) {
			best = i
		}
	}
	return best
}

// TokenManager maintains one TokenPool per target ID.
//
// Targets that have not been explicitly configured with ConfigureTarget are created on demand
// with the manager's default TokenPoolConfig when Request is called. All multi-target operations
// iterate targets in ascending target ID order for determinism.
type TokenManager struct {
	defaultCfg TokenPoolConfig
	pools      map[uint64]*TokenPool
	targetIDs  []uint64 // kept sorted ascending for deterministic iteration
}

// NewTokenManager returns a TokenManager using defaultCfg for targets created on demand.
func NewTokenManager(defaultCfg TokenPoolConfig) *TokenManager {
	return &TokenManager{
		defaultCfg: sanitizeTokenPoolConfig(defaultCfg),
	}
}

// ConfigureTarget creates or replaces the TokenPool for targetID with cfg and returns it.
func (m *TokenManager) ConfigureTarget(targetID uint64, cfg TokenPoolConfig) *TokenPool {
	if m.pools == nil {
		m.pools = make(map[uint64]*TokenPool)
	}
	pool := NewTokenPool(cfg)
	if _, exists := m.pools[targetID]; !exists {
		m.insertTargetID(targetID)
	}
	m.pools[targetID] = pool
	return pool
}

// Pool returns the TokenPool for targetID, or nil if none exists yet.
func (m *TokenManager) Pool(targetID uint64) *TokenPool {
	return m.pools[targetID]
}

// RemoveTarget deletes targetID's TokenPool (for example, when that target is defeated).
func (m *TokenManager) RemoveTarget(targetID uint64) bool {
	if _, ok := m.pools[targetID]; !ok {
		return false
	}
	delete(m.pools, targetID)
	if idx, found := slices.BinarySearch(m.targetIDs, targetID); found {
		m.targetIDs = slices.Delete(m.targetIDs, idx, idx+1)
	}
	return true
}

// Request submits or updates an attack token request for agentID against targetID, creating
// targetID's pool with the manager's default configuration if needed.
func (m *TokenManager) Request(targetID, agentID uint64, priority float64) {
	pool := m.pools[targetID]
	if pool == nil {
		pool = m.ConfigureTarget(targetID, m.defaultCfg)
	}
	pool.Request(agentID, priority)
}

// HasToken reports whether agentID holds an attack token for targetID.
func (m *TokenManager) HasToken(targetID, agentID uint64) bool {
	if pool := m.pools[targetID]; pool != nil {
		return pool.HasToken(agentID)
	}
	return false
}

// Cancel withdraws agentID's pending request for targetID.
func (m *TokenManager) Cancel(targetID, agentID uint64) bool {
	if pool := m.pools[targetID]; pool != nil {
		return pool.Cancel(agentID)
	}
	return false
}

// Release returns agentID's held token for targetID to cooldown and cancels any pending request.
func (m *TokenManager) Release(targetID, agentID uint64) bool {
	if pool := m.pools[targetID]; pool != nil {
		return pool.Release(agentID)
	}
	return false
}

// RemoveAgent releases any tokens held by agentID and cancels any pending requests across all targets.
func (m *TokenManager) RemoveAgent(agentID uint64) {
	for _, tid := range m.targetIDs {
		m.pools[tid].Remove(agentID)
	}
}

// Step advances every target's TokenPool by dt in deterministic ascending target ID order.
func (m *TokenManager) Step(dt time.Duration) {
	for _, tid := range m.targetIDs {
		m.pools[tid].Step(dt)
	}
}

// Reset removes all target pools from m.
func (m *TokenManager) Reset() {
	clear(m.pools)
	m.targetIDs = m.targetIDs[:0]
}

func (m *TokenManager) insertTargetID(targetID uint64) {
	idx, found := slices.BinarySearch(m.targetIDs, targetID)
	if !found {
		m.targetIDs = slices.Insert(m.targetIDs, idx, targetID)
	}
}

// HasAttackToken returns a Condition behavior tree node that succeeds while agentID holds a token in pool.
func HasAttackToken(pool *TokenPool, agentID uint64) Node {
	return Condition(func() bool {
		return pool != nil && pool.HasToken(agentID)
	})
}

// WithAttackToken returns a behavior tree decorator that requests an attack token for agentID
// on pool (evaluating priority on each tick if non-nil, else 0) and ticks child only while
// agentID holds a token.
//
// While waiting for a token, or if the token is preempted, WithAttackToken resets child (if it
// was running) and returns StatusFailure so a parent Selector can fall back to circling or
// holding position. When child finishes with StatusSuccess or StatusFailure, WithAttackToken
// releases the token back to pool. Calling Reset cancels any pending request or releases the
// held token.
func WithAttackToken(pool *TokenPool, agentID uint64, priority func() float64, child Node) Node {
	return &attackTokenNode{
		pool:     pool,
		agentID:  agentID,
		priority: priority,
		child:    child,
	}
}

type attackTokenNode struct {
	pool     *TokenPool
	agentID  uint64
	priority func() float64
	child    Node
	running  bool
}

func (n *attackTokenNode) Tick(dt time.Duration) Status {
	if n.pool == nil || n.child == nil {
		return StatusFailure
	}
	var prio float64
	if n.priority != nil {
		prio = n.priority()
	}
	n.pool.Request(n.agentID, prio)
	if !n.pool.HasToken(n.agentID) {
		if n.running {
			n.child.Reset()
			n.running = false
		}
		return StatusFailure
	}
	st := n.child.Tick(dt)
	if st == StatusRunning {
		n.running = true
		return StatusRunning
	}
	n.running = false
	n.pool.Release(n.agentID)
	return st
}

func (n *attackTokenNode) Reset() {
	if n.running && n.child != nil {
		n.child.Reset()
	}
	n.running = false
	if n.pool != nil {
		n.pool.Remove(n.agentID)
	}
}

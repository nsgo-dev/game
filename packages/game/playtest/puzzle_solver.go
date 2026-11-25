package playtest

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/replay"
)

// ProfilePuzzleSolver is the built-in puzzle solver and human-like player bot profile for puzzle2d templates.
const ProfilePuzzleSolver = "puzzle-solver"

const (
	defaultPuzzleStateLimit    = 2_000_000
	maxPuzzleStateLimit        = 50_000_000
	defaultPuzzleUndoAfterBad  = 3
	maxPuzzleUndoAfterBad      = 1_000
	defaultPuzzleMistakeRate   = 0.35
	defaultPuzzleMaxHumanMoves = 200
	maxPuzzleMaxHumanMoves     = 100_000
	puzzleModeHuman            = "human"
	puzzleModeSolver           = "solver"
)

func init() {
	RegisterBot(ProfilePuzzleSolver, NewPuzzleSolver)
}

// PuzzleMove names one discrete puzzle move (such as "up", "down", "left" or "right").
type PuzzleMove string

// PuzzleState is a discrete puzzle state explored by SolvePuzzleBFS and PuzzleSolver.
type PuzzleState interface {
	// Key returns a unique canonical string identifying this state for visited-state deduplication.
	Key() string
	// Solved reports whether the puzzle's win condition is satisfied.
	Solved() bool
	// Dead reports whether this state is an unrecoverable loss (such as falling into a void).
	Dead() bool
	// Moves returns the legal moves from this state in deterministic order.
	Moves() []PuzzleMove
	// Apply returns the next state after taking move, and whether the move changed the state.
	Apply(move PuzzleMove) (PuzzleState, bool)
}

// PuzzleScorer is optionally implemented by a PuzzleState to provide a progress heuristic (higher is closer to solved) for the human-like puzzle player.
type PuzzleScorer interface {
	ProgressScore() float64
}

// PuzzleLevel describes one level in a puzzle pack evaluated by EvaluatePuzzlePack or NewPuzzlePackSim.
type PuzzleLevel struct {
	// ID uniquely names the level (such as "level_01" or "first-light").
	ID string
	// Name is an optional display title.
	Name string
	// Par is the target move count (such as the 3-star threshold).
	Par int
	// IntendedMoves is the designer's intended solution length; defaults to Par when <= 0.
	IntendedMoves int
	// Start is the initial PuzzleState of the level.
	Start PuzzleState
}

// EffectiveIntendedMoves returns l.IntendedMoves when positive, falling back to l.Par.
func (l PuzzleLevel) EffectiveIntendedMoves() int {
	if l.IntendedMoves > 0 {
		return l.IntendedMoves
	}
	return l.Par
}

// PuzzleSolveResult is the outcome of a breadth-first shortest-solution search on one PuzzleState.
type PuzzleSolveResult struct {
	// Solved reports whether a solution was found within the state limit.
	Solved bool
	// Solution is the shortest sequence of moves from the start state to a solved state.
	Solution []PuzzleMove
	// Moves is len(Solution) when Solved is true, or 0 otherwise.
	Moves int
	// StatesExplored is the number of distinct states visited by BFS.
	StatesExplored int
	// HitStateLimit reports whether BFS terminated because it reached maxStates.
	HitStateLimit bool
}

// SolvePuzzleBFS runs a deterministic breadth-first search from start to find the shortest sequence of moves reaching a Solved state, stopping if maxStates distinct states are visited.
// When maxStates <= 0, it defaults to 2,000,000 states.
func SolvePuzzleBFS(start PuzzleState, maxStates int) PuzzleSolveResult {
	if start == nil {
		return PuzzleSolveResult{}
	}
	if maxStates <= 0 {
		maxStates = defaultPuzzleStateLimit
	}
	if start.Solved() {
		return PuzzleSolveResult{Solved: true, StatesExplored: 1}
	}
	if start.Dead() {
		return PuzzleSolveResult{StatesExplored: 1}
	}

	type bfsNode struct {
		state  PuzzleState
		parent int
		move   PuzzleMove
	}
	visited := make(map[string]struct{}, min(maxStates, 4096))
	visited[start.Key()] = struct{}{}
	queue := make([]bfsNode, 0, min(maxStates, 1024))
	queue = append(queue, bfsNode{state: start, parent: -1})

	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		for _, mv := range cur.state.Moves() {
			next, changed := cur.state.Apply(mv)
			if !changed || next == nil || next.Dead() {
				continue
			}
			if next.Solved() {
				// Reconstruct shortest move sequence from parent pointers.
				var rev []PuzzleMove
				rev = append(rev, mv)
				for p := head; p >= 0 && queue[p].parent >= 0; p = queue[p].parent {
					rev = append(rev, queue[p].move)
				}
				slices.Reverse(rev)
				return PuzzleSolveResult{
					Solved:         true,
					Solution:       rev,
					Moves:          len(rev),
					StatesExplored: len(visited),
				}
			}
			k := next.Key()
			if _, ok := visited[k]; ok {
				continue
			}
			if len(visited) >= maxStates {
				return PuzzleSolveResult{
					StatesExplored: len(visited),
					HitStateLimit:  true,
				}
			}
			visited[k] = struct{}{}
			queue = append(queue, bfsNode{state: next, parent: head, move: mv})
		}
	}
	return PuzzleSolveResult{StatesExplored: len(visited)}
}

// PuzzleObservation is the per-step world view a puzzle simulation supplies to the "puzzle-solver" bot.
type PuzzleObservation struct {
	// LevelID identifies the current level so per-level plans reset cleanly when advancing levels.
	LevelID string
	// State is the current puzzle state.
	State PuzzleState
	// CanUndo reports whether the puzzle simulation supports undoing the last move.
	CanUndo bool
	// ParMoves is the level's par move count.
	ParMoves int
	// IntendedMoves is the designer's intended solution move count.
	IntendedMoves int
}

// PuzzleHumanResult summarizes one deterministic human-like playthrough of a puzzle level.
type PuzzleHumanResult struct {
	// Solved reports whether the human-like player solved the level.
	Solved bool
	// Moves is the net number of moves in the winning state (or final move count if unsolved).
	Moves int
	// MovesOverPar is Moves - Par when Solved is true.
	MovesOverPar int
	// Undos is how many undo actions the human-like player performed after bad-move streaks or dead states.
	Undos int
	// Actions is the sequence of move and undo action names emitted.
	Actions []string
}

type humanStackEntry struct {
	fromKey  string
	stateKey string
	move     PuzzleMove
	score    float64
	optDist  int
	bad      bool
}

// PuzzleSolver is the built-in "puzzle-solver" bot: in "solver" mode it plays the BFS shortest solution, and in "human" mode it plays like a human who explores, makes occasional mistakes, and undoes after three bad moves.
type PuzzleSolver struct {
	stateLimit        int
	undoAfterBadMoves int
	mode              string
	mistakeRate       float64
	maxHumanMoves     int

	upAction    string
	downAction  string
	leftAction  string
	rightAction string
	undoAction  string

	// Per-level deterministic state.
	levelID        string
	planned        bool
	solverPlan     []PuzzleMove
	solverIdx      int
	pulseRelease   bool
	optDistCache   map[string]int
	stack          []humanStackEntry
	visitedCount   map[string]int
	badTransitions map[string]map[PuzzleMove]bool
	badStreak      int
	pendingUndos   int
	lastMoveFrom   string
	lastMove       PuzzleMove
	lastMoveScore  float64
	lastMoveDist   int
	hasLastMove    bool
}

// NewPuzzleSolver constructs a PuzzleSolver from a playtest BotConfig.
//
// Supported optional Bot.Params:
//   - "stateLimit" (number, default 2000000): maximum distinct states BFS may explore per level.
//   - "undoAfterBadMoves" (number, default 3): consecutive bad moves before the human-like player undoes.
//   - "mode" (string, default "human"): "human" (human-like player with undo) or "solver" (BFS shortest solution).
//   - "mistakeRate" (number in [0, 1], default 0.35): seeded exploration rate for non-optimal moves in human mode.
//   - "maxHumanMoves" (number, default 200): step cap per level in pack evaluations.
//   - "upAction" (string, default "up")
//   - "downAction" (string, default "down")
//   - "leftAction" (string, default "left")
//   - "rightAction" (string, default "right")
//   - "undoAction" (string, default "undo")
func NewPuzzleSolver(cfg BotConfig) (Bot, error) {
	b := &PuzzleSolver{
		stateLimit:        defaultPuzzleStateLimit,
		undoAfterBadMoves: defaultPuzzleUndoAfterBad,
		mode:              puzzleModeHuman,
		mistakeRate:       defaultPuzzleMistakeRate,
		maxHumanMoves:     defaultPuzzleMaxHumanMoves,
		upAction:          "up",
		downAction:        "down",
		leftAction:        "left",
		rightAction:       "right",
		undoAction:        "undo",
	}
	if cfg.Params != nil {
		if v, ok := cfg.Params["stateLimit"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 1 || n > maxPuzzleStateLimit {
				return nil, fmt.Errorf("playtest: puzzle-solver param stateLimit must be in [1, %d]", maxPuzzleStateLimit)
			}
			b.stateLimit = int(math.Round(n))
		}
		if v, ok := cfg.Params["undoAfterBadMoves"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 1 || n > maxPuzzleUndoAfterBad {
				return nil, fmt.Errorf("playtest: puzzle-solver param undoAfterBadMoves must be in [1, %d]", maxPuzzleUndoAfterBad)
			}
			b.undoAfterBadMoves = int(math.Round(n))
		}
		if v, ok := cfg.Params["mode"]; ok {
			s, ok := v.(string)
			if !ok || (s != puzzleModeHuman && s != puzzleModeSolver) {
				return nil, fmt.Errorf("playtest: puzzle-solver param mode must be %q or %q", puzzleModeHuman, puzzleModeSolver)
			}
			b.mode = s
		}
		if v, ok := cfg.Params["mistakeRate"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 0 || n > 1 {
				return nil, fmt.Errorf("playtest: puzzle-solver param mistakeRate must be in [0, 1]")
			}
			b.mistakeRate = n
		}
		if v, ok := cfg.Params["maxHumanMoves"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 1 || n > maxPuzzleMaxHumanMoves {
				return nil, fmt.Errorf("playtest: puzzle-solver param maxHumanMoves must be in [1, %d]", maxPuzzleMaxHumanMoves)
			}
			b.maxHumanMoves = int(math.Round(n))
		}
		strParams := []struct {
			key string
			dst *string
		}{
			{"upAction", &b.upAction},
			{"downAction", &b.downAction},
			{"leftAction", &b.leftAction},
			{"rightAction", &b.rightAction},
			{"undoAction", &b.undoAction},
		}
		for _, sp := range strParams {
			if v, ok := cfg.Params[sp.key]; ok {
				s, ok := v.(string)
				if !ok || s == "" {
					return nil, fmt.Errorf("playtest: puzzle-solver param %s must be a non-empty string", sp.key)
				}
				*sp.dst = s
			}
		}
	}
	return b, nil
}

// Channels declares the default action names that puzzle-solver drives.
func (b *PuzzleSolver) Channels() (actions, axes []string) {
	return []string{b.upAction, b.downAction, b.leftAction, b.rightAction, b.undoAction}, nil
}

func (b *PuzzleSolver) resetLevel(levelID string) {
	b.levelID = levelID
	b.planned = false
	b.solverPlan = nil
	b.solverIdx = 0
	b.optDistCache = make(map[string]int)
	b.stack = nil
	b.visitedCount = make(map[string]int)
	b.badTransitions = make(map[string]map[PuzzleMove]bool)
	b.badStreak = 0
	b.pendingUndos = 0
	b.hasLastMove = false
}

// Decide chooses the next move or undo action for one puzzle simulation step.
func (b *PuzzleSolver) Decide(_ int, _ time.Duration, rawObs any, rng *random.Stream) InputState {
	var obs PuzzleObservation
	switch v := rawObs.(type) {
	case PuzzleObservation:
		obs = v
	case *PuzzleObservation:
		if v != nil {
			obs = *v
		}
	default:
		return InputState{}
	}
	if obs.State == nil {
		return InputState{}
	}

	if obs.LevelID != b.levelID || !b.planned {
		b.resetLevel(obs.LevelID)
		b.planned = true
		if b.mode == puzzleModeSolver {
			res := SolvePuzzleBFS(obs.State, b.stateLimit)
			if res.Solved {
				b.solverPlan = res.Solution
			}
		}
	}

	// Alternate one release step between button pulses so JustPressed fires reliably in every game loop.
	if b.pulseRelease {
		b.pulseRelease = false
		return InputState{}
	}

	if obs.State.Solved() {
		return InputState{}
	}

	if b.mode == puzzleModeSolver {
		if b.solverIdx >= len(b.solverPlan) {
			return InputState{}
		}
		mv := b.solverPlan[b.solverIdx]
		b.solverIdx++
		b.pulseRelease = true
		return b.moveInput(mv)
	}

	// Human-like mode: evaluate outcome of the previous move and undo after bad-move streaks or dead states.
	action := b.decideHumanAction(obs.State, obs.CanUndo, rng)
	if action == "" {
		return InputState{}
	}
	b.pulseRelease = true
	return InputState{Actions: map[string]bool{action: true}}
}

func (b *PuzzleSolver) decideHumanAction(state PuzzleState, canUndo bool, rng *random.Stream) string {
	curKey := state.Key()
	curScore := puzzleStateScore(state)
	curDist := b.shortestDist(state)

	if b.hasLastMove {
		b.hasLastMove = false
		isDead := state.Dead()
		isBad := isDead || curDist < 0 || (b.lastMoveDist >= 0 && curDist >= b.lastMoveDist) || (curDist < 0 && curScore <= b.lastMoveScore)
		if b.visitedCount[curKey] > 0 {
			isBad = true
		}
		b.stack = append(b.stack, humanStackEntry{
			fromKey:  b.lastMoveFrom,
			stateKey: curKey,
			move:     b.lastMove,
			score:    curScore,
			optDist:  curDist,
			bad:      isBad,
		})
		if isDead && canUndo {
			// Immediately undo falling into a void or unrecoverable dead end and remember the move as bad.
			b.recordBadTransition(b.lastMoveFrom, b.lastMove)
			b.popStack()
			b.badStreak = 0
			return b.undoAction
		}
		if isBad {
			b.badStreak++
			if b.badStreak >= b.undoAfterBadMoves && canUndo && len(b.stack) > 0 {
				undoCount := min(b.badStreak, len(b.stack))
				// Mark the branch root move that kicked off the bad streak as tried/bad.
				branchRoot := b.stack[len(b.stack)-undoCount]
				b.recordBadTransition(branchRoot.fromKey, branchRoot.move)
				b.pendingUndos = undoCount
				b.badStreak = 0
			}
		} else {
			b.badStreak = 0
		}
	}

	// Drain any queued undos from a 3-bad-moves streak.
	if b.pendingUndos > 0 {
		if canUndo && len(b.stack) > 0 {
			b.pendingUndos--
			b.popStack()
			return b.undoAction
		}
		b.pendingUndos = 0
	}

	b.visitedCount[curKey]++

	moves := state.Moves()
	if len(moves) == 0 {
		if canUndo && len(b.stack) > 0 {
			top := b.stack[len(b.stack)-1]
			b.recordBadTransition(top.fromKey, top.move)
			b.popStack()
			return b.undoAction
		}
		return ""
	}

	type candidate struct {
		move    PuzzleMove
		next    PuzzleState
		dist    int
		score   float64
		tried   bool
		visited bool
	}
	cands := make([]candidate, 0, len(moves))
	for _, mv := range moves {
		next, changed := state.Apply(mv)
		if !changed || next == nil {
			continue
		}
		nk := next.Key()
		c := candidate{
			move:    mv,
			next:    next,
			dist:    b.shortestDist(next),
			score:   puzzleStateScore(next),
			tried:   b.badTransitions[curKey][mv],
			visited: b.visitedCount[nk] > 0,
		}
		cands = append(cands, c)
	}
	if len(cands) == 0 {
		return ""
	}

	// Partition candidates into optimal/progress moves and plausible exploratory mistakes.
	var bestOpt []candidate
	bestDist := math.MaxInt
	var badPool []candidate
	for _, c := range cands {
		if c.tried {
			continue
		}
		if c.next.Dead() {
			continue
		}
		if c.dist >= 0 && c.dist < bestDist {
			bestDist = c.dist
			bestOpt = []candidate{c}
		} else if c.dist >= 0 && c.dist == bestDist {
			bestOpt = append(bestOpt, c)
		}
		if c.dist < 0 || (curDist >= 0 && c.dist >= curDist) || c.visited {
			badPool = append(badPool, c)
		}
	}

	var chosen candidate
	hasChosen := false

	// Human-like mistake: with deterministic seeded mistakeRate, pick an exploratory non-optimal move when available (but never when 1 move away from winning!).
	if len(badPool) > 0 && bestDist > 0 && rng != nil && rng.Float64() < b.mistakeRate {
		chosen = badPool[rng.Intn(len(badPool))]
		hasChosen = true
	} else if len(bestOpt) > 0 {
		if rng != nil && len(bestOpt) > 1 {
			chosen = bestOpt[rng.Intn(len(bestOpt))]
		} else {
			chosen = bestOpt[0]
		}
		hasChosen = true
	} else {
		// Fallback to any untried non-dead move, or undo if every move from here was marked bad.
		for _, c := range cands {
			if !c.tried && !c.next.Dead() {
				chosen = c
				hasChosen = true
				break
			}
		}
		if !hasChosen && canUndo && len(b.stack) > 0 {
			top := b.stack[len(b.stack)-1]
			b.recordBadTransition(top.fromKey, top.move)
			b.popStack()
			return b.undoAction
		}
		if !hasChosen {
			chosen = cands[0]
			hasChosen = true
		}
	}

	if !hasChosen {
		return ""
	}
	b.lastMoveFrom = curKey
	b.lastMove = chosen.move
	b.lastMoveScore = curScore
	b.lastMoveDist = curDist
	b.hasLastMove = true
	return b.actionForMove(chosen.move)
}

func (b *PuzzleSolver) popStack() {
	if len(b.stack) > 0 {
		b.stack = b.stack[:len(b.stack)-1]
	}
}

func (b *PuzzleSolver) recordBadTransition(stateKey string, mv PuzzleMove) {
	if b.badTransitions[stateKey] == nil {
		b.badTransitions[stateKey] = make(map[PuzzleMove]bool)
	}
	b.badTransitions[stateKey][mv] = true
}

func (b *PuzzleSolver) shortestDist(s PuzzleState) int {
	if s == nil || s.Dead() {
		return -1
	}
	if s.Solved() {
		return 0
	}
	k := s.Key()
	if d, ok := b.optDistCache[k]; ok {
		return d
	}
	// Keep human-mode lookahead fast by capping per-state distance probes.
	limit := min(b.stateLimit, 20_000)
	res := SolvePuzzleBFS(s, limit)
	d := -1
	if res.Solved {
		d = res.Moves
	}
	b.optDistCache[k] = d
	return d
}

func (b *PuzzleSolver) actionForMove(mv PuzzleMove) string {
	switch mv {
	case "up":
		return b.upAction
	case "down":
		return b.downAction
	case "left":
		return b.leftAction
	case "right":
		return b.rightAction
	default:
		return string(mv)
	}
}

func (b *PuzzleSolver) moveInput(mv PuzzleMove) InputState {
	act := b.actionForMove(mv)
	if act == "" {
		return InputState{}
	}
	return InputState{Actions: map[string]bool{act: true}}
}

func puzzleStateScore(s PuzzleState) float64 {
	if s == nil {
		return 0
	}
	if sc, ok := s.(PuzzleScorer); ok {
		v := sc.ProgressScore()
		if !isFinite(v) {
			return 0
		}
		return v
	}
	if s.Solved() {
		return 1
	}
	return 0
}

// PuzzleLevelReport holds the BFS solver and human-like playtest findings for one level in a pack.
type PuzzleLevelReport struct {
	ID                  string
	Name                string
	Par                 int
	IntendedMoves       int
	Solve               PuzzleSolveResult
	Human               PuzzleHumanResult
	ShorterThanIntended bool
	DifficultyDip       bool
}

// PuzzlePackReport summarizes BFS solvability, difficulty ordering, unintended shorter solutions and human-like moves over par across a puzzle pack.
type PuzzlePackReport struct {
	Levels             []PuzzleLevelReport
	SolvableCount      int
	TotalLevels        int
	DifficultyDips     int
	ShorterSolutions   int
	MedianMovesOverPar float64
}

// EvaluatePuzzlePack runs the BFS shortest-solution solver (bounded by stateLimit) and the human-like undo player across levels using deterministic seed.
func EvaluatePuzzlePack(levels []PuzzleLevel, cfg BotConfig, seed uint64) (*PuzzlePackReport, error) {
	rawBot, err := NewPuzzleSolver(cfg)
	if err != nil {
		return nil, err
	}
	solver := rawBot.(*PuzzleSolver)
	rng := random.NewStreams(seed).Stream("playtest.puzzle")

	rep := &PuzzlePackReport{
		Levels:      make([]PuzzleLevelReport, 0, len(levels)),
		TotalLevels: len(levels),
	}

	prevShortest := -1
	var overPars []float64

	for _, lvl := range levels {
		intended := lvl.EffectiveIntendedMoves()
		solveRes := SolvePuzzleBFS(lvl.Start, solver.stateLimit)

		lr := PuzzleLevelReport{
			ID:            lvl.ID,
			Name:          lvl.Name,
			Par:           lvl.Par,
			IntendedMoves: intended,
			Solve:         solveRes,
		}
		if solveRes.Solved {
			rep.SolvableCount++
			if intended > 0 && solveRes.Moves < intended {
				lr.ShorterThanIntended = true
				rep.ShorterSolutions++
			}
			if prevShortest >= 0 && solveRes.Moves < prevShortest {
				lr.DifficultyDip = true
				rep.DifficultyDips++
			}
			prevShortest = solveRes.Moves
		}

		// Run human-like player on this level.
		humanRes := runHumanOnLevel(lvl, cfg, rng)
		lr.Human = humanRes
		if humanRes.Solved {
			overPars = append(overPars, float64(humanRes.MovesOverPar))
		}
		rep.Levels = append(rep.Levels, lr)
	}

	if len(overPars) > 0 {
		slices.Sort(overPars)
		rep.MedianMovesOverPar = cleanZero(percentileFloat(overPars, 50))
	}
	return rep, nil
}

func runHumanOnLevel(lvl PuzzleLevel, cfg BotConfig, rng *random.Stream) PuzzleHumanResult {
	humanCfg := cloneBot(cfg)
	if humanCfg.Params == nil {
		humanCfg.Params = map[string]any{}
	}
	humanCfg.Params["mode"] = puzzleModeHuman
	rawBot, err := NewPuzzleSolver(humanCfg)
	if err != nil || lvl.Start == nil {
		return PuzzleHumanResult{}
	}
	bot := rawBot.(*PuzzleSolver)

	history := []PuzzleState{lvl.Start}
	var actions []string
	undos := 0
	maxSteps := bot.maxHumanMoves * 2

	for step := 0; step < maxSteps; step++ {
		cur := history[len(history)-1]
		if cur.Solved() {
			moves := len(history) - 1
			return PuzzleHumanResult{
				Solved:       true,
				Moves:        moves,
				MovesOverPar: moves - lvl.Par,
				Undos:        undos,
				Actions:      actions,
			}
		}
		in := bot.Decide(step, time.Second/60, PuzzleObservation{
			LevelID:       lvl.ID,
			State:         cur,
			CanUndo:       len(history) > 1,
			ParMoves:      lvl.Par,
			IntendedMoves: lvl.EffectiveIntendedMoves(),
		}, rng)
		act := firstPressedAction(in.Actions)
		if act == "" {
			continue
		}
		actions = append(actions, act)
		if act == bot.undoAction {
			if len(history) > 1 {
				history = history[:len(history)-1]
				undos++
			}
			continue
		}
		mv := moveForAction(act, bot)
		next, changed := cur.Apply(mv)
		if changed && next != nil {
			history = append(history, next)
		}
	}
	cur := history[len(history)-1]
	moves := len(history) - 1
	return PuzzleHumanResult{
		Solved:       cur.Solved(),
		Moves:        moves,
		MovesOverPar: moves - lvl.Par,
		Undos:        undos,
		Actions:      actions,
	}
}

func firstPressedAction(actions map[string]bool) string {
	for _, k := range []string{"undo", "up", "right", "down", "left"} {
		if actions[k] {
			return k
		}
	}
	var best string
	for k, v := range actions {
		if v && (best == "" || k < best) {
			best = k
		}
	}
	return best
}

func moveForAction(act string, bot *PuzzleSolver) PuzzleMove {
	switch act {
	case bot.upAction:
		return "up"
	case bot.downAction:
		return "down"
	case bot.leftAction:
		return "left"
	case bot.rightAction:
		return "right"
	default:
		return PuzzleMove(act)
	}
}

// NewPuzzlePackSim returns a SimFactory that runs the "puzzle-solver" bot through playtest.Run across every level in levels, recording replay inputs, BFS shortest solutions, difficulty dips, shorter-than-intended solutions, and human-like moves over par on metrics.
func NewPuzzlePackSim(levels []PuzzleLevel, botCfg BotConfig) SimFactory {
	return func(seed uint64, src replay.ActionSource, metrics *Collector) (Sim, error) {
		rawBot, err := NewPuzzleSolver(botCfg)
		if err != nil {
			return Sim{}, err
		}
		templateBot := rawBot.(*PuzzleSolver)

		// Run deterministic BFS analysis once per level up front.
		bfsResults := make([]PuzzleSolveResult, len(levels))
		solvable := 0
		dips := 0
		shorter := 0
		prevShortest := -1

		metrics.ConfigureSeries("shortest_solution", "Shortest solution per level", "moves", AggMin, nil)
		for i, lvl := range levels {
			res := SolvePuzzleBFS(lvl.Start, templateBot.stateLimit)
			bfsResults[i] = res
			intended := lvl.EffectiveIntendedMoves()
			if res.Solved {
				solvable++
				metrics.SetSeries("shortest_solution", lvl.ID, float64(res.Moves))
				if intended > 0 && res.Moves < intended {
					shorter++
					metrics.FlagSeries("shortest_solution", lvl.ID, FlagWarning, fmt.Sprintf("solved in %d moves (intended %d)", res.Moves, intended))
					metrics.Event("shorter_solution", float64(i+1), float64(res.Moves), lvl.ID)
				}
				if prevShortest >= 0 && res.Moves < prevShortest {
					dips++
					metrics.FlagSeries("shortest_solution", lvl.ID, FlagProblem, fmt.Sprintf("difficulty dip from %d to %d moves", prevShortest, res.Moves))
					metrics.Event("difficulty_dip", float64(i+1), float64(res.Moves), lvl.ID)
				}
				prevShortest = res.Moves
			} else {
				metrics.SetSeries("shortest_solution", lvl.ID, 0)
				metrics.FlagSeries("shortest_solution", lvl.ID, FlagProblem, "unsolvable within state limit")
			}
		}

		metrics.SetCounter("levels_total", float64(len(levels)))
		metrics.SetCounter("levels_solved", float64(solvable))
		metrics.SetCounter("difficulty_dips", float64(dips))
		metrics.SetCounter("shorter_solutions", float64(shorter))

		lvlIdx := 0
		var history []PuzzleState
		levelSteps := 0
		var overPars []float64
		totalUndos := 0

		loadLevel := func(i int) {
			if i >= len(levels) || levels[i].Start == nil {
				history = nil
				levelSteps = 0
				return
			}
			history = []PuzzleState{levels[i].Start}
			levelSteps = 0
		}
		loadLevel(0)

		finishPack := func() {
			if len(overPars) > 0 {
				slices.Sort(overPars)
				metrics.SetCounter("moves_over_par", cleanZero(percentileFloat(overPars, 50)))
			}
			metrics.SetCounter("human_undos", float64(totalUndos))
			if solvable == len(levels) {
				metrics.Finish()
			} else {
				metrics.Fail(fmt.Sprintf("%d of %d levels unsolvable", len(levels)-solvable, len(levels)))
			}
		}

		if len(levels) == 0 {
			finishPack()
		}

		observe := func() any {
			if lvlIdx >= len(levels) || len(history) == 0 {
				return nil
			}
			lvl := levels[lvlIdx]
			return PuzzleObservation{
				LevelID:       lvl.ID,
				State:         history[len(history)-1],
				CanUndo:       len(history) > 1,
				ParMoves:      lvl.Par,
				IntendedMoves: lvl.EffectiveIntendedMoves(),
			}
		}

		step := func(_ time.Duration) {
			src.Step()
			if lvlIdx >= len(levels) {
				return
			}
			lvl := levels[lvlIdx]
			if len(history) == 0 || history[len(history)-1] == nil {
				lvlIdx++
				if lvlIdx >= len(levels) {
					finishPack()
				} else {
					loadLevel(lvlIdx)
				}
				return
			}
			cur := history[len(history)-1]
			levelSteps++

			switch {
			case src.JustPressed(templateBot.undoAction):
				if len(history) > 1 {
					history = history[:len(history)-1]
					totalUndos++
				}
			case src.JustPressed(templateBot.upAction):
				if next, ok := cur.Apply("up"); ok && next != nil {
					history = append(history, next)
				}
			case src.JustPressed(templateBot.downAction):
				if next, ok := cur.Apply("down"); ok && next != nil {
					history = append(history, next)
				}
			case src.JustPressed(templateBot.leftAction):
				if next, ok := cur.Apply("left"); ok && next != nil {
					history = append(history, next)
				}
			case src.JustPressed(templateBot.rightAction):
				if next, ok := cur.Apply("right"); ok && next != nil {
					history = append(history, next)
				}
			}

			cur = history[len(history)-1]
			if cur.Solved() || levelSteps >= templateBot.maxHumanMoves*2 {
				if cur.Solved() {
					moves := len(history) - 1
					overPars = append(overPars, float64(moves-lvl.Par))
				}
				lvlIdx++
				if lvlIdx >= len(levels) {
					finishPack()
				} else {
					loadLevel(lvlIdx)
				}
			}
		}

		return Sim{
			Step:    step,
			Observe: observe,
		}, nil
	}
}

package playtest

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/replay"
)

// ProfileCardAI is the built-in greedy 1-turn-lookahead card AI bot profile for cards2d templates.
const ProfileCardAI = "card-ai"

const (
	defaultCardMatchesPerPair    = 20
	maxCardMatchesPerPair        = 10_000
	defaultCardMaxTurns          = 60
	maxCardMaxTurns              = 10_000
	defaultCardMaxActionsPerTurn = 4
	maxCardMaxActionsPerTurn     = 1_000
	defaultCardMsPerNode         = 0.02
	defaultCardFlagWinRateAbove  = 60.0
	cardActionIndexAxis          = "action_index"
)

func init() {
	RegisterBot(ProfileCardAI, NewCardAI)
}

// CardAction identifies one legal action a CardGameState can perform.
type CardAction struct {
	// ID uniquely names this action within the current state (such as "play:kindle" or "end_turn").
	ID string
	// Action is the optional replay input action name (such as "play", "discard", "draw" or "end_turn").
	Action string
	// Index is an optional hand or target slot index.
	Index int
}

// CardGameState is a deterministic card game state evaluated by the greedy 1-turn-lookahead CardAI.
type CardGameState interface {
	// ActivePlayer returns the seat index (0 or 1) whose decision is next.
	ActivePlayer() int
	// Winner returns the winning seat index (0 or 1) and true when the match is over, (-1, true) for a draw, or (-1, false) while still playing.
	Winner() (player int, done bool)
	// Turn returns the current 1-based turn count.
	Turn() int
	// LegalActions returns the legal actions available to ActivePlayer in deterministic order.
	LegalActions() []CardAction
	// Apply returns the next immutable or cloned state resulting from taking action.
	Apply(action CardAction) CardGameState
	// Evaluate scores the state from player's perspective (higher is better for player).
	Evaluate(player int) float64
}

// CardObservation is the per-step observation supplied to CardAI during playtest.Run.
type CardObservation struct {
	// State is the current card game state awaiting ActivePlayer's decision.
	State CardGameState
}

// CardDecision is the result of one greedy 1-turn-lookahead evaluation by CardAI.
type CardDecision struct {
	// Action is the chosen CardAction.
	Action CardAction
	// ActionIndex is the 0-based index of Action in State.LegalActions().
	ActionIndex int
	// Score is the 1-turn-lookahead evaluation score for the active player.
	Score float64
	// NodesEvaluated is the deterministic count of state transitions evaluated during this decision.
	NodesEvaluated int
	// ThinkMs is the deterministic think-time estimate in milliseconds (NodesEvaluated * msPerNode).
	ThinkMs float64
}

// CardMatchFactory constructs a fresh deterministic match between deckA (seat 0) and deckB (seat 1), where firstPlayer (0 or 1) takes the first turn, seeded by seed.
type CardMatchFactory func(deckA, deckB string, firstPlayer int, seed uint64) (CardGameState, error)

// CardAI is the built-in "card-ai" bot: a greedy player that looks one turn ahead (our turn plus the opponent's best greedy reply) with deterministic tie-breaking and deterministic think-time accounting.
type CardAI struct {
	matchesPerPair    int
	maxTurns          int
	maxActionsPerTurn int
	msPerNode         float64
	flagWinRateAbove  float64

	lastDecision CardDecision
}

// NewCardAI constructs a CardAI from a playtest BotConfig.
//
// Supported optional Bot.Params:
//   - "matchesPerPair" (number, default 20): matches played per unordered deck pair, alternating first player.
//   - "maxTurns" (number, default 60): maximum turns per match before declaring a draw.
//   - "maxActionsPerTurn" (number, default 4): greedy lookahead action horizon within a single player's turn.
//   - "msPerNode" (number, default 0.02): deterministic millisecond cost per evaluated state node for think-time reporting.
//   - "flagWinRateAbove" (number, default 60): win-rate percentage threshold flagged in the deck-pair matrix.
func NewCardAI(cfg BotConfig) (Bot, error) {
	b := &CardAI{
		matchesPerPair:    defaultCardMatchesPerPair,
		maxTurns:          defaultCardMaxTurns,
		maxActionsPerTurn: defaultCardMaxActionsPerTurn,
		msPerNode:         defaultCardMsPerNode,
		flagWinRateAbove:  defaultCardFlagWinRateAbove,
	}
	if cfg.Params != nil {
		if v, ok := cfg.Params["matchesPerPair"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 1 || n > maxCardMatchesPerPair {
				return nil, fmt.Errorf("playtest: card-ai param matchesPerPair must be in [1, %d]", maxCardMatchesPerPair)
			}
			b.matchesPerPair = int(math.Round(n))
		}
		if v, ok := cfg.Params["maxTurns"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 1 || n > maxCardMaxTurns {
				return nil, fmt.Errorf("playtest: card-ai param maxTurns must be in [1, %d]", maxCardMaxTurns)
			}
			b.maxTurns = int(math.Round(n))
		}
		if v, ok := cfg.Params["maxActionsPerTurn"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 1 || n > maxCardMaxActionsPerTurn {
				return nil, fmt.Errorf("playtest: card-ai param maxActionsPerTurn must be in [1, %d]", maxCardMaxActionsPerTurn)
			}
			b.maxActionsPerTurn = int(math.Round(n))
		}
		if v, ok := cfg.Params["msPerNode"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 0 {
				return nil, fmt.Errorf("playtest: card-ai param msPerNode must be a non-negative number")
			}
			b.msPerNode = n
		}
		if v, ok := cfg.Params["flagWinRateAbove"]; ok {
			n, ok := asFloat64(v)
			if !ok || n <= 0 || n > 100 {
				return nil, fmt.Errorf("playtest: card-ai param flagWinRateAbove must be in (0, 100]")
			}
			b.flagWinRateAbove = n
		}
	}
	return b, nil
}

// Channels declares the default action and axis names that card-ai drives.
func (b *CardAI) Channels() (actions, axes []string) {
	return []string{"play", "discard", "draw", "end_turn"}, []string{cardActionIndexAxis}
}

// LastDecision returns the most recent CardDecision produced by ChooseAction or Decide.
func (b *CardAI) LastDecision() CardDecision {
	return b.lastDecision
}

// ChooseAction picks the highest-scoring legal action for state.ActivePlayer() using greedy 1-turn lookahead (completing our turn greedily, then simulating the opponent's best greedy reply turn).
func (b *CardAI) ChooseAction(state CardGameState, rng *random.Stream) CardDecision {
	if state == nil {
		return CardDecision{}
	}
	actions := state.LegalActions()
	if len(actions) == 0 {
		return CardDecision{}
	}
	player := state.ActivePlayer()
	nodes := 0

	bestScore := math.Inf(-1)
	var bestIndices []int

	for i, act := range actions {
		next := state.Apply(act)
		nodes++
		if next == nil {
			continue
		}
		score := b.evaluateWithLookahead(next, player, b.maxActionsPerTurn-1, &nodes)
		const eps = 1e-9
		if score > bestScore+eps {
			bestScore = score
			bestIndices = []int{i}
		} else if math.Abs(score-bestScore) <= eps {
			bestIndices = append(bestIndices, i)
		}
	}

	chosenIdx := 0
	if len(bestIndices) > 0 {
		if rng != nil && len(bestIndices) > 1 {
			chosenIdx = bestIndices[rng.Intn(len(bestIndices))]
		} else {
			chosenIdx = bestIndices[0]
		}
	}

	if !isFinite(bestScore) {
		bestScore = 0
	}

	dec := CardDecision{
		Action:         actions[chosenIdx],
		ActionIndex:    chosenIdx,
		Score:          cleanZero(bestScore),
		NodesEvaluated: nodes,
		ThinkMs:        cleanZero(float64(nodes) * b.msPerNode),
	}
	b.lastDecision = dec
	return dec
}

func (b *CardAI) evaluateWithLookahead(state CardGameState, player int, remainingOwnActions int, nodes *int) float64 {
	if score, terminal := terminalScore(state, player); terminal {
		return score
	}
	// Finish our own turn greedily (if the game allows multiple actions per turn) so we compare full-turn outcomes.
	cur := state
	for remainingOwnActions > 0 && cur.ActivePlayer() == player {
		if score, terminal := terminalScore(cur, player); terminal {
			return score
		}
		acts := cur.LegalActions()
		if len(acts) == 0 {
			break
		}
		bestNext := cur
		bestVal := math.Inf(-1)
		for _, a := range acts {
			cand := cur.Apply(a)
			*nodes++
			if cand == nil {
				continue
			}
			v := immediateScore(cand, player)
			if v > bestVal {
				bestVal = v
				bestNext = cand
			}
		}
		cur = bestNext
		remainingOwnActions--
	}

	if score, terminal := terminalScore(cur, player); terminal {
		return score
	}

	// Look one turn ahead: simulate the opponent's best greedy reply turn.
	opp := cur.ActivePlayer()
	if opp != player {
		oppActions := b.maxActionsPerTurn
		for oppActions > 0 && cur.ActivePlayer() == opp {
			if score, terminal := terminalScore(cur, player); terminal {
				return score
			}
			acts := cur.LegalActions()
			if len(acts) == 0 {
				break
			}
			bestNext := cur
			bestOppVal := math.Inf(-1)
			for _, a := range acts {
				cand := cur.Apply(a)
				*nodes++
				if cand == nil {
					continue
				}
				v := immediateScore(cand, opp)
				if v > bestOppVal {
					bestOppVal = v
					bestNext = cand
				}
			}
			cur = bestNext
			oppActions--
		}
	}

	if score, terminal := terminalScore(cur, player); terminal {
		return score
	}
	return cur.Evaluate(player)
}

func terminalScore(s CardGameState, player int) (float64, bool) {
	w, done := s.Winner()
	if !done {
		return 0, false
	}
	switch {
	case w == player:
		return 1e6 - float64(s.Turn()), true
	case w < 0:
		return 0, true
	default:
		return -1e6 + float64(s.Turn()), true
	}
}

func immediateScore(s CardGameState, player int) float64 {
	if sc, done := terminalScore(s, player); done {
		return sc
	}
	return s.Evaluate(player)
}

// Decide evaluates a CardObservation and encodes the chosen action index into InputState for replay recording.
func (b *CardAI) Decide(_ int, _ time.Duration, rawObs any, rng *random.Stream) InputState {
	var state CardGameState
	switch v := rawObs.(type) {
	case CardObservation:
		state = v.State
	case *CardObservation:
		if v != nil {
			state = v.State
		}
	case CardGameState:
		state = v
	default:
		return InputState{}
	}
	if state == nil {
		return InputState{}
	}
	if _, done := state.Winner(); done {
		return InputState{}
	}
	dec := b.ChooseAction(state, rng)
	actions := map[string]bool{}
	if dec.Action.Action != "" {
		actions[dec.Action.Action] = true
	}
	return InputState{
		Actions: actions,
		Axes: map[string]float64{
			cardActionIndexAxis: float64(dec.ActionIndex + 1),
		},
	}
}

// CardMatchResult holds the outcome of one AI-vs-AI card match.
type CardMatchResult struct {
	DeckA         string
	DeckB         string
	FirstPlayer   int
	Winner        int
	Turns         int
	Decisions     int
	MedianThinkMs float64
	P99ThinkMs    float64
}

// CardTournamentReport summarizes a complete round-robin deck-pair tournament between two greedy 1-turn-lookahead CardAIs.
type CardTournamentReport struct {
	Decks              []string
	MatchesPlayed      int
	FirstPlayerWins    int
	FirstPlayerWinRate float64
	MedianMatchTurns   float64
	MinMatchTurns      int
	MaxMatchTurns      int
	MostLopsidedRate   float64
	MostLopsidedWinner string
	MostLopsidedLoser  string
	MedianThinkMs      float64
	P99ThinkMs         float64
	WinRateMatrix      MatrixChart
	Matches            []CardMatchResult
}

func validateCardDecks(decks []string) error {
	if len(decks) < 2 {
		return fmt.Errorf("playtest: card tournament requires at least 2 decks, got %d", len(decks))
	}
	seen := make(map[string]struct{}, len(decks))
	for i, d := range decks {
		if strings.TrimSpace(d) == "" {
			return fmt.Errorf("playtest: card tournament deck %d name must be non-empty", i)
		}
		if _, dup := seen[d]; dup {
			return fmt.Errorf("playtest: card tournament duplicate deck name %q", d)
		}
		seen[d] = struct{}{}
	}
	return nil
}

// RunCardTournament runs every unordered pair of decks matchesPerPair times (from cfg.Params["matchesPerPair"]), alternating who goes first, using two greedy 1-turn-lookahead CardAI players seeded deterministically from seed.
func RunCardTournament(decks []string, factory CardMatchFactory, cfg BotConfig, seed uint64) (*CardTournamentReport, error) {
	if err := validateCardDecks(decks); err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, fmt.Errorf("playtest: nil card match factory")
	}
	rawBot, err := NewCardAI(cfg)
	if err != nil {
		return nil, err
	}
	aiCfg := rawBot.(*CardAI)

	streams := random.NewStreams(seed)
	rep := &CardTournamentReport{
		Decks: slices.Clone(decks),
	}

	pairWins := make([][]int, len(decks))
	pairPlayed := make([][]int, len(decks))
	for i := range decks {
		pairWins[i] = make([]int, len(decks))
		pairPlayed[i] = make([]int, len(decks))
	}

	var allTurns []float64
	var allThinkMs []float64
	matchIdx := 0

	for i := 0; i < len(decks); i++ {
		for j := i + 1; j < len(decks); j++ {
			for m := 0; m < aiCfg.matchesPerPair; m++ {
				firstPlayer := m % 2
				matchSeed := seed ^ (uint64(i+1)*1_000_003 + uint64(j+1)*10_007 + uint64(m+1)*997)
				state, err := factory(decks[i], decks[j], firstPlayer, matchSeed)
				if err != nil {
					return nil, fmt.Errorf("playtest: match %s vs %s #%d: %w", decks[i], decks[j], m+1, err)
				}
				if state == nil {
					return nil, fmt.Errorf("playtest: match %s vs %s #%d: factory returned nil state", decks[i], decks[j], m+1)
				}
				p0AI := &CardAI{
					maxActionsPerTurn: aiCfg.maxActionsPerTurn,
					msPerNode:         aiCfg.msPerNode,
				}
				p1AI := &CardAI{
					maxActionsPerTurn: aiCfg.maxActionsPerTurn,
					msPerNode:         aiCfg.msPerNode,
				}
				p0RNG := streams.Stream(fmt.Sprintf("match.%d.p0", matchIdx))
				p1RNG := streams.Stream(fmt.Sprintf("match.%d.p1", matchIdx))
				matchIdx++

				var matchThink []float64
				maxActions := aiCfg.maxTurns * max(aiCfg.maxActionsPerTurn, 1) * 2
				for step := 0; step < maxActions; step++ {
					if _, done := state.Winner(); done || state.Turn() > aiCfg.maxTurns {
						break
					}
					var dec CardDecision
					if state.ActivePlayer() == 0 {
						dec = p0AI.ChooseAction(state, p0RNG)
					} else {
						dec = p1AI.ChooseAction(state, p1RNG)
					}
					if len(state.LegalActions()) == 0 {
						break
					}
					matchThink = append(matchThink, dec.ThinkMs)
					allThinkMs = append(allThinkMs, dec.ThinkMs)
					next := state.Apply(dec.Action)
					if next == nil {
						break
					}
					state = next
				}

				winner, done := state.Winner()
				if !done {
					// Tiebreak by board evaluation when hitting maxTurns.
					s0 := state.Evaluate(0)
					s1 := state.Evaluate(1)
					if s0 > s1 {
						winner = 0
					} else if s1 > s0 {
						winner = 1
					} else {
						winner = -1
					}
				}
				turns := state.Turn()
				if turns < 1 {
					turns = 1
				}
				rep.MatchesPlayed++
				if winner == firstPlayer {
					rep.FirstPlayerWins++
				}
				pairPlayed[i][j]++
				pairPlayed[j][i]++
				if winner == 0 {
					pairWins[i][j]++
				} else if winner == 1 {
					pairWins[j][i]++
				}
				allTurns = append(allTurns, float64(turns))
				slices.Sort(matchThink)
				rep.Matches = append(rep.Matches, CardMatchResult{
					DeckA:         decks[i],
					DeckB:         decks[j],
					FirstPlayer:   firstPlayer,
					Winner:        winner,
					Turns:         turns,
					Decisions:     len(matchThink),
					MedianThinkMs: cleanZero(percentileFloat(matchThink, 50)),
					P99ThinkMs:    cleanZero(percentileFloat(matchThink, 99)),
				})
			}
		}
	}

	if rep.MatchesPlayed > 0 {
		rep.FirstPlayerWinRate = cleanZero(100.0 * float64(rep.FirstPlayerWins) / float64(rep.MatchesPlayed))
	}
	if len(allTurns) > 0 {
		slices.Sort(allTurns)
		rep.MedianMatchTurns = cleanZero(percentileFloat(allTurns, 50))
		rep.MinMatchTurns = int(allTurns[0])
		rep.MaxMatchTurns = int(allTurns[len(allTurns)-1])
	}
	if len(allThinkMs) > 0 {
		slices.Sort(allThinkMs)
		rep.MedianThinkMs = cleanZero(percentileFloat(allThinkMs, 50))
		rep.P99ThinkMs = cleanZero(percentileFloat(allThinkMs, 99))
	}

	flagAbove := aiCfg.flagWinRateAbove
	cells := make([][]*float64, len(decks))
	for i := range decks {
		cells[i] = make([]*float64, len(decks))
		for j := range decks {
			if i == j || pairPlayed[i][j] == 0 {
				continue
			}
			rate := cleanZero(100.0 * float64(pairWins[i][j]) / float64(pairPlayed[i][j]))
			v := rate
			cells[i][j] = &v
			if rate > rep.MostLopsidedRate {
				rep.MostLopsidedRate = rate
				rep.MostLopsidedWinner = decks[i]
				rep.MostLopsidedLoser = decks[j]
			}
		}
	}
	rep.WinRateMatrix = MatrixChart{
		Name:      "deck_win_rates",
		Label:     "Win rate by deck pair",
		Unit:      "%",
		FlagAbove: &flagAbove,
		Labels:    slices.Clone(decks),
		Cells:     cells,
	}
	return rep, nil
}

// NewCardTournamentSim returns a SimFactory that executes a round-robin deck-pair tournament inside playtest.Run, recording every AI decision through replay.Recorder and reporting first-player win rate, match length, most lopsided pair, think time, and the win-rate matrix on metrics.
func NewCardTournamentSim(decks []string, factory CardMatchFactory, botCfg BotConfig) SimFactory {
	return func(seed uint64, src replay.ActionSource, metrics *Collector) (Sim, error) {
		if err := validateCardDecks(decks); err != nil {
			return Sim{}, err
		}
		if factory == nil {
			return Sim{}, fmt.Errorf("playtest: nil card match factory")
		}
		rawBot, err := NewCardAI(botCfg)
		if err != nil {
			return Sim{}, err
		}
		aiCfg := rawBot.(*CardAI)

		type pairMatch struct {
			i, j        int
			matchNum    int
			firstPlayer int
			matchSeed   uint64
		}
		var schedule []pairMatch
		for i := 0; i < len(decks); i++ {
			for j := i + 1; j < len(decks); j++ {
				for m := 0; m < aiCfg.matchesPerPair; m++ {
					schedule = append(schedule, pairMatch{
						i:           i,
						j:           j,
						matchNum:    m,
						firstPlayer: m % 2,
						matchSeed:   seed ^ (uint64(i+1)*1_000_003 + uint64(j+1)*10_007 + uint64(m+1)*997),
					})
				}
			}
		}

		flagAbove := aiCfg.flagWinRateAbove
		metrics.ConfigureMatrix("deck_win_rates", "Win rate by deck pair", "%", &flagAbove, decks)

		pairWins := make([][]float64, len(decks))
		pairTotal := make([][]float64, len(decks))
		for i := range decks {
			pairWins[i] = make([]float64, len(decks))
			pairTotal[i] = make([]float64, len(decks))
		}

		var (
			schedIdx        int
			state           CardGameState
			matchActions    int
			allTurns        []float64
			allThinkMs      []float64
			firstPlayerWins float64
			matchesPlayed   float64
		)

		loadMatch := func(idx int) error {
			if idx >= len(schedule) {
				state = nil
				return nil
			}
			pm := schedule[idx]
			st, err := factory(decks[pm.i], decks[pm.j], pm.firstPlayer, pm.matchSeed)
			if err != nil {
				return err
			}
			if st == nil {
				return fmt.Errorf("playtest: match %s vs %s #%d: factory returned nil state", decks[pm.i], decks[pm.j], pm.matchNum+1)
			}
			state = st
			matchActions = 0
			return nil
		}
		if err := loadMatch(0); err != nil {
			return Sim{}, err
		}

		finishTournament := func() {
			metrics.SetCounter("matches_played", matchesPlayed)
			metrics.SetCounter("first_player_wins", firstPlayerWins)
			if len(allTurns) > 0 {
				slices.Sort(allTurns)
				metrics.SetCounter("match_turns", cleanZero(percentileFloat(allTurns, 50)))
			}
			if len(allThinkMs) > 0 {
				slices.Sort(allThinkMs)
				metrics.SetCounter("ai_think_ms", cleanZero(percentileFloat(allThinkMs, 50)))
				metrics.SetCounter("ai_think_p99_ms", cleanZero(percentileFloat(allThinkMs, 99)))
			}
			var maxRate float64
			for i := range decks {
				for j := range decks {
					if i == j || pairTotal[i][j] <= 0 {
						continue
					}
					rate := 100.0 * pairWins[i][j] / pairTotal[i][j]
					if rate > maxRate {
						maxRate = rate
					}
				}
			}
			metrics.SetCounter("max_pair_win_rate", cleanZero(maxRate))
			metrics.Finish()
		}

		if len(schedule) == 0 {
			finishTournament()
		}

		maxActionsPerMatch := aiCfg.maxTurns * max(aiCfg.maxActionsPerTurn, 1) * 2

		observe := func() any {
			if state == nil {
				return nil
			}
			return CardObservation{State: state}
		}

		step := func(_ time.Duration) {
			src.Step()
			if state == nil || schedIdx >= len(schedule) {
				return
			}
			pm := schedule[schedIdx]
			legal := state.LegalActions()
			if _, done := state.Winner(); !done && len(legal) > 0 && state.Turn() <= aiCfg.maxTurns && matchActions < maxActionsPerMatch {
				idx := int(math.Round(src.Axis(cardActionIndexAxis))) - 1
				if idx < 0 || idx >= len(legal) {
					idx = 0
				}
				// Record deterministic think time proportional to legal branching and lookahead depth.
				nodes := max(len(legal), 1) * (1 + aiCfg.maxActionsPerTurn)
				allThinkMs = append(allThinkMs, cleanZero(float64(nodes)*aiCfg.msPerNode))
				next := state.Apply(legal[idx])
				matchActions++
				if next != nil {
					state = next
				}
			}

			winner, done := state.Winner()
			if done || len(state.LegalActions()) == 0 || state.Turn() > aiCfg.maxTurns || matchActions >= maxActionsPerMatch {
				if !done {
					s0 := state.Evaluate(0)
					s1 := state.Evaluate(1)
					if s0 > s1 {
						winner = 0
					} else if s1 > s0 {
						winner = 1
					} else {
						winner = -1
					}
				}
				turns := state.Turn()
				if turns < 1 {
					turns = 1
				}
				matchesPlayed++
				if winner == pm.firstPlayer {
					firstPlayerWins++
				}
				allTurns = append(allTurns, float64(turns))
				pairTotal[pm.i][pm.j]++
				pairTotal[pm.j][pm.i]++
				var wA, wB float64
				if winner == 0 {
					wA = 1
					pairWins[pm.i][pm.j]++
				} else if winner == 1 {
					wB = 1
					pairWins[pm.j][pm.i]++
				}
				metrics.RecordMatchup("deck_win_rates", decks[pm.i], decks[pm.j], wA, 1)
				metrics.RecordMatchup("deck_win_rates", decks[pm.j], decks[pm.i], wB, 1)

				schedIdx++
				if schedIdx >= len(schedule) {
					finishTournament()
					return
				}
				if err := loadMatch(schedIdx); err != nil {
					metrics.Fail(err.Error())
					state = nil
				}
			}
		}

		return Sim{
			Step:    step,
			Observe: observe,
		}, nil
	}
}

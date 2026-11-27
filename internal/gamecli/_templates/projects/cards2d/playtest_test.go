package main

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/cards"
	"github.com/nsgo-dev/game/pkg/playtest"
	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/replay"
)

// duelCard is one card from rules.yaml (suit index and rank 1..Rules.Ranks) in a two-player Suit Ladders duel.
type duelCard struct {
	suit int
	rank int
}

// duelSeat holds one player's focus suit, hand and score in a shared-table Suit Ladders duel.
type duelSeat struct {
	focusSuit int
	hand      []duelCard
	score     int
}

// suitLadderDuelState adapts Suit Ladders' rules.yaml and board.yaml to playtest.CardGameState for the card-ai tournament bot.
// Two players share one shuffled deck of all suits and ranks from rules.yaml, four suit ladders climbing 1..Rules.Ranks, and draw back up to board.yaml's hand capacity.
// Climbing any card scores 1 point; climbing a card of your focus suit scores 2 points. Each turn a player climbs up to two fitting cards (or discards one to cycle), ending once all ladders complete, the deck and redeals run out, or a player leads past the target after equal turns.
type suitLadderDuelState struct {
	rules           *Rules
	handCapacity    int
	targetScore     int
	firstPlayer     int
	active          int
	turn            int
	turnsPlayed     int
	actionsThisTurn int
	deck            []duelCard
	discard         []duelCard
	ladders         [maxSuits]int
	redeals         int
	players         [2]duelSeat
}

func loadCardPlaytestSetup() (*Rules, int, []string, error) {
	rules, err := parseRules(rulesYAML)
	if err != nil {
		return nil, 0, nil, err
	}
	doc, err := cards.ParseBoardDoc(boardYAML)
	if err != nil {
		return nil, 0, nil, err
	}
	tb, err := newTable(rules, doc)
	if err != nil {
		return nil, 0, nil, err
	}
	decks := make([]string, len(rules.Suits))
	for i, s := range rules.Suits {
		decks[i] = s.Name
	}
	return rules, tb.hand.Capacity, decks, nil
}

func suitIndexByName(rules *Rules, name string) int {
	for i, s := range rules.Suits {
		if s.Name == name {
			return i
		}
	}
	return 0
}

func newSuitLadderDuelMatch(rules *Rules, handCap int) playtest.CardMatchFactory {
	// Pair consecutive matches of each deck matchup as duplicate deals: the second match replays the first match's shuffled deck with seats and focus suits swapped, eliminating shuffle noise while preserving genuine first-player and deck-balance signals.
	type pairKey struct {
		deckA, deckB string
	}
	lastDeal := make(map[pairKey]uint64)

	return func(deckA, deckB string, firstPlayer int, seed uint64) (playtest.CardGameState, error) {
		key := pairKey{deckA: deckA, deckB: deckB}
		suitA := suitIndexByName(rules, deckA)
		suitB := suitIndexByName(rules, deckB)

		dealSeed := seed
		mirrored := false
		if firstPlayer == 0 {
			lastDeal[key] = seed
		} else if prev, ok := lastDeal[key]; ok {
			dealSeed = prev
			mirrored = true
		}

		all := make([]duelCard, 0, rules.cardCount())
		for si := range rules.Suits {
			for rank := 1; rank <= rules.Ranks; rank++ {
				all = append(all, duelCard{suit: si, rank: rank})
			}
		}
		random.Shuffle(random.New(dealSeed), all)
		if mirrored {
			// Swap focus suits in the duplicated deck so seat 1 receives the exact same focus-suit positions seat 0 had on the first half of the pair.
			for i := range all {
				switch all[i].suit {
				case suitA:
					all[i].suit = suitB
				case suitB:
					all[i].suit = suitA
				}
			}
		}

		st := &suitLadderDuelState{
			rules:        rules,
			handCapacity: handCap,
			targetScore:  rules.Ranks - 2,
			firstPlayer:  firstPlayer,
			active:       firstPlayer,
			turn:         1,
			redeals:      rules.Redeals,
		}
		h0 := slices.Clone(all[:min(handCap, len(all))])
		h1Start := min(handCap, len(all))
		h1End := min(h1Start+handCap, len(all))
		h1 := slices.Clone(all[h1Start:h1End])
		if mirrored {
			h0, h1 = h1, h0
		}
		st.players[0] = duelSeat{focusSuit: suitA, hand: h0}
		st.players[1] = duelSeat{focusSuit: suitB, hand: h1}
		st.deck = slices.Clone(all[h1End:])
		return st, nil
	}
}

func (s *suitLadderDuelState) clone() *suitLadderDuelState {
	cp := *s
	cp.deck = slices.Clone(s.deck)
	cp.discard = slices.Clone(s.discard)
	for p := range 2 {
		cp.players[p].hand = slices.Clone(s.players[p].hand)
	}
	return &cp
}

func (s *suitLadderDuelState) ActivePlayer() int { return s.active }
func (s *suitLadderDuelState) Turn() int         { return s.turn }

func (s *suitLadderDuelState) allLaddersComplete() bool {
	for i := range len(s.rules.Suits) {
		if s.ladders[i] < s.rules.Ranks {
			return false
		}
	}
	return true
}

func (s *suitLadderDuelState) anyCardFits() bool {
	for p := range 2 {
		for _, c := range s.players[p].hand {
			if c.rank == s.ladders[c.suit]+1 {
				return true
			}
		}
	}
	return false
}

func (s *suitLadderDuelState) Winner() (int, bool) {
	// Resolve only after both players have taken an equal number of turns so going first is a small edge, not a free extra turn.
	if s.turnsPlayed%2 != 0 {
		return -1, false
	}
	s0, s1 := s.players[0].score, s.players[1].score
	done := s0 >= s.targetScore || s1 >= s.targetScore || s.allLaddersComplete() || (len(s.deck) == 0 && (len(s.discard) == 0 || s.redeals == 0) && !s.anyCardFits())
	if !done {
		return -1, false
	}
	if s0 > s1 {
		return 0, true
	}
	if s1 > s0 {
		return 1, true
	}
	return -1, true
}

func (s *suitLadderDuelState) LegalActions() []playtest.CardAction {
	if _, done := s.Winner(); done {
		return nil
	}
	seat := &s.players[s.active]
	acts := make([]playtest.CardAction, 0, len(seat.hand)+1)
	for i, c := range seat.hand {
		if c.rank == s.ladders[c.suit]+1 {
			acts = append(acts, playtest.CardAction{
				ID:     fmt.Sprintf("play:%s-%d", s.rules.Suits[c.suit].Name, c.rank),
				Action: "play",
				Index:  i,
			})
		}
	}
	if s.actionsThisTurn > 0 && len(acts) == 0 {
		return []playtest.CardAction{{ID: "end_turn", Action: "end_turn"}}
	}
	if len(acts) == 0 {
		for i, c := range seat.hand {
			acts = append(acts, playtest.CardAction{
				ID:     fmt.Sprintf("discard:%s-%d", s.rules.Suits[c.suit].Name, c.rank),
				Action: "discard",
				Index:  i,
			})
		}
	}
	if len(acts) == 0 {
		acts = append(acts, playtest.CardAction{ID: "end_turn", Action: "end_turn"})
	}
	return acts
}

func (s *suitLadderDuelState) refill(seat *duelSeat) {
	for len(seat.hand) < s.handCapacity {
		if len(s.deck) == 0 {
			if len(s.discard) == 0 || s.redeals <= 0 {
				break
			}
			s.deck = append(s.deck, s.discard...)
			slices.Reverse(s.deck)
			s.discard = s.discard[:0]
			s.redeals--
		}
		top := len(s.deck) - 1
		seat.hand = append(seat.hand, s.deck[top])
		s.deck = s.deck[:top]
	}
}

func (s *suitLadderDuelState) endTurn() {
	s.refill(&s.players[s.active])
	s.actionsThisTurn = 0
	s.turnsPlayed++
	s.active = 1 - s.active
	if s.turnsPlayed%2 == 0 {
		s.turn++
	}
}

func (s *suitLadderDuelState) Apply(act playtest.CardAction) playtest.CardGameState {
	cp := s.clone()
	seat := &cp.players[cp.active]

	switch act.Action {
	case "play":
		if act.Index >= 0 && act.Index < len(seat.hand) {
			c := seat.hand[act.Index]
			if c.rank == cp.ladders[c.suit]+1 {
				seat.hand = slices.Delete(seat.hand, act.Index, act.Index+1)
				cp.ladders[c.suit] = c.rank
				pts := 1
				if c.suit == seat.focusSuit {
					pts = 2
				}
				seat.score += pts
				cp.actionsThisTurn++
				// Allow climbing up to 2 cards in one turn, then refill and pass turn.
				if cp.actionsThisTurn >= 2 {
					cp.endTurn()
				}
				return cp
			}
		}
	case "discard":
		if act.Index >= 0 && act.Index < len(seat.hand) {
			c := seat.hand[act.Index]
			seat.hand = slices.Delete(seat.hand, act.Index, act.Index+1)
			cp.discard = append(cp.discard, c)
			cp.endTurn()
			return cp
		}
	}
	cp.endTurn()
	return cp
}

func (s *suitLadderDuelState) Evaluate(player int) float64 {
	scoreSeat := func(p int) float64 {
		seat := &s.players[p]
		sc := float64(seat.score) * 10.0
		for _, c := range seat.hand {
			gap := c.rank - s.ladders[c.suit]
			w := 1.0
			if c.suit == seat.focusSuit {
				w = 1.8
			}
			if gap == 1 {
				sc += 3.0 * w
			} else if gap == 2 {
				sc += 1.2 * w
			}
		}
		return sc
	}
	return scoreSeat(player) - scoreSeat(1-player)
}

func loadCardPlaytestBot(t testing.TB) playtest.BotConfig {
	t.Helper()
	cfgPath := os.Getenv(playtest.EnvConfig)
	if cfgPath == "" {
		cfgPath = playtest.FileName
	}
	cfg, err := playtest.LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("load %s: %v", cfgPath, err)
	}
	return cfg.Bot
}

// TestPlaytest runs playtest.yaml's card-ai tournament across Suit Ladders' suit decks.
func TestPlaytest(t *testing.T) {
	rules, handCap, decks, err := loadCardPlaytestSetup()
	if err != nil {
		t.Fatal(err)
	}
	botCfg := loadCardPlaytestBot(t)
	makeSim := func(seed uint64, src replay.ActionSource, metrics *playtest.Collector) (playtest.Sim, error) {
		return playtest.NewCardTournamentSim(decks, newSuitLadderDuelMatch(rules, handCap), botCfg)(seed, src, metrics)
	}
	playtest.RunTest(t, playtest.FileName, makeSim)
}

// TestBotReportCanonicalAndDeterministic checks that playtest.yaml is canonical and repeated runs on the same seeds write byte-identical reports.
func TestBotReportCanonicalAndDeterministic(t *testing.T) {
	raw, err := os.ReadFile(playtest.FileName)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := playtest.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := playtest.EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, encoded) {
		t.Fatalf("playtest.yaml is not canonical;\n%s", encoded)
	}
	rules, handCap, decks, err := loadCardPlaytestSetup()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Runs.Count = 2
	cfg.Bot.Params["matchesPerPair"] = 6
	makeSim := func(seed uint64, src replay.ActionSource, metrics *playtest.Collector) (playtest.Sim, error) {
		return playtest.NewCardTournamentSim(decks, newSuitLadderDuelMatch(rules, handCap), cfg.Bot)(seed, src, metrics)
	}
	r1, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	y1, err := playtest.EncodeReport(r1.Report)
	if err != nil {
		t.Fatal(err)
	}
	y2, err := playtest.EncodeReport(r2.Report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("reports differ for identical seeds:\n--- 1 ---\n%s\n--- 2 ---\n%s", y1, y2)
	}
}

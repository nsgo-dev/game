package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/idle"
)

// testData is hearth.yaml as the game reads it.
var testData = mustParseData(hearthYAML)

// epoch is the wall-clock time the sim tests start at.
var epoch = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

// shopGreedily buys every upgrade it can afford, then the cheapest generator, until nothing more is affordable, and the goal first when it can. While the fire burns faster than it is fed it saves for the next fuel upgrade instead. It reports how many purchases it made.
func shopGreedily(h *hearth) int {
	n := 0
	for {
		if h.buyGoal() {
			n++
			continue
		}
		if j := nextFuelUpgrade(h); j >= 0 && h.netFuel() < 0 {
			if !h.buyUpgrade(j) {
				return n
			}
			n++
			continue
		}
		bought := false
		for j := range h.data.Upgrades {
			if h.buyUpgrade(j) {
				bought = true
			}
		}
		cheapest := -1
		for i := range h.data.Generators {
			if cheapest < 0 || h.generatorCost(i).Cmp(h.generatorCost(cheapest)) < 0 {
				cheapest = i
			}
		}
		if h.buyGenerator(cheapest) {
			bought = true
		}
		if !bought {
			return n
		}
		n++
	}
}

// nextFuelUpgrade returns the index of the first fuel upgrade not bought yet, or -1.
func nextFuelUpgrade(h *hearth) int {
	for j, u := range h.data.Upgrades {
		if u.Effect == effectFuel && !h.bought[j] {
			return j
		}
	}
	return -1
}

// playSession plays d online as an attentive player: it stokes once a second and shops greedily after every second.
func playSession(h *hearth, d time.Duration) {
	for range int(d / time.Second) {
		h.stoke()
		for range 60 {
			h.step(time.Second/60, 1)
		}
		shopGreedily(h)
	}
}

func TestNewHearthStartsLitWithAFullGauge(t *testing.T) {
	h := newHearth(testData)
	if h.cold || h.fuel != testData.Fuel.Capacity || !h.embers.IsZero() || !h.rate().IsZero() {
		t.Fatalf("new hearth: cold %v, fuel %v, embers %v, rate %v", h.cold, h.fuel, h.embers, h.rate())
	}
}

func TestStokeGathersAndFeedsTheFire(t *testing.T) {
	h := newHearth(testData)
	h.fuel = 50
	h.stoke()
	if h.fuel != 50+testData.Fuel.Stoke || h.embers.Cmp(testData.Fuel.Embers) != 0 {
		t.Fatalf("after a stoke: fuel %v, embers %v", h.fuel, h.embers)
	}
	h.fuel = testData.Fuel.Capacity - 1
	h.stoke()
	if h.fuel != testData.Fuel.Capacity {
		t.Fatalf("a stoke overfilled the gauge to %v", h.fuel)
	}
}

func TestUnfedFireGoesColdAndRekindles(t *testing.T) {
	h := newHearth(testData)
	h.ash = idle.New(3)
	steps := 0
	for !h.step(time.Second, 1) {
		steps++
		if steps > 1000 {
			t.Fatal("the fire never went out")
		}
	}
	if want := int(testData.Fuel.Capacity/testData.Fuel.Burn) - 1; steps != want {
		t.Fatalf("the fire lasted %d seconds, want %d", steps, want)
	}
	if h.stoke() || h.buyGenerator(0) || h.prestige() {
		t.Fatal("a cold hearth still stoked, bought or prestiged")
	}
	if !h.rekindle() || h.cold || h.fuel != testData.Fuel.Capacity || h.ash.Cmp(idle.New(3)) != 0 || h.runs != 1 {
		t.Fatalf("rekindle: cold %v, fuel %v, ash %v, runs %d", h.cold, h.fuel, h.ash, h.runs)
	}
}

func TestGeneratorCostsGrowGeometrically(t *testing.T) {
	h := newHearth(testData)
	g := testData.Generators[0]
	h.embers = idle.New(1e6)
	for n := range int64(20) {
		want := g.Cost.Mul(idle.New(g.Growth).Pow(n))
		if got := h.generatorCost(0); got.Cmp(want) != 0 {
			t.Fatalf("cost of spark %d = %v, want %v", n+1, got, want)
		}
		if !h.buyGenerator(0) {
			t.Fatalf("could not buy spark %d", n+1)
		}
	}
	if got, want := h.rate(), g.Rate.Scale(20); got.Cmp(want) != 0 {
		t.Fatalf("20 sparks make %v/s, want %v", got, want)
	}
}

func TestUpgradesMultiplyAndFeed(t *testing.T) {
	h := newHearth(testData)
	h.embers = idle.New(1e12)
	h.buyGenerator(0)
	base := h.rate()
	for j, u := range testData.Upgrades {
		if u.Effect == effectGenerator && u.Target == testData.Generators[0].ID {
			if !h.buyUpgrade(j) || h.buyUpgrade(j) {
				t.Fatalf("upgrade %s: bought %v, bought twice", u.ID, h.bought[j])
			}
			base = base.Scale(u.Value)
		}
		if u.Effect == effectFuel {
			h.buyUpgrade(j)
		}
	}
	if got := h.rate(); got.Cmp(base) != 0 {
		t.Fatalf("rate after spark upgrades %v, want %v", got, base)
	}
	if h.netFuel() <= 0 {
		t.Fatalf("every fuel upgrade together still burns %v/s faster than it feeds", -h.netFuel())
	}
}

func TestPrestigePaysTheSquareRootAndKeepsAsh(t *testing.T) {
	h := newHearth(testData)
	if h.prestige() {
		t.Fatal("prestiged with nothing earned")
	}
	h.earn(testData.Prestige.Threshold.Scale(17))
	if got := h.ashGain(); got.Cmp(idle.New(4)) != 0 {
		t.Fatalf("ash for 17 thresholds = %v, want 4", got)
	}
	h.buyGenerator(0)
	if !h.prestige() || h.ash.Cmp(idle.New(4)) != 0 || !h.embers.IsZero() || h.owned[0] != 0 || h.runs != 1 {
		t.Fatalf("after prestige: ash %v, embers %v, sparks %d, runs %d", h.ash, h.embers, h.owned[0], h.runs)
	}
	h.buyGenerator(0)
	h.embers = idle.New(1e9)
	h.buyGenerator(0)
	if got, want := h.rate(), testData.Generators[0].Rate.Scale(1+4*testData.Prestige.Bonus); got.Cmp(want) != 0 {
		t.Fatalf("one spark with 4 ash makes %v/s, want %v", got, want)
	}
}

func TestAshGainBeyondFloat64(t *testing.T) {
	h := newHearth(testData)
	h.earned = testData.Prestige.Threshold.Mul(idle.NewExp(4, 400))
	if got, want := h.ashGain(), idle.NewExp(2, 200); got.Cmp(want) != 0 {
		t.Fatalf("ash for 4e400 thresholds = %v, want %v", got, want)
	}
	h.earned = testData.Prestige.Threshold.Mul(idle.NewExp(9, 401))
	if got, want := h.ashGain(), idle.NewExp(3, 200).Scale(3.1622776601683795); got.Cmp(want) != 0 {
		t.Fatalf("ash for 9e401 thresholds = %v, want %v", got, want)
	}
}

func TestCreditAwayReplaysAtOfflineEfficiency(t *testing.T) {
	h := newHearth(testData)
	h.embers = idle.New(1e6)
	for j, u := range testData.Upgrades {
		if u.Effect == effectFuel {
			h.buyUpgrade(j)
		}
	}
	h.buyGenerator(1)
	h.embers = idle.Number{}
	rate := h.rate()

	r := h.creditAway(epoch, epoch.Add(2*time.Hour))
	if r.WentCold || r.Away.Credited != 2*time.Hour {
		t.Fatalf("2h away: %+v", r)
	}
	want := rate.Scale(2 * 3600 * testData.Offline.Efficiency)
	if diff := r.Earned.Add(want.Neg()).Abs(); diff.Cmp(want.Scale(1e-9)) > 0 {
		t.Fatalf("2h away earned %v, want %v", r.Earned, want)
	}
	if h.embers.Cmp(r.Earned) != 0 {
		t.Fatalf("embers %v after earning %v from nothing", h.embers, r.Earned)
	}

	// The cap limits a long absence; a short one and a clock set back credit nothing.
	if r := h.creditAway(epoch, epoch.Add(30*24*time.Hour)); !r.Away.Capped || r.Away.Credited != time.Duration(testData.Offline.CapMinutes)*time.Minute {
		t.Fatalf("a month away: %+v", r.Away)
	}
	if r := h.creditAway(epoch, epoch.Add(10*time.Second)); !r.Earned.IsZero() {
		t.Fatalf("10s away earned %v", r.Earned)
	}
	if r := h.creditAway(epoch, epoch.Add(-time.Hour)); !r.Away.Rewound || !r.Earned.IsZero() {
		t.Fatalf("a clock set back: %+v", r)
	}
	if r := h.creditAway(time.Time{}, epoch); !r.Earned.IsZero() {
		t.Fatalf("a new game earned %v", r.Earned)
	}
}

func TestUnfedFireGoesColdWhileAway(t *testing.T) {
	h := newHearth(testData)
	h.embers = idle.New(10)
	h.buyGenerator(0)
	r := h.creditAway(epoch, epoch.Add(time.Hour))
	if !r.WentCold || !h.cold || r.Earned.IsZero() {
		t.Fatalf("an hour away with an unfed fire: %+v, cold %v", r, h.cold)
	}
}

// TestBalanceReachesTheGoal plays the game as an attentive player who plays ten minutes, leaves for the offline cap and scatters the ashes whenever that would at least double them, and checks the goal comes within the sessions the design aims at. Change hearth.yaml's numbers and this test says what the change did to the pace.
func TestBalanceReachesTheGoal(t *testing.T) {
	h := newHearth(testData)
	now := epoch
	for session := 1; session <= 40; session++ {
		playSession(h, 10*time.Minute)
		now = now.Add(10 * time.Minute)
		if h.cold {
			t.Fatalf("session %d: an attentive player let the fire go out", session)
		}
		if h.won {
			t.Logf("won in session %d after %v of play and %d prestiges", session, h.played, h.runs)
			if session < 4 || session > 20 {
				t.Fatalf("the goal came in session %d; the design aims at 4 to 20", session)
			}
			return
		}
		if gain := h.ashGain(); gain.Cmp(h.ash.Max(idle.New(1))) >= 0 {
			h.prestige()
			continue
		}
		next := now.Add(time.Duration(testData.Offline.CapMinutes) * time.Minute)
		if r := h.creditAway(now, next); r.WentCold {
			t.Fatalf("session %d: the fire went out while away", session)
		}
		now = next
	}
	t.Fatalf("no win in 40 sessions: embers %v, ash %v, rate %v/s", h.embers, h.ash, h.rate())
}

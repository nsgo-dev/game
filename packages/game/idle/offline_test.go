package idle_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/idle"
)

var lastSeen = time.Date(2026, time.March, 14, 21, 30, 0, 0, time.UTC)

func TestOfflineEarnMatchesRateAndCap(t *testing.T) {
	rate := idle.New(2.5) // per second
	cases := []struct {
		name     string
		o        idle.Offline
		away     time.Duration
		earned   idle.Number
		credited time.Duration
		capped   bool
	}{
		{"uncapped", idle.Offline{}, 3 * time.Hour, idle.New(27000), 3 * time.Hour, false},
		{"under the cap", idle.Offline{Cap: 8 * time.Hour}, 3 * time.Hour, idle.New(27000), 3 * time.Hour, false},
		{"exactly the cap", idle.Offline{Cap: 3 * time.Hour}, 3 * time.Hour, idle.New(27000), 3 * time.Hour, false},
		{"over the cap", idle.Offline{Cap: 2 * time.Hour}, 30 * 24 * time.Hour, idle.New(18000), 2 * time.Hour, true},
		{"half efficiency", idle.Offline{Cap: 8 * time.Hour, Efficiency: 0.5}, 3 * time.Hour, idle.New(13500), 3 * time.Hour, false},
		{"full efficiency", idle.Offline{Efficiency: 1}, time.Minute, idle.New(150), time.Minute, false},
		{"below the minimum", idle.Offline{Min: time.Minute}, 30 * time.Second, idle.Number{}, 0, false},
		{"at the minimum", idle.Offline{Min: time.Minute}, time.Minute, idle.New(150), time.Minute, false},
		{"fractional seconds", idle.Offline{}, 1500 * time.Millisecond, idle.New(3.75), 1500 * time.Millisecond, false},
	}
	for _, c := range cases {
		earned, a := c.o.Earn(rate, lastSeen, lastSeen.Add(c.away))
		if earned != c.earned {
			t.Errorf("%s: earned %v, want %v", c.name, earned, c.earned)
		}
		if a.Elapsed != c.away || a.Credited != c.credited || a.Capped != c.capped || a.Rewound {
			t.Errorf("%s: away = %+v, want elapsed %v credited %v capped %v", c.name, a, c.away, c.credited, c.capped)
		}
		if !a.Seen.Equal(lastSeen.Add(c.away)) {
			t.Errorf("%s: seen %v", c.name, a.Seen)
		}
	}
}

func TestOfflineEarnLargeRates(t *testing.T) {
	o := idle.Offline{Cap: 8 * time.Hour}
	rate := idle.NewExp(1.5, 300)
	earned, a := o.Earn(rate, lastSeen, lastSeen.Add(72*time.Hour))
	// 1.5e300 per second for the 28800 capped seconds.
	if want := idle.NewExp(4.32, 304); !near(earned, want, 1e-15) || !a.Capped {
		t.Errorf("earned %v (%+v), want %v", earned, a, want)
	}
	if got := (idle.Format{}).Format(earned); got != "4.32e304" {
		t.Errorf("formatted %q", got)
	}
	huge := idle.NewExp(7, 5000)
	if earned, _ := o.Earn(huge, lastSeen, lastSeen.Add(time.Second)); earned != huge {
		t.Errorf("one second at %v earned %v", huge, earned)
	}
}

func TestOfflineClock(t *testing.T) {
	var o idle.Offline

	first := o.Absence(time.Time{}, lastSeen)
	if first.Credited != 0 || first.Elapsed != 0 || !first.Seen.Equal(lastSeen) {
		t.Errorf("first launch = %+v", first)
	}

	back := lastSeen.Add(-10 * time.Hour)
	earned, rewound := o.Earn(idle.New(1), lastSeen, back)
	if !earned.IsZero() || !rewound.Rewound || rewound.Credited != 0 || !rewound.Seen.Equal(lastSeen) {
		t.Errorf("rewound clock = %v, %+v", earned, rewound)
	}
	// Setting the clock back and forward again earns only the real time away: Seen kept the later timestamp.
	forward := back.Add(10*time.Hour + time.Minute)
	if again := o.Absence(rewound.Seen, forward); again.Credited != time.Minute {
		t.Errorf("rewind then forward credited %v, want 1m", again.Credited)
	}

	// The same instant in another zone is no absence, and a monotonic reading is ignored.
	tokyo := lastSeen.In(time.FixedZone("JST", 9*3600)).Add(time.Hour)
	if a := o.Absence(lastSeen, tokyo); a.Elapsed != time.Hour {
		t.Errorf("zone change: %+v", a)
	}
	start := time.Now()
	if a := o.Absence(start, start.Add(2*time.Hour)); a.Elapsed != 2*time.Hour || a.Seen != a.Seen.Round(0) {
		t.Errorf("monotonic: %+v", a)
	}
}

func TestAwaySteps(t *testing.T) {
	a := idle.Offline{Cap: 2 * time.Hour}.Absence(lastSeen, lastSeen.Add(5*time.Hour))
	if n, rest := a.Steps(time.Minute); n != 120 || rest != 0 {
		t.Errorf("Steps(1m) = %d, %v", n, rest)
	}
	if n, rest := a.Steps(7 * time.Minute); n != 17 || rest != time.Minute {
		t.Errorf("Steps(7m) = %d, %v", n, rest)
	}
	if n, rest := (idle.Away{}).Steps(time.Second); n != 0 || rest != 0 {
		t.Errorf("no absence: %d, %v", n, rest)
	}
}

func TestOfflinePanicsOnBadConfig(t *testing.T) {
	bad := []idle.Offline{
		{Cap: -1},
		{Min: -time.Second},
		{Efficiency: -0.5},
		{Efficiency: math.NaN()},
		{Efficiency: math.Inf(1)},
		{Cap: time.Hour, Min: 2 * time.Hour},
	}
	for _, o := range bad {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%+v did not panic", o)
				}
			}()
			o.Absence(lastSeen, lastSeen.Add(time.Hour))
		}()
	}
	defer func() {
		if recover() == nil {
			t.Error("Steps(0) did not panic")
		}
	}()
	idle.Away{}.Steps(0)
}

func TestSeconds(t *testing.T) {
	if got := idle.Seconds(90 * time.Minute); got != idle.New(5400) {
		t.Errorf("Seconds(90m) = %v", got)
	}
	if got := idle.Seconds(250 * time.Millisecond); got != idle.New(0.25) {
		t.Errorf("Seconds(250ms) = %v", got)
	}
}

func BenchmarkEarn(b *testing.B) {
	o := idle.Offline{Cap: 8 * time.Hour, Efficiency: 0.5}
	rate := idle.NewExp(3.7, 120)
	now := lastSeen.Add(5 * time.Hour)
	for b.Loop() {
		_, _ = o.Earn(rate, lastSeen, now)
	}
}

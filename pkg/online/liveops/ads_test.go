package liveops

import (
	"errors"
	"testing"
	"time"
)

// fakeAdHost answers each Show synchronously unless hold is set, in which case it keeps done for the test to call.
type fakeAdHost struct {
	calls   int
	kinds   []AdKind
	end     bool
	err     error
	twice   bool
	hold    bool
	pending func(bool, error)
}

func (f *fakeAdHost) Show(kind AdKind, _ string, done func(bool, error)) error {
	f.calls++
	f.kinds = append(f.kinds, kind)
	if f.hold {
		f.pending = done
		return nil
	}
	done(f.end, f.err)
	if f.twice {
		done(f.end, f.err)
	}
	return nil
}

func TestRewardedOnlyCompletesOnEndedAndOnce(t *testing.T) {
	f := &fakeAdHost{end: false, twice: true}
	var got []AdResult
	a := &Ads{Host: f}
	if err := a.ShowRewarded("coin", func(r AdResult) { got = append(got, r) }); err != nil {
		t.Fatal(err)
	}
	f.end = true
	if err := a.ShowRewarded("coin", func(r AdResult) { got = append(got, r) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != AdSkipped || got[1] != AdRewarded {
		t.Fatalf("results = %v, want [skipped rewarded]", got)
	}
	if f.kinds[0] != Rewarded {
		t.Fatalf("kind = %q", f.kinds[0])
	}
}

func TestRewardedNoFillAndFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want AdResult
	}{
		{err: ErrAdNoFill, want: AdNoFill},
		{err: errors.New("host broke"), want: AdFailed},
	} {
		f := &fakeAdHost{end: true, err: tc.err}
		var got AdResult
		a := &Ads{Host: f}
		_ = a.ShowRewarded("coin", func(r AdResult) { got = r })
		if got != tc.want {
			t.Fatalf("err %v: result = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestAdsPausesResetsInputAndRefusesOverlap(t *testing.T) {
	f := &fakeAdHost{hold: true}
	var pauses []bool
	resets := 0
	a := &Ads{Host: f, Pause: func(p bool) { pauses = append(pauses, p) }, ResetInput: func() { resets++ }}
	var got AdResult
	if err := a.ShowRewarded("coin", func(r AdResult) { got = r }); err != nil {
		t.Fatal(err)
	}
	if len(pauses) != 1 || !pauses[0] || resets != 1 {
		t.Fatalf("while showing: pauses = %v, resets = %d", pauses, resets)
	}
	var second AdResult
	if err := a.ShowInterstitial("i", func(r AdResult) { second = r }); !errors.Is(err, ErrAdBusy) || second != AdFailed {
		t.Fatalf("overlapping show: err = %v, result = %q", err, second)
	}
	if f.calls != 1 || len(pauses) != 1 {
		t.Fatalf("overlapping show reached the host or touched pause: calls = %d, pauses = %v", f.calls, pauses)
	}
	f.pending(true, nil)
	if got != AdRewarded || len(pauses) != 2 || pauses[1] {
		t.Fatalf("after close: result = %q, pauses = %v", got, pauses)
	}
	f.hold = false
	if err := a.ShowInterstitial("i", func(r AdResult) { second = r }); err != nil || second != AdShown {
		t.Fatalf("show after close: err = %v, result = %q", err, second)
	}
}

func TestInterstitialCapsAndInterval(t *testing.T) {
	now := time.Now()
	f := &fakeAdHost{}
	a := &Ads{Host: f, InterstitialCap: 2, InterstitialInterval: time.Minute, Now: func() time.Time { return now }}
	var got []AdResult
	record := func(r AdResult) { got = append(got, r) }
	_ = a.ShowInterstitial("i", record)
	_ = a.ShowInterstitial("i", record)
	now = now.Add(time.Minute)
	_ = a.ShowInterstitial("i", record)
	now = now.Add(time.Hour)
	_ = a.ShowInterstitial("i", record)
	want := []AdResult{AdShown, AdCapped, AdShown, AdCapped}
	if f.calls != 2 || len(got) != len(want) {
		t.Fatalf("calls = %d, results = %v, want %v", f.calls, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("results = %v, want %v", got, want)
		}
	}
}

func TestDisabledStopsInterstitialsButNotRewarded(t *testing.T) {
	f := &fakeAdHost{end: true}
	a := &Ads{Host: f, Disabled: func() bool { return true }}
	var inter, reward AdResult
	_ = a.ShowInterstitial("i", func(r AdResult) { inter = r })
	_ = a.ShowRewarded("coin", func(r AdResult) { reward = r })
	if inter != AdCapped || reward != AdRewarded || f.calls != 1 {
		t.Fatalf("interstitial = %q, rewarded = %q, calls = %d", inter, reward, f.calls)
	}
}

package liveops

import (
	"testing"
	"time"
)

type fakeAdHost struct {
	calls int
	end   bool
	err   error
}

func (f *fakeAdHost) Show(_ AdKind, _ string, done func(bool, error)) error {
	f.calls++
	done(f.end, f.err)
	return f.err
}

func TestRewardedOnlyCompletesOnEndedAndOnce(t *testing.T) {
	f := &fakeAdHost{end: false}
	var got AdResult
	a := &Ads{Host: f}
	if err := a.ShowRewarded("coin", func(r AdResult) { got = r }); err != nil {
		t.Fatal(err)
	}
	if got != AdSkipped {
		t.Fatalf("result=%q", got)
	}
	f.end = true
	got = ""
	_ = a.ShowRewarded("coin", func(r AdResult) { got = r })
	if got != AdRewarded {
		t.Fatalf("result=%q", got)
	}
}

func TestInterstitialCapsAndInterval(t *testing.T) {
	now := time.Now()
	f := &fakeAdHost{end: true}
	a := &Ads{Host: f, InterstitialCap: 2, InterstitialInterval: time.Minute, Now: func() time.Time { return now }}
	_ = a.ShowInterstitial("i", nil)
	_ = a.ShowInterstitial("i", nil)
	if f.calls != 1 {
		t.Fatalf("calls=%d", f.calls)
	}
	now = now.Add(time.Minute)
	_ = a.ShowInterstitial("i", nil)
	if f.calls != 2 {
		t.Fatalf("calls=%d", f.calls)
	}
}

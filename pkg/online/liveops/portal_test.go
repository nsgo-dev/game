package liveops

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakePortal struct {
	events []string
	reward bool
	err    error
}

func (f *fakePortal) LoadingFinished(context.Context) error {
	f.events = append(f.events, "loading")
	return nil
}
func (f *fakePortal) GameplayStart(context.Context) error {
	f.events = append(f.events, "start")
	return nil
}
func (f *fakePortal) GameplayStop(context.Context) error {
	f.events = append(f.events, "stop")
	return nil
}
func (f *fakePortal) CommercialBreak(context.Context) error {
	f.events = append(f.events, "break")
	return f.err
}
func (f *fakePortal) RewardedBreak(context.Context) (bool, error) {
	f.events = append(f.events, "rewarded")
	return f.reward, f.err
}
func TestPortalBreakOrderRewardAndNoop(t *testing.T) {
	f := &fakePortal{reward: true}
	var events []string
	p := Portal{Provider: PortalPoki, SDK: f, Hooks: PortalHooks{Pause: func() { events = append(events, "pause") }, Mute: func() { events = append(events, "mute") }, ResetInput: func() { events = append(events, "reset") }, Unmute: func() { events = append(events, "unmute") }, Resume: func() { events = append(events, "resume") }}}
	if err := p.Break(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"pause", "mute", "reset", "unmute", "resume"}) || !reflect.DeepEqual(f.events, []string{"break"}) {
		t.Fatalf("events=%v sdk=%v", events, f.events)
	}
	granted, err := p.Rewarded(context.Background())
	if err != nil || !granted {
		t.Fatalf("reward=%v err=%v", granted, err)
	}
	f.reward = false
	granted, _ = p.Rewarded(context.Background())
	if granted {
		t.Fatal("reward granted on false result")
	}
	var no Portal
	if err := no.Break(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestPortalTimeoutRestoresAndDoesNotGrant(t *testing.T) {
	f := &fakePortal{}
	f.err = context.DeadlineExceeded
	restored := 0
	p := Portal{Provider: PortalCrazyGames, SDK: f, Timeout: time.Nanosecond, Hooks: PortalHooks{Unmute: func() { restored++ }, Resume: func() { restored++ }}}
	if _, err := p.Rewarded(context.Background()); err == nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timeout not reported")
	}
	if restored != 2 {
		t.Fatalf("restored=%d", restored)
	}
}

package playtest

import "testing"

func TestBaselinesRoundTripAndDurationValidation(t *testing.T) {
	cfg, err := ParseConfig([]byte("version: 1\nscope: {scene: level}\nruns: {count: 2, maxDuration: 10s}\nbot: {profile: runner}\nbaselines:\n  idle: {outcome: lose, within: 8s}\n  reference: {winRate: 0.8}\n  degraded: {reactionScale: 2, dropRate: 0.25, maxWinRate: 0.4, margin: 0.2}\n"))
	if err != nil || cfg.Baselines == nil || cfg.Baselines.Idle.Within.String() != "8s" {
		t.Fatalf("config=%+v err=%v", cfg, err)
	}
	encoded, err := EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseConfig(encoded); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseConfig([]byte("version: 1\nscope: {scene: level}\nruns: {count: 2, maxDuration: 10s}\nbot: {profile: runner}\nbaselines: {idle: {outcome: lose, within: 11s}}\n")); err == nil {
		t.Fatal("idle.within longer than maxDuration accepted")
	}
}

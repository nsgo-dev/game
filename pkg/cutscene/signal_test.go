package cutscene

import (
	"testing"
	"time"
)

func TestSignalTrack(t *testing.T) {
	n := 0
	p, err := NewTimelinePlayer(Cutscene{Version: 2, Kind: KindTimeline, Length: Duration(time.Second), Tracks: []Track{{Kind: TrackSignal, Clips: []Clip{{Start: Duration(500 * time.Millisecond), Duration: Duration(time.Millisecond), Signal: "open"}}}}}, TimelineTarget{OnSignal: func(string, map[string]any) { n++ }})
	if err != nil {
		t.Fatal(err)
	}
	p.Seek(500 * time.Millisecond)
	p.Play()
	p.Update(time.Second)
	if n != 1 {
		t.Fatalf("signals=%d", n)
	}
}

package netplay

import (
	"testing"
	"time"
)

func TestAckTrackerTracksGaps(t *testing.T) {
	var a AckTracker
	a.Receive(0)
	a.Receive(2)
	h := a.Header()
	if h.Next != 1 || h.Mask != 1 {
		t.Fatalf("header after gap = %+v", h)
	}
	a.Receive(1)
	if h = a.Header(); h.Next != 3 || h.Mask != 0 {
		t.Fatalf("header after fill = %+v", h)
	}
	b := make([]byte, AckHeaderSize)
	if !h.Encode(b) {
		t.Fatal("encode failed")
	}
	if got, ok := DecodeAckHeader(b); !ok || got != h {
		t.Fatalf("decode = %+v, %v", got, ok)
	}
}

func TestPacerCoalescesAndAimd(t *testing.T) {
	p := NewPacer(CongestionConfig{RateMin: 100, RateMax: 1000, InitialRate: 500, MTU: 10})
	if err := p.Enqueue(Outbound{Payload: []byte("abc"), Reliable: true}); err != nil {
		t.Fatal(err)
	}
	if err := p.Enqueue(Outbound{Payload: []byte("de"), Reliable: true}); err != nil {
		t.Fatal(err)
	}
	got := p.Poll(time.Unix(0, 0))
	if len(got) != 1 || string(got[0]) != "abcde" {
		t.Fatalf("poll = %q", got)
	}
	before := p.Rate()
	p.Acked(1000)
	if p.Rate() <= before {
		t.Fatal("ack did not increase rate")
	}
	p.Lost()
	if p.Rate() >= before+125 {
		t.Fatal("loss did not reduce rate")
	}
	if p.QueueBytes() != 0 || p.DrainTime() != 0 {
		t.Fatal("queue remained")
	}
}

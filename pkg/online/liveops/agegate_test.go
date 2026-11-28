package liveops

import (
	"testing"
	"time"
)

type ageMem struct{ v map[string]string }

func (m *ageMem) Get(k string) (string, bool) { v, ok := m.v[k]; return v, ok }
func (m *ageMem) Set(k, v string) error       { m.v[k] = v; return nil }
func TestAgeGatePersistsAndFailsClosed(t *testing.T) {
	m := &ageMem{v: map[string]string{}}
	g := NewAgeGate(m, "")
	if !g.ChildMode() {
		t.Fatal("unanswered gate must be child mode")
	}
	if err := g.SetBand(Adult); err != nil {
		t.Fatal(err)
	}
	g2 := NewAgeGate(m, "")
	if g2.ChildMode() {
		t.Fatal("adult band lost")
	}
	m.v["age_gate"] = "garbage"
	if !NewAgeGate(m, "").ChildMode() {
		t.Fatal("corrupt record must fail closed")
	}
}
func TestAgeBands(t *testing.T) {
	for _, b := range []AgeBand{Under13, Age13To15, Age16To17, Adult} {
		if !b.Valid() {
			t.Fatal(b)
		}
	}
	if (AgeRecord{Band: Adult, AskedAt: time.Now()}).Band.Child() {
		t.Fatal("adult child")
	}
}

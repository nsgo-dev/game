package liveops

import (
	"encoding/json"
	"errors"
	"time"
)

type AgeBand string

const (
	Under13   AgeBand = "under13"
	Age13To15 AgeBand = "13to15"
	Age16To17 AgeBand = "16to17"
	Adult     AgeBand = "adult"
)

func (b AgeBand) Valid() bool {
	switch b {
	case Under13, Age13To15, Age16To17, Adult:
		return true
	}
	return false
}
func (b AgeBand) Child() bool { return b != Adult }

// AgeRecord is the only data AgeGate persists. It contains no date of birth.
type AgeRecord struct {
	Band    AgeBand   `json:"band"`
	AskedAt time.Time `json:"asked_at"`
}
type AgeStorage interface {
	Get(string) (string, bool)
	Set(string, string) error
}
type AgeGate struct {
	storage AgeStorage
	key     string
	now     func() time.Time
	record  AgeRecord
	loaded  bool
}

func NewAgeGate(storage AgeStorage, key string) *AgeGate {
	if key == "" {
		key = "age_gate"
	}
	g := &AgeGate{storage: storage, key: key, now: time.Now}
	g.load()
	return g
}
func (g *AgeGate) load() {
	if g.storage == nil {
		return
	}
	raw, ok := g.storage.Get(g.key)
	if !ok {
		return
	}
	var r AgeRecord
	if json.Unmarshal([]byte(raw), &r) == nil && r.Band.Valid() && !r.AskedAt.IsZero() {
		g.record = r
		g.loaded = true
	}
}
func (g *AgeGate) Band() (AgeBand, bool) {
	if !g.loaded {
		return "", false
	}
	return g.record.Band, true
}
func (g *AgeGate) SetBand(b AgeBand) error {
	if !b.Valid() {
		return errors.New("liveops: invalid age band")
	}
	r := AgeRecord{Band: b, AskedAt: g.now().UTC()}
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	if g.storage != nil {
		if e = g.storage.Set(g.key, string(raw)); e != nil {
			return e
		}
	}
	g.record = r
	g.loaded = true
	return nil
}
func (g *AgeGate) ChildMode() bool { b, ok := g.Band(); return !ok || b.Child() }

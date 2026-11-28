package liveops

import (
	"encoding/json"
	"errors"
	"time"
)

type Purpose string

const (
	AnalyticsPurpose       Purpose = "analytics"
	CrashPurpose           Purpose = "crash"
	PersonalizationPurpose Purpose = "personalization"
	MarketingPurpose       Purpose = "marketing"
)

type Regime int

const (
	OptIn Regime = iota
	OptOut
)

type ConsentChoice int

const (
	UnknownChoice ConsentChoice = iota
	GrantedChoice
	DeniedChoice
)

type ConsentRecord struct {
	Choice ConsentChoice `json:"choice"`
	At     time.Time     `json:"at"`
	Policy string        `json:"policy"`
	Source string        `json:"source"`
}
type ConsentStorage interface {
	Get(string) (string, bool)
	Set(string, string) error
}
type Consent struct {
	store   ConsentStorage
	key     string
	regime  Regime
	policy  string
	records map[Purpose]ConsentRecord
}

func NewConsent(store ConsentStorage, key, policy string, regime Regime) *Consent {
	if key == "" {
		key = "liveops.consent"
	}
	c := &Consent{store: store, key: key, policy: policy, regime: regime, records: map[Purpose]ConsentRecord{}}
	c.load()
	return c
}
func (c *Consent) load() {
	if c.store == nil {
		return
	}
	raw, ok := c.store.Get(c.key)
	if !ok {
		return
	}
	var v struct {
		Version int                       `json:"version"`
		Records map[Purpose]ConsentRecord `json:"records"`
	}
	if json.Unmarshal([]byte(raw), &v) != nil || v.Version != 1 {
		return
	}
	for p, r := range v.Records {
		if validPurpose(p) && validChoice(r.Choice) {
			c.records[p] = r
		}
	}
}
func validPurpose(p Purpose) bool {
	switch p {
	case AnalyticsPurpose, CrashPurpose, PersonalizationPurpose, MarketingPurpose:
		return true
	}
	return false
}
func validChoice(v ConsentChoice) bool { return v >= UnknownChoice && v <= DeniedChoice }
func (c *Consent) Allowed(p Purpose) bool {
	r, ok := c.records[p]
	if !ok || r.Policy != c.policy || r.Choice == UnknownChoice {
		return c.regime == OptOut
	}
	return r.Choice == GrantedChoice
}
func (c *Consent) RequiresPrompt(p Purpose) bool {
	r, ok := c.records[p]
	return !ok || r.Policy != c.policy || r.Choice == UnknownChoice
}
func (c *Consent) Set(p Purpose, v ConsentChoice, source string) error {
	if !validPurpose(p) || !validChoice(v) || source == "" {
		return errors.New("liveops: invalid consent record")
	}
	c.records[p] = ConsentRecord{Choice: v, At: time.Now().UTC(), Policy: c.policy, Source: source}
	return c.save()
}
func (c *Consent) save() error {
	if c.store == nil {
		return nil
	}
	raw, e := json.Marshal(struct {
		Version int                       `json:"version"`
		Records map[Purpose]ConsentRecord `json:"records"`
	}{1, c.records})
	if e != nil {
		return e
	}
	return c.store.Set(c.key, string(raw))
}

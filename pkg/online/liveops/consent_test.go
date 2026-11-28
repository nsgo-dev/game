package liveops

import "testing"

type consentMem struct{ v map[string]string }

func (m *consentMem) Get(k string) (string, bool) { v, ok := m.v[k]; return v, ok }
func (m *consentMem) Set(k, v string) error       { m.v[k] = v; return nil }
func TestConsentPurposePolicyAndOptOut(t *testing.T) {
	m := &consentMem{v: map[string]string{}}
	c := NewConsent(m, "", "v1", OptIn)
	if c.Allowed(AnalyticsPurpose) || !c.RequiresPrompt(AnalyticsPurpose) {
		t.Fatal("opt in default")
	}
	if err := c.Set(AnalyticsPurpose, GrantedChoice, "prompt"); err != nil || !c.Allowed(AnalyticsPurpose) {
		t.Fatal("grant")
	}
	if NewConsent(m, "", "v2", OptIn).Allowed(AnalyticsPurpose) {
		t.Fatal("policy bump should prompt")
	}
	o := NewConsent(nil, "", "v1", OptOut)
	if !o.Allowed(MarketingPurpose) {
		t.Fatal("opt out default")
	}
}

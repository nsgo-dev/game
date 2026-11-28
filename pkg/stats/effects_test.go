package stats

import "testing"

func TestGameplayEffectApply(t *testing.T) {
	s, _ := NewSheet(Def{Name: "hp", Base: 10})
	g, err := s.ApplyEffect(Effect{Name: "burn", Kind: "timed", Steps: 2, Modifiers: []Modifier{{Stat: "hp", Op: OpAdd, Value: -3}}}, "burn")
	if err != nil || g.Handle == 0 || s.Value("hp") != 7 {
		t.Fatalf("g=%+v err=%v hp=%v", g, err, s.Value("hp"))
	}
}

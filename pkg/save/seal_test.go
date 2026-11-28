package save

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func sealOpts(bind string) Option {
	return WithSeal(SealOptions{Key: func() ([]byte, error) { return []byte("test secret"), nil }, Bind: bind})
}
func TestSealDetectsEditedDataAndBind(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir, sealOpts("acct-a"))
	if err := s.Save("slot", map[string]int{"coins": 1}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "slot.json")
	raw, _ := os.ReadFile(p)
	var env map[string]any
	_ = json.Unmarshal(raw, &env)
	env["data"] = map[string]int{"coins": 999}
	raw, _ = json.Marshal(env)
	_ = os.WriteFile(p, raw, 0600)
	var out map[string]int
	if err := s.Load("slot", &out); !errors.Is(err, ErrTampered) {
		t.Fatalf("edited err=%v", err)
	}
}
func TestSealRoundTripAndBind(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir, sealOpts("acct-a"))
	if err := s.Save("slot", map[string]int{"coins": 2}); err != nil {
		t.Fatal(err)
	}
	var out map[string]int
	if err := s.Load("slot", &out); err != nil || out["coins"] != 2 {
		t.Fatalf("load=%v %#v", err, out)
	}
	wrong := NewFileStore(dir, sealOpts("acct-b"))
	if err := wrong.Load("slot", &out); !errors.Is(err, ErrTampered) {
		t.Fatalf("bind err=%v", err)
	}
}

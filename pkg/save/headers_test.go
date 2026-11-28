package save_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/save"
)

type slotHeader struct {
	Chapter string `json:"chapter"`
}

func TestHeadersListWithoutDecodingData(t *testing.T) {
	dir := t.TempDir()
	s := save.NewFileStore(dir)
	if err := s.SaveWithHeader("slot1", slotHeader{Chapter: "one"}, playerState{Name: "A", Level: 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("plain", playerState{Name: "B"}); err != nil {
		t.Fatal(err)
	}
	// A data value no Go type here decodes proves Headers never unmarshals the data.
	if err := os.WriteFile(filepath.Join(dir, "odd.json"), []byte(`{"version":1,"header":{"chapter":"two"},"data":{"level":"not a number"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := s.Headers()
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 2 || string(h["slot1"]) != `{"chapter":"one"}` || string(h["odd"]) != `{"chapter":"two"}` {
		t.Fatalf("Headers = %s", h)
	}
	if _, ok := h["plain"]; ok {
		t.Fatal("a slot saved without a header was listed")
	}
	var got playerState
	if err := s.Load("slot1", &got); err != nil || got.Name != "A" || got.Level != 3 {
		t.Fatalf("Load = %+v, %v", got, err)
	}
}

func TestSaveWithHeaderKeepsPreviousSaveAsBackup(t *testing.T) {
	dir := t.TempDir()
	s := save.NewFileStore(dir)
	if err := s.SaveWithHeader("slot1", slotHeader{Chapter: "one"}, playerState{Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveWithHeader("slot1", slotHeader{Chapter: "two"}, playerState{Name: "B"}); err != nil {
		t.Fatal(err)
	}
	// A torn primary falls back to the backup for both the header and the data, so they still describe the same save.
	if err := os.WriteFile(filepath.Join(dir, "slot1.json"), []byte(`{"version":1,"hea`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := s.Headers()
	if err != nil {
		t.Fatal(err)
	}
	if string(h["slot1"]) != `{"chapter":"one"}` {
		t.Fatalf("header after torn primary = %s", h["slot1"])
	}
	var got playerState
	if err := s.Load("slot1", &got); err != nil || got.Name != "A" {
		t.Fatalf("Load after torn primary = %+v, %v", got, err)
	}
}

func TestSaveWithHeaderRefusesNonObjectHeader(t *testing.T) {
	s := save.NewFileStore(t.TempDir())
	for _, header := range []any{nil, "chapter one", 3, []string{"a"}} {
		if err := s.SaveWithHeader("slot1", header, playerState{Name: "A"}); err == nil {
			t.Fatalf("SaveWithHeader(%v) = nil, want an error", header)
		}
	}
	if s.Exists("slot1") {
		t.Fatal("a refused header still wrote the slot")
	}
}

func TestEnvelopeRefusesBadHeader(t *testing.T) {
	for name, payload := range map[string]string{
		"not an object": `{"version":1,"header":"x","data":{"name":"A"}}`,
		"null":          `{"version":1,"header":null,"data":{"name":"A"}}`,
		"duplicate":     `{"version":1,"header":{},"header":{},"data":{"name":"A"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "slot1.json"), []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			s := save.NewFileStore(dir)
			var got playerState
			if err := s.Load("slot1", &got); !errors.Is(err, save.ErrCorrupt) {
				t.Fatalf("Load err = %v, want ErrCorrupt", err)
			}
			h, err := s.Headers()
			if err != nil || len(h) != 0 {
				t.Fatalf("Headers = %s, %v; want none", h, err)
			}
		})
	}
}

func TestEnvelopeVersionReadsHeaderEnvelope(t *testing.T) {
	v, ok := save.EnvelopeVersion(json.RawMessage(`{"version":4,"header":{"chapter":"one"},"data":{}}`))
	if !ok || v != 4 {
		t.Fatalf("EnvelopeVersion = %d, %v", v, ok)
	}
}

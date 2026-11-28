package save_test

import (
	"github.com/nsgo-dev/game/pkg/save"
	"testing"
)

func TestHeadersListWithoutDecodingData(t *testing.T) {
	s := save.NewFileStore(t.TempDir())
	if err := s.SaveWithHeader("slot", map[string]any{"chapter": "one"}, map[string]any{"world": "large"}); err != nil {
		t.Fatal(err)
	}
	h, err := s.Headers()
	if err != nil {
		t.Fatal(err)
	}
	if string(h["slot"]) != "{\"chapter\":\"one\"}" {
		t.Fatalf("headers=%s", h["slot"])
	}
}

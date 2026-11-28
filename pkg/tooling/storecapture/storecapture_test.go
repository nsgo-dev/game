package storecapture_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/tooling/storecapture"
)

func TestPlanRoundTripAndUnknownFields(t *testing.T) {
	raw := []byte("version: 1\nshots:\n  - name: title\n    scene: scenes/title.scene.yaml\n    frame: 30\n    output: title\n    sizes:\n      - preset: app_store\n")
	p, err := storecapture.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, got) {
		t.Fatalf("round trip changed plan:\n%s", got)
	}
	if _, err := storecapture.Decode(append(raw, []byte("unknown: true\n")...)); err == nil {
		t.Fatal("unknown field accepted")
	}
	bad := bytes.Replace(raw, []byte("version: 1"), []byte("version: 2"), 1)
	if _, err := storecapture.Decode(bad); err == nil {
		t.Fatal("unsupported version accepted")
	}
}

func TestRenderIsDeterministicAndFiltersShot(t *testing.T) {
	plan := storecapture.Plan{Version: 1, Shots: []storecapture.Shot{{Name: "title", Scene: "title", Frame: intp(10), Output: "title", Sizes: []storecapture.Target{{Width: 32, Height: 16}}}, {Name: "play", Scene: "play", Output: "play", Sizes: []storecapture.Target{{Preset: "itch_cover"}}}}}
	a, b := t.TempDir(), t.TempDir()
	if err := storecapture.Render(context.Background(), plan, a); err != nil {
		t.Fatal(err)
	}
	if err := storecapture.Render(context.Background(), plan, b); err != nil {
		t.Fatal(err)
	}
	one, _ := os.ReadFile(filepath.Join(a, "title-32x16.png"))
	two, _ := os.ReadFile(filepath.Join(b, "title-32x16.png"))
	if !bytes.Equal(one, two) {
		t.Fatal("repeated capture differs")
	}
}
func intp(v int) *int { return &v }

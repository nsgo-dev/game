package navbake3d_test

import (
	"bytes"
	"flag"
	"os"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai/navbake3d"
)

var update = flag.Bool("update", false, "rewrite testdata/reference.navmesh.yaml from the current bake")

// TestBakeMatchesGolden bakes the reference level and compares the file byte for byte with the committed one, so a bake that differs between CPU architectures (CI runs this on Linux x86 and macOS arm64) or after a change to the pipeline fails here.
func TestBakeMatchesGolden(t *testing.T) {
	f, err := navbake3d.Bake(referenceLevel(t), referenceSettings, []navbake3d.AgentType{referenceAgent, {Name: "small", Radius: 0.2, Height: 1, MaxClimb: 0.3}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := navbake3d.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/reference.navmesh.yaml"
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		gl, wl := bytes.Split(got, []byte("\n")), bytes.Split(want, []byte("\n"))
		for i := range min(len(gl), len(wl)) {
			if !bytes.Equal(gl[i], wl[i]) {
				t.Fatalf("the bake differs from %s at line %d:\n got %s\nwant %s\n(run with -update after an intended change)", path, i+1, gl[i], wl[i])
			}
		}
		t.Fatalf("the bake has %d lines, %s has %d (run with -update after an intended change)", len(gl), path, len(wl))
	}
}

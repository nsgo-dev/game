package assetcheck_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
)

func buildPointLightsSceneYAML(n int, withShadowSlot bool) string {
	var b strings.Builder
	b.WriteString("version: 4\nname: Lamps\nlayers:\n  - name: world\n    objects:\n")
	if withShadowSlot {
		b.WriteString("      - id: shadow_pt\n        light:\n          kind: point\n          shadows: true\n")
		b.WriteString("      - id: shadow_spot\n        light:\n          kind: spot\n          shadows: true\n")
	}
	for i := range n {
		fmt.Fprintf(&b, "      - id: lamp_%02d\n        posZ: %d\n        light:\n          kind: point\n          range: 4\n", i, -i)
	}
	return b.String()
}

func TestCheckSceneLightFallbackCapWarning(t *testing.T) {
	// Exactly 16 unshadowed local lights + 1 shadow point + 1 shadow spot fits the fallback cap cleanly.
	fsOK := fstest.MapFS{
		"scenes/ok.scene.yaml": {Data: []byte(buildPointLightsSceneYAML(render3d.DefaultFallbackMaxLights, true))},
	}
	repOK, err := assetcheck.Check(fsOK)
	if err != nil {
		t.Fatalf("Check ok: %v", err)
	}
	if !repOK.OK() {
		t.Fatalf("16 unshadowed + 2 shadow slot-0 lights should pass, got: %+v", repOK.Problems)
	}

	// 64 unshadowed point lights exceeds the 16-light fallback cap by 48, reported at the 17th light (lamp_16).
	yaml64 := buildPointLightsSceneYAML(64, false)
	fs64 := fstest.MapFS{
		"scenes/corridor.scene.yaml": {Data: []byte(yaml64)},
	}
	rep64, err := assetcheck.Check(fs64)
	if err != nil {
		t.Fatalf("Check 64: %v", err)
	}
	if len(rep64.Problems) != 1 {
		t.Fatalf("got %d problems, want 1: %+v", len(rep64.Problems), rep64.Problems)
	}
	p := rep64.Problems[0]
	if p.File != "scenes/corridor.scene.yaml" {
		t.Errorf("File = %q, want scenes/corridor.scene.yaml", p.File)
	}
	if p.Object != "lamp_16" {
		t.Errorf("Object = %q, want lamp_16", p.Object)
	}
	if p.Line <= 0 {
		t.Errorf("Line = %d, want positive line number of lamp_16", p.Line)
	}
	// Verify p.Line actually points to lamp_16's light key in the YAML.
	lines := strings.Split(yaml64, "\n")
	if p.Line > len(lines) || !strings.Contains(lines[p.Line-1], "light:") {
		t.Errorf("Line %d (%q) does not point at lamp_16's light field", p.Line, lines[min(p.Line-1, len(lines)-1)])
	}
	for _, want := range []string{"warning:", "64 local lights", "16-light fallback cap", "48 dropped"} {
		if !strings.Contains(p.Message, want) {
			t.Errorf("Message = %q, want substring %q", p.Message, want)
		}
	}
}

func TestCheckSceneDirectionalAndHiddenAndInstancedLightCaps(t *testing.T) {
	// 5 directional lights warns at the 5th directional light (1 dropped).
	var dirYAML strings.Builder
	dirYAML.WriteString("version: 4\nlayers:\n  - name: world\n    objects:\n")
	for i := range 5 {
		fmt.Fprintf(&dirYAML, "      - id: sun_%d\n        light:\n          kind: directional\n", i)
	}
	// Hidden layer with 30 point lights must NOT trigger a warning.
	dirYAML.WriteString("  - name: hidden_lights\n    visible: false\n    objects:\n")
	for i := range 30 {
		fmt.Fprintf(&dirYAML, "      - id: h_%02d\n        light:\n          kind: point\n", i)
	}

	repDir, err := assetcheck.Check(fstest.MapFS{
		"scenes/suns.scene.yaml": {Data: []byte(dirYAML.String())},
	})
	if err != nil {
		t.Fatalf("Check dirs: %v", err)
	}
	if len(repDir.Problems) != 1 {
		t.Fatalf("got %d problems, want 1: %+v", len(repDir.Problems), repDir.Problems)
	}
	if p := repDir.Problems[0]; p.Object != "sun_4" || p.Line <= 0 || !strings.Contains(p.Message, "warning:") || !strings.Contains(p.Message, "1 dropped") {
		t.Errorf("unexpected directional light problem: %+v", p)
	}

	// Instancing two 10-light scenes combines to 20 local lights (4 dropped on fallback) and warns on the parent scene.
	childYAML := buildPointLightsSceneYAML(10, false)
	parentYAML := "version: 4\nlayers:\n  - name: world\n    objects:\n      - id: wing_a\n        scene: scenes/wing.scene.yaml\n      - id: wing_b\n        scene: scenes/wing.scene.yaml\n"
	repInst, err := assetcheck.Check(fstest.MapFS{
		"nsgo.yaml":              {Data: []byte("name: test\n")},
		"scenes/wing.scene.yaml": {Data: []byte(childYAML)},
		"scenes/hall.scene.yaml": {Data: []byte(parentYAML)},
	})
	if err != nil {
		t.Fatalf("Check instanced: %v", err)
	}
	if len(repInst.Problems) != 1 {
		t.Fatalf("got %d problems, want 1: %+v", len(repInst.Problems), repInst.Problems)
	}
	if p := repInst.Problems[0]; p.File != "scenes/hall.scene.yaml" || p.Line <= 0 || !strings.Contains(p.Message, "20 local lights") || !strings.Contains(p.Message, "4 dropped") {
		t.Errorf("unexpected instanced scene light problem: %+v", p)
	}
}

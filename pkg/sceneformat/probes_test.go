package sceneformat_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/sceneformat"
)

func TestReflectionAndLightProbesRoundTripAndValidation(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Name:    "Probe Room",
		Layers: []sceneformat.LayerDoc{{
			Name: "world",
			Objects: []sceneformat.ObjectDoc{
				{
					ID:   "refl_main",
					Name: "Room Reflection Probe",
					Y:    1.5,
					ReflectionProbe: &sceneformat.ReflectionProbeDoc{
						Center:        &[3]float32{0, 0.5, 0},
						Size:          &[3]float32{12, 5, 10},
						BlendDistance: 0.75,
						Intensity:     sceneformat.Float32(1.2),
						Priority:      2,
						BoxProjection: sceneformat.Bool(true),
						Resolution:    64,
						Cubemap:       "probes/room_refl.hdr",
					},
				},
				{
					ID:   "lpg_main",
					Name: "Room Light Probe Grid",
					Y:    1.5,
					LightProbeGrid: &sceneformat.LightProbeGridDoc{
						Center:    &[3]float32{0, 0.25, 0},
						Size:      &[3]float32{10, 4, 8},
						Counts:    &[3]int{3, 2, 3},
						Intensity: sceneformat.Float32(0.9),
						Data:      "probes/room_lpg.probes.yaml",
					},
				},
			},
		}},
	}

	raw1, err := sceneformat.Encode(doc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	parsed, err := sceneformat.Parse(raw1)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	raw2, err := sceneformat.Encode(parsed)
	if err != nil {
		t.Fatalf("re-Encode: %v", err)
	}
	if !bytes.Equal(raw1, raw2) {
		t.Fatalf("round-trip mismatch:\n%s\nvs\n%s", raw1, raw2)
	}

	rp := parsed.Layers[0].Objects[0].ReflectionProbe
	if rp == nil || rp.Cubemap != "probes/room_refl.hdr" || rp.EffectiveIntensity() != 1.2 || !rp.EffectiveBoxProjection() || rp.EffectiveResolution() != 64 {
		t.Fatalf("parsed ReflectionProbe = %+v", rp)
	}
	lpg := parsed.Layers[0].Objects[1].LightProbeGrid
	if lpg == nil || lpg.Data != "probes/room_lpg.probes.yaml" || lpg.EffectiveCounts() != ([3]int{3, 2, 3}) || lpg.EffectiveIntensity() != 0.9 {
		t.Fatalf("parsed LightProbeGrid = %+v", lpg)
	}

	// Version 3 rejects probes.
	v3 := doc.Clone()
	v3.Version = 3
	if err := sceneformat.Validate(v3); err == nil || !strings.Contains(err.Error(), "version 4") {
		t.Errorf("v3 validate err = %v, want version 4 error", err)
	}

	// Bad reflection probe cubemap extension rejected.
	badCubemap := doc.Clone()
	badCubemap.Layers[0].Objects[0].ReflectionProbe.Cubemap = "probes/room.png"
	if err := sceneformat.Validate(badCubemap); err == nil {
		t.Error("expected error for non-HDR cubemap path")
	}

	// Non-positive probe size rejected.
	badSize := doc.Clone()
	badSize.Layers[0].Objects[0].ReflectionProbe.Size = &[3]float32{4, 0, 4}
	if err := sceneformat.Validate(badSize); err == nil {
		t.Error("expected error for zero Y extent in reflection probe size")
	}

	// Excessive grid counts rejected.
	badCounts := doc.Clone()
	badCounts.Layers[0].Objects[1].LightProbeGrid.Counts = &[3]int{100, 100, 100}
	if err := sceneformat.Validate(badCounts); err == nil {
		t.Error("expected error for oversized light probe grid counts")
	}

	// Merge disjoint edits on probes cleanly.
	ours := doc.Clone()
	theirs := doc.Clone()
	ours.Layers[0].Objects[0].ReflectionProbe.BlendDistance = 1.5
	ours.Layers[0].Objects[1].LightProbeGrid.Counts = &[3]int{4, 2, 3}
	theirs.Layers[0].Objects[0].ReflectionProbe.Priority = 5
	theirs.Layers[0].Objects[1].LightProbeGrid.Data = "probes/room_v2.probes.yaml"

	res, err := sceneformat.Merge(doc, ours, theirs)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if res.HasConflicts() {
		t.Fatalf("expected clean probe merge, got conflicts: %+v", res.Conflicts)
	}
	gotRP := res.Doc.Layers[0].Objects[0].ReflectionProbe
	gotLPG := res.Doc.Layers[0].Objects[1].LightProbeGrid
	if gotRP.BlendDistance != 1.5 || gotRP.Priority != 5 {
		t.Errorf("merged ReflectionProbe = %+v, want blendDistance=1.5 priority=5", gotRP)
	}
	if *gotLPG.Counts != ([3]int{4, 2, 3}) || gotLPG.Data != "probes/room_v2.probes.yaml" {
		t.Errorf("merged LightProbeGrid = %+v", gotLPG)
	}
}

package anim3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/internal/loaderrtest"
	"github.com/nsgo-dev/game/pkg/anim3d"
	"github.com/nsgo-dev/game/pkg/loaderr"
)

func TestEventLoadersLocateErrors(t *testing.T) {
	cases := []struct {
		name string
		load func([]byte) (map[string][]anim3d.ClipEvent, error)
		data string
		line int
	}{
		{"sidecar syntax", anim3d.LoadEventsSidecar, "{\n  \"version\": 1,\n  \"clips\": {\n    \"run\": [\n      {\"time\": 0.5,, \"name\": \"step\"}\n    ]\n  }\n}", 5},
		{"sidecar wrong type", anim3d.LoadEventsSidecar, "{\n  \"version\": 1,\n  \"clips\": {\n    \"run\": [\n      {\"time\": \"late\", \"name\": \"step\"}\n    ]\n  }\n}", 5},
		{"flat sidecar wrong type", anim3d.LoadEventsSidecar, "{\n  \"run\": [\n    {\"time\": 0.1},\n    {\"name\": 7}\n  ]\n}", 4},
		{"gltf syntax", anim3d.LoadEventsFromGLTFExtras, "{\n  \"animations\": [\n    {\"name\": \"run\"\n  ]\n}", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.load([]byte(tc.data))
			loaderrtest.Expect(t, loaderr.WithFile(err, "hero.events.json"), "hero.events.json", tc.line)
		})
	}
}

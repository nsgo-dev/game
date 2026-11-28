package editormcp_test

import (
	"encoding/json"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

func TestValidatePlayStep(t *testing.T) {
	for _, n := range []int{1, 60, 3600} {
		if err := editormcp.ValidatePlayStep(editormcp.PlayStepArgs{Count: n}); err != nil {
			t.Errorf("count %d: %v", n, err)
		}
	}
	for _, n := range []int{-1, 0, 3601} {
		if editormcp.ValidatePlayStep(editormcp.PlayStepArgs{Count: n}) == nil {
			t.Errorf("count %d accepted", n)
		}
	}
}

func TestValidatePlayInput(t *testing.T) {
	ok := []editormcp.PlayInputArgs{
		{Action: "right", Event: editormcp.PlayInputPress},
		{Action: "right", Event: editormcp.PlayInputRelease},
		{Action: "jump", Event: editormcp.PlayInputTap},
		{Action: "move_x", Event: editormcp.PlayInputAxis, Value: -1},
		{Action: "move_x", Event: editormcp.PlayInputAxis, Value: 0.5},
		{Action: "aim", Event: editormcp.PlayInputPointer, X: 120, Y: 64},
	}
	for _, a := range ok {
		if err := editormcp.ValidatePlayInput(a); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	bad := []editormcp.PlayInputArgs{
		{Event: editormcp.PlayInputPress},
		{Action: "right", Event: "bogus"},
		{Action: "right"},
		{Action: "move_x", Event: editormcp.PlayInputAxis, Value: 1.5},
		{Action: "move_x", Event: editormcp.PlayInputAxis, Value: -2},
	}
	for _, a := range bad {
		if editormcp.ValidatePlayInput(a) == nil {
			t.Errorf("%+v accepted", a)
		}
	}
}

func TestPlayContractsUseSnakeCaseJSON(t *testing.T) {
	var in editormcp.PlayInputArgs
	if err := json.Unmarshal([]byte(`{"action":"right","event":"axis","value":0.25}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.Action != "right" || in.Event != editormcp.PlayInputAxis || in.Value != 0.25 {
		t.Fatalf("decoded %+v", in)
	}
	state := editormcp.PlayState{Playing: true, Step: 60, Scene: "level.scene.yaml", Entities: []editormcp.PlayEntity{{ID: "e1", Name: "hero", X: 251}}}
	got, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"playing":true,"paused":false,"step":60,"scene":"level.scene.yaml","entities":[{"id":"e1","name":"hero","x":251,"y":0,"z":0}]}`
	if string(got) != want {
		t.Fatalf("PlayState JSON\n got %s\nwant %s", got, want)
	}
	entry, err := json.Marshal(editormcp.OutputEntry{Cursor: 7, Level: "error", Message: "panic: nil map"})
	if err != nil {
		t.Fatal(err)
	}
	if string(entry) != `{"cursor":7,"level":"error","message":"panic: nil map"}` {
		t.Fatalf("OutputEntry JSON %s", entry)
	}
}

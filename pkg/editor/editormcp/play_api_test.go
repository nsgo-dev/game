package editormcp_test

import (
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"testing"
)

func TestPlayArgumentsValidate(t *testing.T) {
	if err := editormcp.ValidatePlayStep(editormcp.PlayStepArgs{Count: 60}); err != nil {
		t.Fatal(err)
	}
	if editormcp.ValidatePlayStep(editormcp.PlayStepArgs{Count: 3601}) == nil {
		t.Fatal("accepted oversized step")
	}
	if editormcp.ValidatePlayInput(editormcp.PlayInputArgs{Action: "right", Event: "press"}) != nil {
		t.Fatal("rejected press")
	}
	if editormcp.ValidatePlayInput(editormcp.PlayInputArgs{Action: "right", Event: "bogus"}) == nil {
		t.Fatal("accepted bogus input")
	}
}

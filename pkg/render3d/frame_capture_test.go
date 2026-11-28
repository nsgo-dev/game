package render3d_test

import (
	"github.com/nsgo-dev/game/pkg/render3d"
	"testing"
)

func TestFrameCaptureArmsOneFrame(t *testing.T) {
	var c render3d.FrameCapture
	c.BeginPass("ignored", "")
	c.Arm()
	if !c.BeginPass("opaque", "swap") {
		t.Fatal("begin")
	}
	c.Draw(render3d.CaptureDraw{Name: "cube", Triangles: 12})
	got := c.Finish()
	if len(got) != 1 || len(got[0].Draws) != 1 || got[0].Draws[0].Name != "cube" {
		t.Fatalf("%+v", got)
	}
	if c.Finish() != nil {
		t.Fatal("capture repeated")
	}
}

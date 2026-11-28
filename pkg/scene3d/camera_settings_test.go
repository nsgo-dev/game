package scene3d

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/save"
)

func TestApplyCameraSettings(t *testing.T) {
	cam := NewPerspectiveCamera(math3d.Vec3{}, 0, 1, 0.1, 100)
	cam.ApplyCameraSettings(save.CameraSettings{FieldOfView: 110, MotionBlur: true})
	if math.Abs(float64(cam.Projection.FOVY-math.Pi*110/180)) > 1e-5 {
		t.Fatalf("FOVY = %v", cam.Projection.FOVY)
	}
	if !cam.MotionBlur {
		t.Fatal("MotionBlur = false, want true")
	}
	prev := NewPerspectiveCamera(math3d.Vec3{}, 0, 1, 0.1, 100)
	if !prev.Lerp(cam, 0.5).MotionBlur {
		t.Fatal("Lerp dropped MotionBlur")
	}
	ortho := NewOrthographicCamera(math3d.Vec3{}, 10, 1, 0.1, 100)
	before := ortho.Projection
	ortho.ApplyCameraSettings(save.CameraSettings{FieldOfView: 60})
	if ortho.Projection != before {
		t.Fatalf("orthographic projection changed: %+v", ortho.Projection)
	}
	fps := NewFPSController(math3d.Vec3{})
	fps.ApplyCameraSettings(save.CameraSettings{CameraBob: 0.25, CameraSway: 0.5})
	if fps.CameraBob != 0.25 || fps.CameraSway != 0.5 {
		t.Fatalf("fps effects = %v, %v", fps.CameraBob, fps.CameraSway)
	}
	tp := NewThirdPersonController()
	tp.ApplyCameraSettings(save.CameraSettings{CameraBob: 2, CameraSway: 0})
	if tp.CameraBob != 1 || tp.CameraSway != 0 {
		t.Fatalf("third-person effects = %v, %v, want 1, 0", tp.CameraBob, tp.CameraSway)
	}
}

package tween

import (
	"time"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func effectiveZoom(z float32) float32 {
	if z <= 0 {
		return 1
	}
	return z
}

// CameraPan returns a tween that pans cam from its position when the tween starts to (toX, toY) over d with curve, clamping to cam.Bounds if set.
func CameraPan(cam *tilemap.Camera, toX, toY float32, d time.Duration, curve core.Curve) *Tween {
	if d < 0 {
		d = 0
	}
	if cam == nil {
		return &Tween{dur: d, curve: curve}
	}
	var fromX, fromY float32
	return &Tween{
		dur:   d,
		curve: curve,
		onStart: func() {
			fromX, fromY = cam.X, cam.Y
		},
		apply: func(t float64) {
			cam.X = lerpFloat32(fromX, toX, t)
			cam.Y = lerpFloat32(fromY, toY, t)
			cam.Clamp()
		},
	}
}

// CameraZoom returns a tween that zooms cam from its zoom when the tween starts (treating <= 0 as 1) to toZoom over d with curve, clamping to cam.Bounds if set.
func CameraZoom(cam *tilemap.Camera, toZoom float32, d time.Duration, curve core.Curve) *Tween {
	if d < 0 {
		d = 0
	}
	if cam == nil {
		return &Tween{dur: d, curve: curve}
	}
	var fromZoom float32
	return &Tween{
		dur:   d,
		curve: curve,
		onStart: func() {
			fromZoom = effectiveZoom(cam.Zoom)
		},
		apply: func(t float64) {
			cam.Zoom = lerpFloat32(fromZoom, toZoom, t)
			cam.Clamp()
		},
	}
}

// CameraRotate returns a tween that rotates cam from its rotation when the tween starts to toRot (in radians) over d with curve, clamping to cam.Bounds if set.
func CameraRotate(cam *tilemap.Camera, toRot float32, d time.Duration, curve core.Curve) *Tween {
	if d < 0 {
		d = 0
	}
	if cam == nil {
		return &Tween{dur: d, curve: curve}
	}
	var fromRot float32
	return &Tween{
		dur:   d,
		curve: curve,
		onStart: func() {
			fromRot = cam.Rotation
		},
		apply: func(t float64) {
			cam.Rotation = lerpFloat32(fromRot, toRot, t)
			cam.Clamp()
		},
	}
}

// CameraMove returns a tween that moves cam simultaneously to (toX, toY), toZoom, and toRot over d with curve, clamping to cam.Bounds if set.
func CameraMove(cam *tilemap.Camera, toX, toY, toZoom, toRot float32, d time.Duration, curve core.Curve) *Tween {
	if d < 0 {
		d = 0
	}
	if cam == nil {
		return &Tween{dur: d, curve: curve}
	}
	var fromX, fromY, fromZoom, fromRot float32
	return &Tween{
		dur:   d,
		curve: curve,
		onStart: func() {
			fromX, fromY = cam.X, cam.Y
			fromZoom = effectiveZoom(cam.Zoom)
			fromRot = cam.Rotation
		},
		apply: func(t float64) {
			cam.X = lerpFloat32(fromX, toX, t)
			cam.Y = lerpFloat32(fromY, toY, t)
			cam.Zoom = lerpFloat32(fromZoom, toZoom, t)
			cam.Rotation = lerpFloat32(fromRot, toRot, t)
			cam.Clamp()
		},
	}
}

// CameraFollowTransition returns a tween that smoothly moves cam from its position when the tween starts onto a moving target position sampled from target each step, ending directly on target() (clamped to cam.Bounds if set).
func CameraFollowTransition(cam *tilemap.Camera, target func() core.Point, d time.Duration, curve core.Curve) *Tween {
	if d < 0 {
		d = 0
	}
	if cam == nil {
		return &Tween{dur: d, curve: curve}
	}
	var fromX, fromY float32
	return &Tween{
		dur:   d,
		curve: curve,
		onStart: func() {
			fromX, fromY = cam.X, cam.Y
		},
		apply: func(t float64) {
			if target == nil {
				cam.X, cam.Y = fromX, fromY
				cam.Clamp()
				return
			}
			dst := target()
			cam.X = lerpFloat32(fromX, float32(dst.X), t)
			cam.Y = lerpFloat32(fromY, float32(dst.Y), t)
			cam.Clamp()
		},
	}
}

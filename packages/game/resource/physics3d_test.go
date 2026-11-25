package resource_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/resource"
)

func TestPhysicsMaterialApplyTo3D(t *testing.T) {
	pm, err := resource.ParseAs[*resource.PhysicsMaterial](readTestdata(t, "ice.physics.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{}, 1)
	body.AngularDamping = 0.3
	pm.ApplyTo3D(&body)
	if body.Friction != 0.02 || body.Restitution != 0.1 || body.LinearDamping != 0.05 || body.AngularDamping != 0.3 {
		t.Fatalf("ApplyTo3D body = friction %v restitution %v linear %v angular %v",
			body.Friction, body.Restitution, body.LinearDamping, body.AngularDamping)
	}
	// Nil safety.
	var nilPM *resource.PhysicsMaterial
	nilPM.ApplyTo3D(&body)
	pm.ApplyTo3D(nil)

	// Library.PhysicsMaterial resolves shared .physics.yaml resources.
	lib := resource.NewLibrary(libraryFS())
	loaded, err := lib.PhysicsMaterial("res/ice.physics.yaml")
	if err != nil {
		t.Fatalf("Library.PhysicsMaterial: %v", err)
	}
	if loaded.Friction == nil || *loaded.Friction != 0.02 {
		t.Fatalf("loaded ice friction = %v, want 0.02", loaded.Friction)
	}
}

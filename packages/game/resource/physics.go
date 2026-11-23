package resource

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/physics"
)

// PhysicsMaterial holds the surface properties of rigid bodies: friction, bounce and damping. A field left out keeps the body's own value when applied.
type PhysicsMaterial struct {
	Header `yaml:",inline"`
	// Name is an optional human-readable name.
	Name string `yaml:"name,omitempty"`
	// Friction is the Coulomb friction coefficient; it must be finite and non-negative.
	Friction *float32 `yaml:"friction,omitempty"`
	// Restitution is the bounce from 0, which stops dead, to 1, which bounces back fully.
	Restitution *float32 `yaml:"restitution,omitempty"`
	// LinearDamping and AngularDamping slow movement and spin each second; they must be finite and non-negative.
	LinearDamping  *float32 `yaml:"linearDamping,omitempty"`
	AngularDamping *float32 `yaml:"angularDamping,omitempty"`
}

// Kind returns KindPhysicsMaterial.
func (p *PhysicsMaterial) Kind() Kind { return KindPhysicsMaterial }

// Validate checks the header and the ranges.
func (p *PhysicsMaterial) Validate() error {
	if p == nil {
		return fmt.Errorf("resource: nil physics material")
	}
	if err := p.Header.validate(KindPhysicsMaterial); err != nil {
		return err
	}
	if p.Friction != nil {
		if err := checkNonNegative("physics material friction", *p.Friction); err != nil {
			return err
		}
	}
	if p.Restitution != nil {
		if err := checkUnit("physics material restitution", *p.Restitution); err != nil {
			return err
		}
	}
	if p.LinearDamping != nil {
		if err := checkNonNegative("physics material linearDamping", *p.LinearDamping); err != nil {
			return err
		}
	}
	if p.AngularDamping != nil {
		if err := checkNonNegative("physics material angularDamping", *p.AngularDamping); err != nil {
			return err
		}
	}
	return nil
}

// ApplyTo sets the fields the material holds on body, leaving the others as they are.
func (p *PhysicsMaterial) ApplyTo(body *physics.RigidBody) {
	if p == nil || body == nil {
		return
	}
	if p.Friction != nil {
		body.Friction = *p.Friction
	}
	if p.Restitution != nil {
		body.Restitution = *p.Restitution
	}
	if p.LinearDamping != nil {
		body.LinearDamping = *p.LinearDamping
	}
	if p.AngularDamping != nil {
		body.AngularDamping = *p.AngularDamping
	}
}

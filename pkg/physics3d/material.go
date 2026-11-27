package physics3d

// PhysicsMaterial holds surface and damping properties for 3D physics bodies.
// Assign a *PhysicsMaterial to Body.Material to share properties across bodies,
// or copy it onto one body with Body.ApplyMaterial.
type PhysicsMaterial struct {
	// Friction is the Coulomb friction coefficient (>= 0).
	Friction float32 `json:"friction"`
	// Restitution is the bounciness in [0, 1].
	Restitution float32 `json:"restitution"`
	// LinearDamping reduces linear velocity smoothly over time (>= 0).
	LinearDamping float32 `json:"linearDamping"`
	// AngularDamping reduces angular velocity smoothly over time (>= 0).
	AngularDamping float32 `json:"angularDamping"`
}

// ApplyMaterial copies m's surface and damping properties onto b, clamping them
// to valid ranges, and clears b.Material so the copied values take effect directly.
func (b *Body) ApplyMaterial(m PhysicsMaterial) {
	if b == nil {
		return
	}
	b.Material = nil
	b.Friction = clampNonNegative32(m.Friction)
	b.Restitution = clampUnit32(m.Restitution)
	b.LinearDamping = clampNonNegative32(m.LinearDamping)
	b.AngularDamping = clampNonNegative32(m.AngularDamping)
}

func (b *Body) effectiveFriction() float32 {
	if b == nil {
		return 0
	}
	if b.Material != nil {
		return clampNonNegative32(b.Material.Friction)
	}
	return clampNonNegative32(b.Friction)
}

func (b *Body) effectiveRestitution() float32 {
	if b == nil {
		return 0
	}
	if b.Material != nil {
		return clampUnit32(b.Material.Restitution)
	}
	return clampUnit32(b.Restitution)
}

func (b *Body) effectiveLinearDamping() float32 {
	if b == nil {
		return 0
	}
	if b.Material != nil {
		return clampNonNegative32(b.Material.LinearDamping)
	}
	return clampNonNegative32(b.LinearDamping)
}

func (b *Body) effectiveAngularDamping() float32 {
	if b == nil {
		return 0
	}
	if b.Material != nil {
		return clampNonNegative32(b.Material.AngularDamping)
	}
	return clampNonNegative32(b.AngularDamping)
}

func clampNonNegative32(v float32) float32 {
	if !isFinite32(v) || v < 0 {
		return 0
	}
	return v
}

func clampUnit32(v float32) float32 {
	if !isFinite32(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

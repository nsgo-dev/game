package physics

// SetTargetPose moves a kinematic body to (x, y) at angle over the next Step, the way an animation drives a moving platform. That Step sets VX, VY and AngularVel to the velocity that covers the distance in one step, so the bodies the platform pushes and carries move with it, and ends with the body exactly at the pose. A body given no new target before the following Step stops there, unless its velocity was changed in between. Step ignores a target while the body is not Kinematic, and SetTargetPose ignores a pose that is not finite.
//
// Assigning X, Y and Angle directly also moves a kinematic body, but as a teleport: the bodies it lands in are pushed out without being given its motion.
func (b *RigidBody) SetTargetPose(x, y, angle float32) {
	if b == nil || !isFinite32(x) || !isFinite32(y) || !isFinite32(angle) {
		return
	}
	b.target = rigidPose{x: x, y: y, angle: angle}
	b.targetPending = true
}

// driveKinematic gives a kinematic body the velocity its pending target asks for over a step of h seconds, or stops a body still moving at the velocity the last target gave it. A body that is not kinematic drops its target.
func (b *RigidBody) driveKinematic(h float64) {
	if !b.Kinematic || b.Static {
		b.targetPending = false
		b.targetDriven = false
		return
	}
	if b.targetPending {
		vx := float32((float64(b.target.x) - float64(b.X)) / h)
		vy := float32((float64(b.target.y) - float64(b.Y)) / h)
		av := float32((float64(b.target.angle) - float64(b.Angle)) / h)
		if !isFinite32(vx) || !isFinite32(vy) || !isFinite32(av) {
			// A body at a pose that is not finite, or a step too short for the distance, cannot be driven there.
			b.targetPending = false
			return
		}
		b.VX, b.VY, b.AngularVel = vx, vy, av
		b.targetVX, b.targetVY, b.targetAV = b.VX, b.VY, b.AngularVel
		b.targetDriven = true
		return
	}
	if b.targetDriven {
		if b.VX == b.targetVX && b.VY == b.targetVY && b.AngularVel == b.targetAV {
			b.VX, b.VY, b.AngularVel = 0, 0, 0
		}
		b.targetDriven = false
	}
}

// kinematicMovedFrom reports whether b is a kinematic body whose pose this step differs from start, the pose it began the step's position integration with.
func kinematicMovedFrom(b *RigidBody, start rigidPose) bool {
	return b.Kinematic && !b.Static && (b.X != start.x || b.Y != start.y || b.Angle != start.angle)
}

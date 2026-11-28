// Package motion provides deterministic, rate-independent 2D motion helpers for
// games that drive objects from Go code without scene files, sharing the exact
// math and semantics of sceneformat's built-in 2D motion behavior components:
//
//   - Bullet: linear projectile motion with speed, acceleration, gravity, optional
//     angle alignment and continuous swept bouncing off solid geometry.
//   - Sine: periodic oscillation (sine, triangle, sawtooth, reverse_sawtooth,
//     square) with phase and magnitude applied as drift-free deltas.
//   - Rotate: angular velocity and angular acceleration in degrees per second.
//   - Orbit: circular motion around a fixed point or moving target.
//   - MoveTo: direct movement toward a target point with optional acceleration
//     and solid stopping.
//   - Wrap: toroidal edge wrapping across scene or camera view bounds.
//   - Bound: clamping an object's edge box or origin inside bounds.
//   - DestroyOutside: detecting when an object leaves expanded bounds.
//   - Fade: fade-in, hold (wait) and fade-out opacity envelope.
//   - Flash: timed on/off visibility blinking with base-alpha restoration.
//
// All trigonometry uses internal/detmath so arm64, amd64 and wasm produce
// bit-for-bit identical trajectories. Constant-acceleration and closed-form
// stepping ensure fixed-rate simulations (such as 60 Hz vs 120 Hz) reach the
// same positions per simulated second.
package motion

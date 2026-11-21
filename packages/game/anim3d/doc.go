// Package anim3d provides 3D skeletal animation state machines, inverse
// kinematics solvers, and root motion extraction for NSGo 3D games.
//
// All transforms, vectors, and quaternions follow package math3d's right-handed
// Y-up conventions (+X right, +Y up, -Z forward). Time advances in fixed
// time.Duration steps matching game.Loop.Update.
package anim3d

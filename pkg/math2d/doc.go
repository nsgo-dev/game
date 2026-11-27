// Package math2d provides small, zero-allocation 2D value types shared by game packages:
// vectors (Vec2), axis-aligned rectangles (Rect), circles (Circle), and 3×2 affine
// transforms (Affine).
//
// Coordinates use float32 to match NSGo's core geometry (core.Points, core.Point,
// core.Size, core.Bounds) and core.Drawing, with lossless conversions to and from core.
// Angles are in radians, turning from +X toward +Y (clockwise in NSGo's Y-down drawing
// coordinates).
package math2d

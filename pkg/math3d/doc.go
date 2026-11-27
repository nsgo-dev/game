// Package math3d provides 3D linear algebra, rotations, transforms, and spatial
// primitives for NSGo 3D games using pure Go float32 types with zero external
// dependencies.
//
// # Conventions
//
// Every type and function in this package follows one consistent set of
// conventions aligned with glTF 2.0, WebGPU, and Metal so matrices and vectors
// can be uploaded directly to GPU uniform buffers without transposing or
// flipping handedness:
//
//   - Coordinate system: Right-handed, Y-up.
//     +X points right (Vec3Right), +Y points up (Vec3Up), and -Z points forward
//     (Vec3Forward). A default camera at the origin looks down -Z; +Z points
//     toward the viewer. Cross products obey the right-hand rule:
//     X × Y = Z, Y × Z = X, Z × X = Y.
//
//   - Angles and rotations: All angles are in radians (use DegToRad and
//     RadToDeg to convert). Positive rotation angles are counter-clockwise when
//     looking from the positive tip of the rotation axis toward the origin
//     (right-hand thumb rule).
//
//   - Matrix layout: Mat3 ([9]float32) and Mat4 ([16]float32) are stored in
//     column-major order: column c, row r lives at index c*3+r (Mat3) or
//     c*4+r (Mat4). This matches WGSL mat4x4<f32>, Metal simd::float4x4, GLSL
//     mat4, and glTF 2.0 node matrices directly.
//
//   - Vector multiplication and composition: Vectors are treated as column
//     vectors and multiplied on the right (M * v). Matrix multiplication
//     composes right-to-left: (A.Mul(B)).MulVec4(v) == A.MulVec4(B.MulVec4(v)).
//     Model transforms use TRS order: M = T * R * S (scale, then rotate, then
//     translate). The full clip-space transform is P * V * M.
//
//   - Clip-space (NDC) depth range: Perspective and Ortho default to WebGPU /
//     Metal / Vulkan zero-to-one depth [0, 1] (also available explicitly as
//     PerspectiveZO and OrthoZO), mapping view-space z = -near to NDC z = 0
//     and z = -far to NDC z = 1. OpenGL / WebGL negative-one-to-one depth
//     [-1, 1] is available via PerspectiveNO and OrthoNO.
//
//   - Quaternions: Quat stores (X, Y, Z, W) matching glTF 2.0's [x, y, z, w]
//     order, with identity QuatIdentity() = {0, 0, 0, 1}. Unit quaternions
//     represent right-handed rotations; q and -q represent the same rotation.
//     Multiplication composes right-to-left, matching Mat4:
//     (q1.Mul(q2)).RotateVec3(v) == q1.RotateVec3(q2.RotateVec3(v)).
//     QuatFromEuler and ToEuler use intrinsic YXZ (yaw-pitch-roll:
//     Ry(yaw) * Rx(pitch) * Rz(roll)); QuatFromEulerXYZ and ToEulerXYZ use
//     intrinsic XYZ (Rx(x) * Ry(y) * Rz(z)).
//
//   - Frustums and planes: Plane represents {p | Normal·p + D = 0}.
//     FrustumFromMat4 (and FrustumFromMat4ZO) extracts six inward-pointing
//     normalized planes via Gribb-Hartmann for [0, 1] depth projections;
//     FrustumFromMat4NO extracts planes for [-1, 1] depth projections.
package math3d

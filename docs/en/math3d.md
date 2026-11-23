# 3D Math Conventions

`math3d` provides pure-Go `float32` vectors, matrices, quaternions, transforms, bounding volumes, rays, planes, and frustums with a single consistent set of conventions matching glTF 2.0, WebGPU, and Metal.

```go
import "github.com/nsgo-dev/game/packages/game/math3d"
```

## Quick Reference

| Property | Convention | Notes |
| --- | --- | --- |
| Handedness | Right-handed, Y-up | `+X` right, `+Y` up, `-Z` forward |
| Angles | Radians | Right-handed CCW looking down `+axis`; `DegToRad` / `RadToDeg` |
| Matrix storage | Column-major (`[9]float32`, `[16]float32`) | Matches WGSL `mat4x4<f32>`, Metal `simd::float4x4`, GLSL `mat4`, glTF 2.0 |
| Vector convention | Column vectors (`M * v`) | Composition runs right-to-left: `(A * B) * v = A * (B * v)` |
| Transform order | `TRS` (`T * R * S`) | Scale first, then rotate, then translate |
| Clip-space depth (NDC) | `[0, 1]` default (`ZO`) | WebGPU / Metal / Vulkan; `[-1, 1]` available via `*NO` |
| Quaternions | `(X, Y, Z, W)` | Identity `{0, 0, 0, 1}`; matches glTF 2.0 `[x, y, z, w]` |
| Euler order | Intrinsic `YXZ` default | `Ry(yaw) * Rx(pitch) * Rz(roll)`; `XYZ` via `*XYZ` |

## Coordinate System

`math3d` uses a **right-handed, Y-up** coordinate system:

- `+X` points **right** (`Vec3Right()`).
- `+Y` points **up** (`Vec3Up()`).
- `-Z` points **forward** (`Vec3Forward()`). A camera at the origin with identity view looks down `-Z`, while `+Z` points out of the screen toward the viewer.

Cross products obey the right-hand rule:

```text
+X × +Y = +Z
+Y × +Z = +X
+Z × +X = +Y
```

All rotation functions take **radians**. A positive angle rotates **counter-clockwise** when looking from the positive tip of the rotation axis toward the origin (curl the fingers of your right hand with your thumb pointing along `+axis`):

- `RotateX(+π/2)` rotates `+Y` to `+Z`.
- `RotateY(+π/2)` rotates `-Z` (forward) to `-X` (left).
- `RotateZ(+π/2)` rotates `+X` to `+Y`.

## Matrices and Storage Layout

`Mat3` (`[9]float32`) and `Mat4` (`[16]float32`) are stored in **column-major** order. The element at `(row, col)` sits at `col*3 + row` for `Mat3` and `col*4 + row` for `Mat4`:

```text
Mat4 indices:
  col 0   col 1   col 2   col 3
[  m[0]    m[4]    m[8]   m[12] ]   row 0
[  m[1]    m[5]    m[9]   m[13] ]   row 1
[  m[2]    m[6]   m[10]   m[14] ]   row 2
[  m[3]    m[7]   m[11]   m[15] ]   row 3
```

Because WGSL (`mat4x4<f32>`), Metal (`simd::float4x4`), GLSL (`mat4`), and glTF 2.0 `node.matrix` are all column-major, a `Mat4` can be uploaded directly to a GPU uniform buffer with no transpose.

Vectors are column vectors multiplied on the right (`M * v`). Matrix multiplication therefore composes **right-to-left**:

```go
// Scale first, then rotate, then translate: M = T * R * S
model := math3d.TRS(pos, rot, scale)

// Model -> World -> View -> Clip: MVP = P * V * M
mvp := proj.Mul(view).Mul(model)
```

For surface normals under non-uniform scale, transform with `NormalMatrix(model)` (the inverse-transpose of the upper-left 3×3) and re-normalize.

## Clip Space and Depth Range

`Perspective` and `Ortho` default to **zero-to-one (`[0, 1]`)** NDC depth, matching **WebGPU**, **Metal**, **Vulkan**, and **Direct3D**:

- View-space `z = -near` maps to NDC `z = 0`.
- View-space `z = -far` maps to NDC `z = 1`.
- View-space `x` and `y` map to `[-1, 1]`.

Both depth conventions are provided explicitly so code targeting OpenGL or WebGL can opt into `[-1, 1]` without custom matrices:

| Depth range | Perspective | Orthographic | Frustum extraction |
| --- | --- | --- | --- |
| `[0, 1]` (WebGPU / Metal / Vulkan) | `Perspective` / `PerspectiveZO` | `Ortho` / `OrthoZO` | `FrustumFromMat4` / `FrustumFromMat4ZO` |
| `[-1, 1]` (OpenGL / WebGL) | `PerspectiveNO` | `OrthoNO` | `FrustumFromMat4NO` |

## Quaternions and Transforms

`Quat` stores `{X, Y, Z, W}` with identity `QuatIdentity() = {0, 0, 0, 1}`, matching glTF 2.0's `[x, y, z, w]` ordering.

- **Equivalence**: `q` and `-q` represent the same 3D rotation. `Slerp` and `Nlerp` automatically negate the target when `q.Dot(b) < 0` so interpolation always follows the **shortest arc**.
- **Composition**: `q1.Mul(q2)` applies `q2` first and then `q1`, matching `q1.ToMat4().Mul(q2.ToMat4())`.
- **Euler angles**: `QuatFromEuler(pitch, yaw, roll)` and `ToEuler()` use intrinsic **YXZ** (`Ry(yaw) * Rx(pitch) * Rz(roll)`), standard for cameras and characters. Intrinsic **XYZ** (`Rx(x) * Ry(y) * Rz(z)`) is available via `QuatFromEulerXYZ` and `ToEulerXYZ`. Both extract in float64, so yaw and roll stay separate right up to the ±90° gimbal pole; only an exact lock folds roll into yaw.
- **Orientation vs. View**: `LookRotation(forward, up)` builds a world orientation quaternion whose local `-Z` points along `forward` and local `+Y` points toward `up`. For a camera at `eye` looking at `target`, `Transform{Pos: eye, Rot: LookRotation(target.Sub(eye), up), Scale: Vec3One()}.ToMat4()` is the exact inverse of `LookAt(eye, target, up)`.

`Transform` pairs `Pos`, `Rot`, and `Scale` for scene hierarchies and interpolation (`Lerp` linearly interpolates position and scale and uses `Slerp` for rotation). `parent.Mul(child)` composes TRS directly; for hierarchies that combine non-uniform parent scale with rotated children (which introduces shear), compose as `Mat4`.

## Bounds, Rays, Planes, and Frustums

- **`AABB`**: Axis-aligned bounding box `{Min, Max}`. Start incremental bounds with `AABBEmpty()`, grow with `ExpandPoint` / `Union`, and transform tightly into world space with `Transform(m)`.
- **`Sphere`**: `{Center, Radius}` with `Contains`, `IntersectsSphere`, and `IntersectsAABB`.
- **`Plane`**: `{Normal, D}` representing `Normal · p + D = 0`. Call `Normalized()` so `SignedDist(p)` returns Euclidean signed distance (positive on the side `Normal` points toward).
- **`Ray`**: Half-line `P(t) = Origin + t * Dir` (`t >= 0`) with `IntersectAABB` (slab test), `IntersectSphere`, `IntersectPlane`, and `IntersectTriangle` (double-sided Möller–Trumbore returning `(t, u, v, hit)`). Their parallel tests are relative to the vector lengths, so tiny triangles and short ray directions still hit.
- **`Frustum`**: Six inward-pointing normalized planes (`FrustumLeft` through `FrustumFar`) extracted from any perspective or orthographic view-projection matrix via Gribb–Hartmann (`FrustumFromMat4` for `[0, 1]`, `FrustumFromMat4NO` for `[-1, 1]`), supporting `ContainsPoint`, `IntersectsSphere`, and `IntersectsAABB`.

```go
view := math3d.LookAt(math3d.Vec3{X: 0, Y: 3, Z: 8}, math3d.Vec3Zero(), math3d.Vec3Up())
proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, 0.1, 200)
frustum := math3d.FrustumFromMat4(proj.Mul(view))

if frustum.IntersectsAABB(worldBounds) {
	// Object is visible.
}
```

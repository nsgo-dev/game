### Added

- `game/math3d`: `Transform.InverseMat4` returns the exact inverse matrix of any transform, including a non-uniform scale under a rotation, whose inverse shears and which `Transform.Inverse` cannot represent; the `Inverse` docs now say so (#546).
- `game/physics3d`: `DebugStyle.DepthZO` and `DrawLine3DZO` clip debug drawing at the near plane of a `[0, 1]` depth projection such as `math3d.Perspective`, `math3d.Ortho` or a `render3d` camera, as `render3d.ProjectSegment` does; the default stays the `[-1, 1]` near plane of `physics3d.Mat4Perspective` and `Mat4Ortho` (#546).

### Fixed

- `game/physics3d`: stacks no longer sink into each other or fall asleep overlapping. A bottom-up shock-propagation pass after the solver stops and pushes out each upper body against the one below, so a 100 kg box on a 1 kg box rests about 1 mm deep instead of 4 cm, and a tall stack under a heavy box settles and sleeps instead of bobbing 8 cm deep; a body more than 5 mm into a neighbor stays awake, and a sleeping body pushed deeper than 1 cm wakes (#546).
- `game/physics3d`: a shape below a heightfield's surface is pushed out by the depth measured along the upward normal it is pushed along, not the shallower depth of the downward normal it replaced (a sphere 0.3 below the surface got 0.2 instead of 0.8); a shape beside a cell, such as one at the foot of a step touching the plateau's edge, keeps its measured depth (#546).
- `game/physics3d`: debug drawing with an orthographic `[0, 1]` camera no longer has to draw colliders, contacts and rays behind the camera; set `DebugStyle.DepthZO` (#546).
- `game/scene3d`: `ReparentKeepWorld` (and `SetParent` with `keepWorld`) solves the new local transform on world matrices, so a node under a rotated, non-uniformly scaled parent keeps its world matrix; when that local transform would need shear, or the parent is singular, it returns the new `ErrNotDecomposable` and changes nothing (#546).

# scene3d

`game/scene3d` provides a 3D scene graph with dirty-propagated transforms, named sockets and typed attachments, a dynamic fat-AABB / SAH `BVH` for fast spatial queries, perspective/orthographic cameras with Orbit/FPS/ThirdPerson controllers, and screen-space 3D picking.

```go
import "github.com/nsgo-dev/game/packages/game/scene3d"
```

All transforms, projections, frustums, and rays follow [math3d](math3d.md) conventions (right-handed Y-up, column-major, `[0, 1]` NDC depth).

## Node hierarchy, sockets, and attachments

Build hierarchies with `NewScene()` and `NewNode(name)` so transforms, visibility, layers, and bounds initialize cleanly:

```go
sc := scene3d.NewScene()
hero := scene3d.NewNode("hero")
hero.SetPosition(math3d.Vec3{X: 0, Y: 0, Z: -4})
hero.SetLocalBounds(math3d.AABB{Min: math3d.Vec3{X: -0.5, Y: 0, Z: -0.5}, Max: math3d.Vec3{X: 0.5, Y: 1.8, Z: 0.5}})

// Define a named socket and attach a weapon node:
hero.DefineSocket("hand_r", math3d.Transform{
	Pos:   math3d.Vec3{X: 0.4, Y: 1.1, Z: -0.2},
	Rot:   math3d.QuatIdentity(),
	Scale: math3d.Vec3One(),
})
sword := scene3d.NewNode("sword")
_ = hero.Attach("hand_r", sword)

// Store typed gameplay metadata on any node:
hero.SetAttachment("hp", 100)
hp, ok := scene3d.AttachmentAs[int](hero, "hp")
_ = sc.Add(hero)
_, _ = hp, ok
```

Use `Reparent` / `ReparentKeepWorld`, `Detach`, `FindByName`, `FindByTag`, `Walk`, `WorldTransform()`, `WorldMatrix()`, `WorldBounds()`, and `HierarchyBounds()`. Calling `sc.UpdateTransforms()` propagates only dirty subtrees and keeps the scene's internal BVH in sync.

## Dynamic fat-AABB / SAH `BVH` and spatial queries

`Scene` maintains a dynamic fat-AABB Bounding Volume Hierarchy (`BVH`) for fast culling and spatial queries across large scenes (10k–50k+ nodes):

```go
sc.UpdateTransforms()

// Frustum culling (reusing caller slice with CullFrustumInto avoids allocations):
visible = sc.CullFrustumInto(cam.Frustum(), visible[:0])

// Spatial volume and ray queries:
nearby := sc.QuerySphere(math3d.Sphere{Center: heroPos, Radius: 12})
inBox := sc.QueryAABB(triggerBox)
hit, ok := sc.RaycastFirst(ray, scene3d.LayerAll)
_, _, _, _ = nearby, inBox, hit, ok
```

Call `sc.RebuildBVH()` after bulk level loads to run a top-down Surface Area Heuristic (SAH) rebuild. You can also use `scene3d.NewBVH()` standalone (`Insert`, `Update`, `Remove`, `Rebuild`, `QueryFrustum`, `QueryAABB`, `QuerySphere`, `Raycast`, `RaycastAll`).

## Cameras and controllers

Create perspective or orthographic cameras with `NewPerspectiveCamera` / `NewOrthographicCamera` (or sync a camera to a scene node via `cam.SyncFromNode(node)`), and drive them with one of the three built-in controllers:

| Controller | Constructor | Key methods |
| --- | --- | --- |
| `OrbitController` | `NewOrbitController(target, dist)` | `Orbit`, `Zoom`, `Pan`, `SetTarget`, `SetAngles`, `Snap`, `Update(cam, dt)` |
| `FPSController` | `NewFPSController(pos)` | `Look(dYaw, dPitch)`, `Move(fwd, right, up, dt)`, `Apply(cam)` (`Grounded` locks horizontal motion to XZ) |
| `ThirdPersonController` | `NewThirdPersonController()` | `Snap`, `Update(cam, targetPos, targetForward, dt)` with `ObstructionCheck` |

```go
cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 3, Z: 8}, math3d.DegToRad(60), 16.0/9.0, 0.1, 300)
tp := scene3d.NewThirdPersonController()
tp.ObstructionCheck = scene3d.SceneObstructionCheck(sc, scene3d.LayerDefault, hero)

// In game.Loop.Update:
tp.Update(cam, heroPos, heroForward, dt)
```

### Projection, viewport and interpolation

A camera's `Projection` is either perspective (`FOVY`, in radians) or orthographic (`OrthoHeight`, the visible height in world units), with `Aspect`, `Near` and `Far`; `ViewMatrix`, `ProjMatrix` and `ViewProj` give the matrices with [0, 1] clip depth, as WebGPU, Metal and Direct3D expect. Call `cam.SetViewport(width, height)` when the drawable resizes so `Aspect` follows it; sizes that are not positive and finite (zero, negative, NaN or infinite) leave the camera unchanged. `cam.Eye()` returns the homogeneous eye point: `(Pos, 1)` for a perspective camera and `(-Forward, 0)` for an orthographic one, whose eye lies at infinity, so a shader finds the direction toward the viewer as `normalize(eye.xyz - p * eye.w)` under both.

Step the camera in `game.Loop.Update` and draw it interpolated in `Draw`: keep the camera from the previous update and draw `prev.Lerp(cam, alpha)`. `Lerp` blends `Pos` linearly, `Rot` by shortest-path slerp and the projection's fields linearly between the values `Matrix` uses, so a field left at zero blends from its default rather than from zero; projections of different kinds do not blend, so the first holds until `t` reaches 1.

```go
prev := *cam
loop := &game.Loop{
	Update: func(dt time.Duration) {
		prev = *cam
		orbit.Update(cam, dt)
	},
	Draw: func(alpha float64) {
		drawn := prev.Lerp(cam, float32(alpha))
		camBuf.WriteCamera(&drawn) // render3d.CameraBuffer
		// record and submit draws
	},
}
```

## 3D picking

Attach a `Collider` (`*MeshCollider` or `*SphereCollider`) to pickable nodes (or fall back to `LocalBounds`) and pick directly from pointer coordinates:

```go
if hit, ok := scene3d.Pick(sc, cam, pointerX, pointerY, viewW, viewH, scene3d.LayerAll); ok {
	fmt.Printf("picked %s at %v (tri %d)\n", hit.Node.Name, hit.Point, hit.TriangleIndex)
}
```

Use `PickAll` when you need every hit sorted front-to-back along `cam.ScreenRay(screenX, screenY, viewW, viewH)`, and `cam.WorldToScreen` to project 3D points into 2D viewport coordinates.

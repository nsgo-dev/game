# scene3d

`game/scene3d` provides a 3D scene graph with dirty-propagated transforms, named sockets and typed attachments, a dynamic fat-AABB / SAH `BVH` for fast spatial queries, perspective/orthographic cameras with Orbit/FPS/ThirdPerson controllers, screen-space 3D picking, and box-projected 3D decals for the mobile forward path.

```go
import "github.com/nsgo-dev/game/pkg/scene3d"
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

Use `Reparent` / `ReparentKeepWorld`, `Detach`, `FindByName`, `FindByTag`, `Walk`, `WorldTransform()`, `WorldMatrix()`, `WorldBounds()`, and `HierarchyBounds()`. Calling `sc.UpdateTransforms()` propagates only dirty subtrees and queues the changed nodes for the scene's internal BVH; every query applies the queue first, and `sc.SyncBVH()` applies it on demand.

`ReparentKeepWorld` (and `SetParent(parent, true)`) solves the new local transform from the world matrices, so the node stays exactly where it is drawn. When that local transform would need shear, as for an unrotated child under a rotated, non-uniformly scaled parent, or the new parent's scale has a zero component, it returns `ErrNotDecomposable` and leaves the node where it was in the hierarchy.

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

Attach a `Collider` (`*MeshCollider` or `*SphereCollider`) to pickable nodes (or fall back to `LocalBounds`) and pick directly from pointer coordinates. A node's world bounds cover both its `LocalBounds` and its collider. `MeshCollider` caches its own bounds and notices when `Triangles` is replaced, but the node's world bounds and BVH entry refresh only when the node is marked dirty: after replacing `Triangles` call `node.MarkDirty()` (or assign the collider again with `node.SetCollider`), and after editing vertices in place call both `collider.MarkDirty()` and `node.MarkDirty()`. Visibility filtering is memoized per query, so hits deep in a hierarchy do not each walk every ancestor.

```go
if hit, ok := scene3d.Pick(sc, cam, pointerX, pointerY, viewW, viewH, scene3d.LayerAll); ok {
	fmt.Printf("picked %s at %v (tri %d)\n", hit.Node.Name, hit.Point, hit.TriangleIndex)
}
```

Use `PickAll` when you need every hit sorted front-to-back along `cam.ScreenRay(screenX, screenY, viewW, viewH)`, and `cam.WorldToScreen` to project 3D points into 2D viewport coordinates.

## Box-projected 3D decals

Place impact marks, scorches, blob shadows, and splats onto scene geometry with `Decal`, `DecalPool`, `ProjectDecal`, and `GPUDecalRenderer`. Decals project along their local `-Z` axis onto surfaces facing `+Z`, using BVH box overlap and 6-plane Sutherland–Hodgman polygon clipping to emit forward-path triangle geometry—no deferred G-buffer, compute shaders, or depth-buffer readback are needed, so decals run identically on WebGPU, Metal, Vulkan, OpenGL ES 3.0, and WebGL 2.

- **Normals and angle fade**: surfaces within `AngleFadeStart` receive full opacity; surfaces between `AngleFadeStart` and `AngleFadeEnd` fade smoothly by surface normal; perpendicular and back-facing surfaces are discarded so decals never bleed onto side or rear faces. `DepthFade` fades smoothly near the front and back faces of the projection box.
- **Layer masks**: `Decal.LayerMask` filters receiver nodes against `Node.Layer` (`LayerAll` matches everything, `0` matches nothing); hidden and disabled subtrees are skipped automatically. Any node with a `*MeshCollider` or an attached `DecalReceiver` (`node.SetDecalReceiver(&scene3d.DecalReceiverMesh{...})`) can receive decals.
- **Fixed-capacity pool**: `NewDecalPool(maxDecals)` caps the live decal count with deterministic eviction (lowest `SortOrder`, then oldest spawn order), ages and expires decals in `Step(dt)` with `FadeIn`/`FadeOut`, and reuses scratch buffers so steady-state `Spawn`, `Step`, and `BuildGeometry` allocate zero times per frame.

```go
pool := scene3d.NewDecalPool(64)

// Place a red impact mark from a raycast hit, excluding character layers:
mark := scene3d.NewDecalOnSurface(hit.Point, hit.Normal, math3d.Vec3Up(), math3d.Vec3{X: 0.35, Y: 0.35, Z: 0.25})
mark.Albedo = core.RGB(0xdd2222)
mark.LayerMask = scene3d.LayerDefault
mark.Lifetime = 10 * time.Second
mark.FadeOut = 2 * time.Second
pool.Spawn(mark)

// Each update & frame:
pool.Step(dt)
var geom scene3d.DecalGeometry
pool.BuildGeometry(sc, &geom)
_ = decalRenderer.Upload(&geom)
// Inside the forward render pass after opaque geometry:
decalRenderer.DrawWithCamera(pass, camBuf.BindGroup())
```

Create `GPUDecalRenderer` once in `gpu.View`'s `OnReady` with `NewGPUDecalRenderer(cx, dev, opts)`. Pass `render3d.CameraBuffer.Layout()` as `CameraLayout` to share the frame's camera bind group (`DrawWithCamera`), or leave `CameraLayout` nil to use the renderer's own camera buffer (`WriteCamera` + `Draw`). Bind custom textures per `Decal.TextureID` with `SetTexture` or `UploadTextureRGBA`; consecutive batches sharing a texture merge into a single `DrawIndexed` call.

## Scene lighting limits per target

When a 3D scene file is built and rendered through [`scenemodel.Renderer`](scenemodel.md#light-limits-per-target), its visible directional lights (up to 4) and first shadow-casting point and spot lights stay in the `render3d.LightRig` uniform block, while every other visible point and spot light is shaded through `render3d.ClusteredLightSystem`:

- **Storage-buffer targets** (macOS/iOS Metal, WebGPU, Vulkan, Direct3D 11, and headless software tests): up to **512** visible unshadowed local lights (point + spot) per frame (`64` per frustum cluster cell), plus `4` directional lights and `1` shadow-casting point + `1` shadow-casting spot light.
- **Fallback targets without storage buffers** (WebGL 2, OpenGL ES 3.0, Linux GtkGLArea, `nsgotest.NoGPUCompute`): up to **16** prioritized visible unshadowed local lights per frame (frustum-culled and ranked by luminous power, range, and camera distance), plus `4` directional lights and `1` shadow-casting point + `1` shadow-casting spot light.
- **Direct `render3d.LightRig` without clustering** (`Built.LightRig()` / `Built.LightRigNear`): up to `4` directional, `16` point, and `8` spot lights, keeping slot 0's shadow caster and picking the nearest lights to the camera when truncating.

Run `game check` to flag scenes whose unshadowed local light count exceeds the 16-light fallback cap (with file, line, and dropped count).

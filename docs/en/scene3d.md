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

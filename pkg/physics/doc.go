// Package physics provides 2D collision shapes, exact overlap contacts, collision layers,
// trigger areas, tile and kinematic character movement, spatial queries, and deterministic
// 2D rigid bodies and joints for games.
//
// Coordinates follow NSGo's screen space: X increases to the right, Y increases downward,
// and angles are measured in radians increasing clockwise.
//
// Shapes (Box, Circle, convex and optionally rounded Polygon, Capsule, Segment and static Chain, unified by Shape) test overlaps with TestBoxBox, TestCircleCircle, TestBoxCircle, TestCircleBox and TestShapes, returning a Contact whose unit normal points from B toward A by the penetration Depth needed to separate them. Polygons, capsules, segments and chains collide by the separating axis test on rounded hulls, as in Box2D, and chains suppress the ghost contacts their interior vertices would otherwise cause.
//
// Collision filtering uses 32-bit LayerMask values (Layer and Mask) on Body, Trigger and
// RigidBody. Zero values default to LayerAll so unfiltered code stays backwards-compatible.
//
// MoveAndSlide and MoveAndSlideInto advance a Body over a fixed game.Loop step against any TileGrid (or the
// concrete Grid), resolving solid walls, floors, ceilings and one-way platforms without
// tunneling at high speeds; MoveAndSlideInto reuses a caller-supplied MoveResult so steady-state
// steps allocate nothing. World.MoveAndSlide, World.MoveAndSlideWith, and their Into variants sweep the body's
// full shape against static and kinematic bodies (including OneWay bodies), solid and one-way
// tiles, and static RigidBody colliders, sliding along contact normals for a bounded number
// of iterations with configurable up direction and floor angle. CharacterBody builds on this
// for platformer characters with walkable and steep Slope segments, downhill snapping, and
// MovingPlatform riding, reusing scratch buffers across steps (with MoveInto and MoveWorldInto
// for caller-owned result storage).
//
// World groups bodies, Trigger areas, an optional TileGrid and an optional RigidWorld for
// deterministic overlap, point, raycast, layer-filtered queries, MoveAndSlide, and
// enter/stay/exit trigger events via StepTriggers.
//
// RigidWorld and RigidBody implement lightweight, pure-Go deterministic 2D rigid-body
// dynamics and constraints (DistanceJoint with rope/range mode, SpringJoint, RevoluteJoint
// with limits and motor, PrismaticJoint, WeldJoint, WheelJoint and MouseJoint, all with
// reaction force/torque readouts and optional break thresholds) directly inside
// this package rather than binding a C/C++ library such as Box2D or Chipmunk. Keeping the
// solver pure Go eliminates cgo and external dependencies, guarantees uniform builds across
// macOS, iOS, Windows, Linux, Android and Web/WASM, shares Shape and LayerMask directly with
// kinematic physics, and produces deterministic results under game.Loop fixed steps.
//
// RigidBody.Kinematic bodies move by their velocity or RigidBody.SetTargetPose and push and carry dynamic bodies without being moved by them, RigidBody.Sensor bodies report SensorEvents instead of colliding, and RigidWorld.Tiles collides dynamic bodies with a TileGrid's solid tiles and, from above, its one-way tiles. After each Step, RigidWorld.ContactEvents reports the contacts that began, with point, normal and approach speed, and ended, in deterministic order; RigidWorld.Raycast, RaycastFirst and QueryOverlap query the bodies at their true poses and the tiles, and QueryPoint the bodies only, selected by QueryMask, with LayerMask filtering the bodies.
//
// RigidWorld finds contact pairs once per step with a sort-and-sweep broadphase, integrates and solves contacts and joints in substeps (RigidWorld.Substeps, defaulting to DefaultSubsteps) with per-substep warm starting, soft contact push-out at contact points, velocity relaxation and coupled pinched-pair solving for high mass ratios, puts islands of resting bodies to sleep (RigidBody.Sleeping, RigidBody.Wake), and sweeps fast bodies against static bodies and tiles (and every RigidBody.Bullet against dynamic and kinematic bodies too) so they stop at the first surface instead of passing through thin walls.
//
// DrawShape, DrawBody, DrawContact, DrawTileContacts, DrawTileGrid, DrawWorld and DrawRaycast record zoom-compensated debug overlays into a core.Drawing; builds tagged release compile them to no-ops.
package physics

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

// MoveAndSlide advances a Body over a fixed game.Loop step against any TileGrid (or the
// concrete Grid), resolving solid walls, floors, ceilings and one-way platforms without
// tunneling at high speeds. World.MoveAndSlide and World.MoveAndSlideWith sweep the body's
// full shape against static and kinematic bodies (including OneWay bodies), solid and one-way
// tiles, and static RigidBody colliders, sliding along contact normals for a bounded number
// of iterations with configurable up direction and floor angle. CharacterBody builds on this
// for platformer characters with walkable and steep Slope segments, downhill snapping, and
// MovingPlatform riding.
//
// World groups bodies, Trigger areas, an optional TileGrid and an optional RigidWorld for
// deterministic overlap, point, raycast, layer-filtered queries, MoveAndSlide, and
// enter/stay/exit trigger events via StepTriggers.
//
// RigidWorld and RigidBody implement lightweight, pure-Go deterministic 2D rigid-body
// dynamics and constraints (DistanceJoint, SpringJoint and RevoluteJoint) directly inside
// this package rather than binding a C/C++ library such as Box2D or Chipmunk. Keeping the
// solver pure Go eliminates cgo and external dependencies, guarantees uniform builds across
// macOS, iOS, Windows, Linux, Android and Web/WASM, shares Shape and LayerMask directly with
// kinematic physics, and produces deterministic results under game.Loop fixed steps.
//
// DrawShape, DrawBody, DrawContact, DrawTileContacts, DrawTileGrid, DrawWorld and DrawRaycast record zoom-compensated debug overlays into a core.Drawing; builds tagged release compile them to no-ops.

package physics

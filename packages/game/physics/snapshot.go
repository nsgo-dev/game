package physics

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

// SnapshotVersion is the schema version stored in WorldSnapshot and RigidWorldSnapshot. Version 2 added the kinematic and sensor flags, kinematic targets, Tiles and their settings, the contact and sensor overlap state, tile entries in the contact cache and a grid's origin; Restore still reads version 1 snapshots, which have none of them, and refuses any other version, so a build that only knows version 1 refuses a version 2 snapshot instead of dropping what it cannot read.
const SnapshotVersion = 2

// oldestSnapshotVersion is the oldest snapshot version Restore reads.
const oldestSnapshotVersion = 1

// BodySnapshot is a JSON-serializable snapshot of one physics.Body.
type BodySnapshot struct {
	ID          int       `json:"id"`
	Tag         string    `json:"tag,omitempty"`
	Shape       Shape     `json:"shape"`
	VX          float32   `json:"vx"`
	VY          float32   `json:"vy"`
	Layer       LayerMask `json:"layer"`
	Mask        LayerMask `json:"mask"`
	Grounded    bool      `json:"grounded,omitempty"`
	OnCeiling   bool      `json:"onCeiling,omitempty"`
	OnLeftWall  bool      `json:"onLeftWall,omitempty"`
	OnRightWall bool      `json:"onRightWall,omitempty"`
	OneWay      bool      `json:"oneWay,omitempty"`
	OneWayDrop  bool      `json:"oneWayDrop,omitempty"`
}

// TriggerSnapshot is a JSON-serializable snapshot of one physics.Trigger (excluding callbacks and UserData).
type TriggerSnapshot struct {
	ID    int          `json:"id"`
	Tag   string       `json:"tag,omitempty"`
	Shape Shape        `json:"shape"`
	Layer LayerMask    `json:"layer"`
	Mask  LayerMask    `json:"mask"`
	Area  AreaSettings `json:"area"`
}

// WorldTriggerActiveSnapshot records the active overlapping bodies tracked for one trigger in World.
// Non-negative BodyIndices index into WorldSnapshot.Bodies; negative values (-1 - k) index Detached[k].
type WorldTriggerActiveSnapshot struct {
	TriggerIndex int            `json:"triggerIndex"`
	BodyIndices  []int          `json:"bodyIndices,omitempty"`
	Detached     []BodySnapshot `json:"detached,omitempty"`
}

// GridSnapshot is a JSON-serializable snapshot of a *Grid tile grid.
type GridSnapshot struct {
	Cols       int         `json:"cols"`
	Rows       int         `json:"rows"`
	TileWidth  float32     `json:"tileWidth"`
	TileHeight float32     `json:"tileHeight"`
	OriginX    float32     `json:"originX,omitempty"`
	OriginY    float32     `json:"originY,omitempty"`
	Cells      []TileFlags `json:"cells,omitempty"`
}

// snapshotGrid returns a snapshot of tiles when it is a *Grid, or nil.
func snapshotGrid(tiles TileGrid) *GridSnapshot {
	g, ok := tiles.(*Grid)
	if !ok || g == nil {
		return nil
	}
	cells := make([]TileFlags, len(g.Cells))
	copy(cells, g.Cells)
	return &GridSnapshot{
		Cols:       g.Cols,
		Rows:       g.Rows,
		TileWidth:  g.TileWidth,
		TileHeight: g.TileHeight,
		OriginX:    g.OriginX,
		OriginY:    g.OriginY,
		Cells:      cells,
	}
}

// restoreGrid writes gs into tiles when it is a *Grid, keeping the caller's pointer, or returns a new *Grid holding gs.
func restoreGrid(tiles TileGrid, gs *GridSnapshot) TileGrid {
	cells := make([]TileFlags, len(gs.Cells))
	copy(cells, gs.Cells)
	g, ok := tiles.(*Grid)
	if !ok || g == nil {
		g = new(Grid)
	}
	*g = Grid{
		Cols:       gs.Cols,
		Rows:       gs.Rows,
		TileWidth:  gs.TileWidth,
		TileHeight: gs.TileHeight,
		OriginX:    gs.OriginX,
		OriginY:    gs.OriginY,
		Cells:      cells,
	}
	return g
}

// WorldSnapshot captures the complete deterministic state of a physics.World,
// including all bodies, triggers, private trigger enter/stay/exit tracking,
// gravity, and any *Grid tilemap. It marshals cleanly with encoding/json.
type WorldSnapshot struct {
	Version       int                          `json:"version"`
	GravityX      float32                      `json:"gravityX"`
	GravityY      float32                      `json:"gravityY"`
	Bodies        []BodySnapshot               `json:"bodies,omitempty"`
	Triggers      []TriggerSnapshot            `json:"triggers,omitempty"`
	TriggerActive []WorldTriggerActiveSnapshot `json:"triggerActive,omitempty"`
	Grid          *GridSnapshot                `json:"grid,omitempty"`
}

// RigidBodySnapshot captures the complete public and private simulation state of one RigidBody.
type RigidBodySnapshot struct {
	ID              int       `json:"id"`
	Tag             string    `json:"tag,omitempty"`
	Shape           Shape     `json:"shape"`
	X               float32   `json:"x"`
	Y               float32   `json:"y"`
	VX              float32   `json:"vx"`
	VY              float32   `json:"vy"`
	Angle           float32   `json:"angle"`
	AngularVel      float32   `json:"angularVel"`
	Mass            float32   `json:"mass"`
	InvMass         float32   `json:"invMass"`
	Inertia         float32   `json:"inertia"`
	InvInertia      float32   `json:"invInertia"`
	Restitution     float32   `json:"restitution"`
	Friction        float32   `json:"friction"`
	LinearDamping   float32   `json:"linearDamping"`
	AngularDamping  float32   `json:"angularDamping"`
	GravityScale    float32   `json:"gravityScale"`
	GravityScaleSet bool      `json:"gravityScaleSet,omitempty"`
	Static          bool      `json:"static,omitempty"`
	Layer           LayerMask `json:"layer"`
	Mask            LayerMask `json:"mask"`
	ForceX          float64   `json:"forceX,omitempty"`
	ForceY          float64   `json:"forceY,omitempty"`
	Torque          float64   `json:"torque,omitempty"`
	// Local is the body's Polygon, Capsule, Segment or Chain shape relative to (X, Y) at Angle 0, which the solver rotates each step; nil for a Box or Circle.
	Local *Shape `json:"local,omitempty"`
	// Synced reports that Shape is the world shape last derived from Local at pose (SyncX, SyncY, SyncAngle), so it is not re-derived on the next step.
	Synced    bool    `json:"synced,omitempty"`
	SyncX     float32 `json:"syncX,omitempty"`
	SyncY     float32 `json:"syncY,omitempty"`
	SyncAngle float32 `json:"syncAngle,omitempty"`
	// DerivedInertia is the Inertia the body last derived from its shape.
	DerivedInertia float32 `json:"derivedInertia,omitempty"`
	Bullet         bool    `json:"bullet,omitempty"`
	// Sleeping reports that the body rests in a sleeping island; SleepEnv is the gravity, damping and fluid it fell asleep in.
	Sleeping bool        `json:"sleeping,omitempty"`
	SleepEnv *AreaEffect `json:"sleepEnv,omitempty"`
	// SleepTime is how long, in seconds, the body has moved slower than the sleep thresholds.
	SleepTime float64 `json:"sleepTime,omitempty"`
	// RestValid reports that (RestX, RestY, RestAngle) is the pose the body ended its last step with, which tells the next step whether it was moved by hand.
	RestValid bool    `json:"restValid,omitempty"`
	RestX     float32 `json:"restX,omitempty"`
	RestY     float32 `json:"restY,omitempty"`
	RestAngle float32 `json:"restAngle,omitempty"`
	Kinematic bool    `json:"kinematic,omitempty"`
	Sensor    bool    `json:"sensor,omitempty"`
	// TargetPending reports a pose (TargetX, TargetY, TargetAngle) that SetTargetPose asked the next Step to reach.
	TargetPending bool    `json:"targetPending,omitempty"`
	TargetX       float32 `json:"targetX,omitempty"`
	TargetY       float32 `json:"targetY,omitempty"`
	TargetAngle   float32 `json:"targetAngle,omitempty"`
	// TargetDriven reports that the last Step moved the body to a target with velocity (TargetVX, TargetVY, TargetAngularVel), which the next Step stops unless a new target is set.
	TargetDriven     bool    `json:"targetDriven,omitempty"`
	TargetVX         float32 `json:"targetVX,omitempty"`
	TargetVY         float32 `json:"targetVY,omitempty"`
	TargetAngularVel float32 `json:"targetAngularVel,omitempty"`
}

// RigidContactPointSnapshot captures the impulses one contact point ended a step with, which warm-start the next step.
type RigidContactPointSnapshot struct {
	// Feature names the pair of shape features in contact.
	Feature        uint32  `json:"feature"`
	NormalImpulse  float64 `json:"normalImpulse"`
	TangentImpulse float64 `json:"tangentImpulse"`
}

// RigidContactSnapshot captures one cached contact manifold of a RigidWorld: the pair of bodies, as indices into RigidWorldSnapshot.Bodies, the manifold's index within the pair (the chain segment against a Chain, 0 otherwise) and its points. A contact with the world's Tiles has Tile set, BodyBIndex -1 and the tile's row*cols + col in Sub.
type RigidContactSnapshot struct {
	BodyAIndex int                         `json:"bodyAIndex"`
	BodyBIndex int                         `json:"bodyBIndex"`
	Tile       bool                        `json:"tile,omitempty"`
	Sub        int                         `json:"sub,omitempty"`
	Points     []RigidContactPointSnapshot `json:"points,omitempty"`
}

// RigidTouchSnapshot captures one contact a RigidWorld reported begun and not yet ended: two bodies, as indices into RigidWorldSnapshot.Bodies, or a body and the Tiles, with Tile set, BodyBIndex -1 and the last tile touched.
type RigidTouchSnapshot struct {
	BodyAIndex int       `json:"bodyAIndex"`
	BodyBIndex int       `json:"bodyBIndex"`
	Tile       bool      `json:"tile,omitempty"`
	Col        int       `json:"col,omitempty"`
	Row        int       `json:"row,omitempty"`
	TileFlags  TileFlags `json:"tileFlags,omitempty"`
}

// RigidSenseSnapshot captures one body overlapping a sensor, both as indices into RigidWorldSnapshot.Bodies.
type RigidSenseSnapshot struct {
	SensorIndex  int `json:"sensorIndex"`
	VisitorIndex int `json:"visitorIndex"`
}

// RigidJointSnapshot captures the complete public and private solver state of one RigidWorld Joint.
type RigidJointSnapshot struct {
	Kind             string             `json:"kind"` // "distance", "spring", "revolute", "prismatic", "weld", "wheel", "mouse"
	BodyAIndex       int                `json:"bodyAIndex"`
	BodyBIndex       int                `json:"bodyBIndex"`
	DetachedA        *RigidBodySnapshot `json:"detachedA,omitempty"`
	DetachedB        *RigidBodySnapshot `json:"detachedB,omitempty"`
	AnchorAX         float32            `json:"anchorAX"`
	AnchorAY         float32            `json:"anchorAY"`
	AnchorBX         float32            `json:"anchorBX"`
	AnchorBY         float32            `json:"anchorBY"`
	LocalAxisAX      float32            `json:"localAxisAX,omitempty"`
	LocalAxisAY      float32            `json:"localAxisAY,omitempty"`
	TargetX          float32            `json:"targetX,omitempty"`
	TargetY          float32            `json:"targetY,omitempty"`
	Length           float32            `json:"length,omitempty"`
	MinLength        float32            `json:"minLength,omitempty"`
	MaxLength        float32            `json:"maxLength,omitempty"`
	MaxOnly          bool               `json:"maxOnly,omitempty"`
	RestLength       float32            `json:"restLength,omitempty"`
	ReferenceAngle   float32            `json:"referenceAngle,omitempty"`
	EnableLimit      bool               `json:"enableLimit,omitempty"`
	LowerAngle       float32            `json:"lowerAngle,omitempty"`
	UpperAngle       float32            `json:"upperAngle,omitempty"`
	LowerTranslation float32            `json:"lowerTranslation,omitempty"`
	UpperTranslation float32            `json:"upperTranslation,omitempty"`
	EnableMotor      bool               `json:"enableMotor,omitempty"`
	MotorSpeed       float32            `json:"motorSpeed,omitempty"`
	MaxMotorTorque   float32            `json:"maxMotorTorque,omitempty"`
	MaxMotorForce    float32            `json:"maxMotorForce,omitempty"`
	MaxForce         float32            `json:"maxForce,omitempty"`
	FrequencyHz      float32            `json:"frequencyHz,omitempty"`
	DampingRatio     float32            `json:"dampingRatio,omitempty"`
	Stiffness        float32            `json:"stiffness,omitempty"`
	Compliance       float32            `json:"compliance,omitempty"`
	Damping          float32            `json:"damping,omitempty"`
	BreakForce       float32            `json:"breakForce,omitempty"`
	BreakTorque      float32            `json:"breakTorque,omitempty"`
	CollideConnected bool               `json:"collideConnected,omitempty"`
	StepImpulseX     float64            `json:"stepImpulseX,omitempty"`
	StepImpulseY     float64            `json:"stepImpulseY,omitempty"`
	StepAngImpulse   float64            `json:"stepAngImpulse,omitempty"`
	LastDt           float64            `json:"lastDt,omitempty"`
	Broken           bool               `json:"broken,omitempty"`
	Initialized      bool               `json:"initialized,omitempty"`
	Active           bool               `json:"active,omitempty"`
	Soft             bool               `json:"soft,omitempty"`
	RotLocked        bool               `json:"rotLocked,omitempty"`
	SpringActive     bool               `json:"springActive,omitempty"`
	Dt               float64            `json:"dt,omitempty"`
	UX               float64            `json:"ux,omitempty"`
	UY               float64            `json:"uy,omitempty"`
	AX               float64            `json:"ax,omitempty"`
	AY               float64            `json:"ay,omitempty"`
	PX               float64            `json:"px,omitempty"`
	PY               float64            `json:"py,omitempty"`
	RAX              float64            `json:"rAx,omitempty"`
	RAY              float64            `json:"rAy,omitempty"`
	RBX              float64            `json:"rBx,omitempty"`
	RBY              float64            `json:"rBy,omitempty"`
	S1               float64            `json:"s1,omitempty"`
	S2               float64            `json:"s2,omitempty"`
	A1               float64            `json:"a1,omitempty"`
	A2               float64            `json:"a2,omitempty"`
	EffMass          float64            `json:"effMass,omitempty"`
	PerpMass         float64            `json:"perpMass,omitempty"`
	AxisMass         float64            `json:"axisMass,omitempty"`
	SpringMass       float64            `json:"springMass,omitempty"`
	MotorMass        float64            `json:"motorMass,omitempty"`
	M11              float64            `json:"m11,omitempty"`
	M12              float64            `json:"m12,omitempty"`
	M22              float64            `json:"m22,omitempty"`
	KA               float64            `json:"kA,omitempty"`
	KB               float64            `json:"kB,omitempty"`
	KC               float64            `json:"kC,omitempty"`
	KD               float64            `json:"kD,omitempty"`
	KE               float64            `json:"kE,omitempty"`
	KF               float64            `json:"kF,omitempty"`
	Gamma            float64            `json:"gamma,omitempty"`
	GammaAng         float64            `json:"gammaAng,omitempty"`
	Bias             float64            `json:"bias,omitempty"`
	BiasX            float64            `json:"biasX,omitempty"`
	BiasY            float64            `json:"biasY,omitempty"`
	BiasAng          float64            `json:"biasAng,omitempty"`
	AccImpulse       float64            `json:"accImpulse,omitempty"`
	ImpulseX         float64            `json:"impulseX,omitempty"`
	ImpulseY         float64            `json:"impulseY,omitempty"`
	AngImpulse       float64            `json:"angImpulse,omitempty"`
	SpringImpulse    float64            `json:"springImpulse,omitempty"`
	MotorImpulse     float64            `json:"motorImpulse,omitempty"`
	LimitImpulse     float64            `json:"limitImpulse,omitempty"`
	Mode             int                `json:"mode,omitempty"`
	LimitState       int                `json:"limitState,omitempty"`
	TargetLen        float64            `json:"targetLen,omitempty"`
}

// RigidJointBreakEventSnapshot captures one JointBreakEvent from the most recent RigidWorld.Step.
type RigidJointBreakEventSnapshot struct {
	Joint      *RigidJointSnapshot `json:"joint,omitempty"`
	BodyAIndex int                 `json:"bodyAIndex"`
	BodyBIndex int                 `json:"bodyBIndex"`
	DetachedA  *RigidBodySnapshot  `json:"detachedA,omitempty"`
	DetachedB  *RigidBodySnapshot  `json:"detachedB,omitempty"`
	ForceX     float32             `json:"forceX"`
	ForceY     float32             `json:"forceY"`
	Torque     float32             `json:"torque"`
}

// RigidWorldSnapshot captures the complete deterministic state of a RigidWorld, including all rigid bodies (and pending forces/torques, sleep state and timers), joints and their private constraint state, joint-break events, the warm-start contact cache, area triggers, gravity, and solver and sleep configuration. It marshals cleanly with encoding/json.
type RigidWorldSnapshot struct {
	Version               int                            `json:"version"`
	GravityX              float32                        `json:"gravityX"`
	GravityY              float32                        `json:"gravityY"`
	SolverIterations      int                            `json:"solverIterations"`
	DisableSleep          bool                           `json:"disableSleep,omitempty"`
	SleepLinearThreshold  float32                        `json:"sleepLinearThreshold,omitempty"`
	SleepAngularThreshold float32                        `json:"sleepAngularThreshold,omitempty"`
	TimeToSleep           float32                        `json:"timeToSleep,omitempty"`
	Bodies                []RigidBodySnapshot            `json:"bodies,omitempty"`
	Joints                []RigidJointSnapshot           `json:"joints,omitempty"`
	JointBreakEvents      []RigidJointBreakEventSnapshot `json:"jointBreakEvents,omitempty"`
	Triggers              []TriggerSnapshot              `json:"triggers,omitempty"`
	// Contacts is the warm-start cache between bodies of Bodies, and LastDt the length in seconds of the step that filled it.
	Contacts []RigidContactSnapshot `json:"contacts,omitempty"`
	LastDt   float64                `json:"lastDt,omitempty"`
	// Grid is the world's Tiles when they are a *Grid, with the settings they collide with. CustomTiles reports Tiles of another TileGrid type, which Save cannot capture: Restore keeps the caller's Tiles then, and clears them when neither is set, as for a world without Tiles or a version 1 snapshot.
	Grid            *GridSnapshot `json:"grid,omitempty"`
	CustomTiles     bool          `json:"customTiles,omitempty"`
	TileLayer       LayerMask     `json:"tileLayer,omitempty"`
	TileMask        LayerMask     `json:"tileMask,omitempty"`
	TileFriction    float32       `json:"tileFriction,omitempty"`
	TileRestitution float32       `json:"tileRestitution,omitempty"`
	// Touching and Sensing are the contacts and sensor overlaps the next Step compares its own with to report ContactEvents and SensorEvents. Entries with a body outside Bodies are not saved, so the end of such a contact is not reported after Restore.
	Touching []RigidTouchSnapshot `json:"touching,omitempty"`
	Sensing  []RigidSenseSnapshot `json:"sensing,omitempty"`
}

func snapshotBody(b *Body) BodySnapshot {
	return BodySnapshot{
		ID:          b.ID,
		Tag:         b.Tag,
		Shape:       cloneShape(b.Shape),
		VX:          b.VX,
		VY:          b.VY,
		Layer:       b.Layer,
		Mask:        b.Mask,
		Grounded:    b.Grounded,
		OnCeiling:   b.OnCeiling,
		OnLeftWall:  b.OnLeftWall,
		OnRightWall: b.OnRightWall,
		OneWay:      b.OneWay,
		OneWayDrop:  b.OneWayDrop,
	}
}

func applyBodySnapshot(dst *Body, src BodySnapshot) {
	ud := dst.UserData
	*dst = Body{
		ID:          src.ID,
		Tag:         src.Tag,
		Shape:       cloneShape(src.Shape),
		VX:          src.VX,
		VY:          src.VY,
		Layer:       src.Layer,
		Mask:        src.Mask,
		Grounded:    src.Grounded,
		OnCeiling:   src.OnCeiling,
		OnLeftWall:  src.OnLeftWall,
		OnRightWall: src.OnRightWall,
		OneWay:      src.OneWay,
		OneWayDrop:  src.OneWayDrop,
		UserData:    ud,
	}
}

func snapshotTrigger(tr *Trigger) TriggerSnapshot {
	return TriggerSnapshot{
		ID:    tr.ID,
		Tag:   tr.Tag,
		Shape: cloneShape(tr.Shape),
		Layer: tr.Layer,
		Mask:  tr.Mask,
		Area:  tr.Area,
	}
}

func applyTriggerSnapshot(dst *Trigger, src TriggerSnapshot) {
	onEnter, onStay, onExit, ud := dst.OnEnter, dst.OnStay, dst.OnExit, dst.UserData
	*dst = Trigger{
		ID:       src.ID,
		Tag:      src.Tag,
		Shape:    cloneShape(src.Shape),
		Layer:    src.Layer,
		Mask:     src.Mask,
		OnEnter:  onEnter,
		OnStay:   onStay,
		OnExit:   onExit,
		Area:     src.Area,
		UserData: ud,
	}
}

// Save captures a deep, deterministic, JSON-serializable snapshot of w.
func (w *World) Save() WorldSnapshot {
	if w == nil {
		return WorldSnapshot{Version: SnapshotVersion}
	}
	snap := WorldSnapshot{
		Version:  SnapshotVersion,
		GravityX: w.GravityX,
		GravityY: w.GravityY,
	}

	bodyIdx := make(map[*Body]int, len(w.Bodies))
	for _, b := range w.Bodies {
		if b == nil {
			continue
		}
		idx := len(snap.Bodies)
		bodyIdx[b] = idx
		snap.Bodies = append(snap.Bodies, snapshotBody(b))
	}

	trigPtrs := make([]*Trigger, 0, len(w.Triggers))
	for _, tr := range w.Triggers {
		if tr == nil {
			continue
		}
		trigPtrs = append(trigPtrs, tr)
		snap.Triggers = append(snap.Triggers, snapshotTrigger(tr))
	}

	if len(w.triggerActive) > 0 {
		for ti, tr := range trigPtrs {
			prev := w.triggerActive[tr]
			if len(prev) == 0 {
				continue
			}
			entry := WorldTriggerActiveSnapshot{
				TriggerIndex: ti,
				BodyIndices:  make([]int, 0, len(prev)),
			}
			for _, b := range prev {
				if b == nil {
					continue
				}
				if bi, ok := bodyIdx[b]; ok {
					entry.BodyIndices = append(entry.BodyIndices, bi)
				} else {
					detIdx := len(entry.Detached)
					entry.Detached = append(entry.Detached, snapshotBody(b))
					entry.BodyIndices = append(entry.BodyIndices, -1-detIdx)
				}
			}
			if len(entry.BodyIndices) > 0 {
				snap.TriggerActive = append(snap.TriggerActive, entry)
			}
		}
	}

	snap.Grid = snapshotGrid(w.Tiles)
	return snap
}

// Restore replaces w's state from snap. Existing *Body and *Trigger pointers
// are reused in place (matching by non-zero ID first, then by slot index) so
// caller references, UserData, and Trigger callbacks remain intact.
func (w *World) Restore(snap WorldSnapshot) error {
	if w == nil {
		return errors.New("physics: cannot restore nil World")
	}
	if snap.Version < oldestSnapshotVersion || snap.Version > SnapshotVersion {
		return fmt.Errorf("physics: unsupported WorldSnapshot version %d (want %d to %d)", snap.Version, oldestSnapshotVersion, SnapshotVersion)
	}
	for i, ta := range snap.TriggerActive {
		if ta.TriggerIndex < 0 || ta.TriggerIndex >= len(snap.Triggers) {
			return fmt.Errorf("physics: triggerActive[%d] has out-of-range trigger index %d", i, ta.TriggerIndex)
		}
		for _, bi := range ta.BodyIndices {
			if bi >= len(snap.Bodies) || (bi < 0 && (-1-bi) >= len(ta.Detached)) {
				return fmt.Errorf("physics: triggerActive[%d] has out-of-range body index %d", i, bi)
			}
		}
	}

	// Build pools of existing *Body pointers for in-place reuse.
	existingBodiesByID := make(map[int]*Body, len(w.Bodies))
	existingBodiesUsed := make(map[*Body]bool, len(w.Bodies))
	for _, b := range w.Bodies {
		if b != nil && b.ID != 0 {
			if _, dup := existingBodiesByID[b.ID]; !dup {
				existingBodiesByID[b.ID] = b
			}
		}
	}

	restoredBodies := make([]*Body, len(snap.Bodies))
	for i, bs := range snap.Bodies {
		var dst *Body
		if bs.ID != 0 {
			if cand, ok := existingBodiesByID[bs.ID]; ok && !existingBodiesUsed[cand] {
				dst = cand
			}
		}
		if dst == nil && i < len(w.Bodies) && w.Bodies[i] != nil && !existingBodiesUsed[w.Bodies[i]] {
			dst = w.Bodies[i]
		}
		if dst == nil {
			dst = new(Body)
		}
		existingBodiesUsed[dst] = true
		applyBodySnapshot(dst, bs)
		restoredBodies[i] = dst
	}

	// Build pools of existing *Trigger pointers so callbacks stay attached.
	existingTriggersByID := make(map[int]*Trigger, len(w.Triggers))
	existingTriggersUsed := make(map[*Trigger]bool, len(w.Triggers))
	for _, tr := range w.Triggers {
		if tr != nil && tr.ID != 0 {
			if _, dup := existingTriggersByID[tr.ID]; !dup {
				existingTriggersByID[tr.ID] = tr
			}
		}
	}

	restoredTriggers := make([]*Trigger, len(snap.Triggers))
	for i, ts := range snap.Triggers {
		var dst *Trigger
		if ts.ID != 0 {
			if cand, ok := existingTriggersByID[ts.ID]; ok && !existingTriggersUsed[cand] {
				dst = cand
			}
		}
		if dst == nil && i < len(w.Triggers) && w.Triggers[i] != nil && !existingTriggersUsed[w.Triggers[i]] {
			dst = w.Triggers[i]
		}
		if dst == nil {
			dst = new(Trigger)
		}
		existingTriggersUsed[dst] = true
		applyTriggerSnapshot(dst, ts)
		restoredTriggers[i] = dst
	}

	var restoredActive map[*Trigger][]*Body
	if len(snap.TriggerActive) > 0 {
		restoredActive = make(map[*Trigger][]*Body, len(snap.TriggerActive))
		for _, ta := range snap.TriggerActive {
			tr := restoredTriggers[ta.TriggerIndex]
			list := make([]*Body, len(ta.BodyIndices))
			for j, bi := range ta.BodyIndices {
				if bi >= 0 {
					list[j] = restoredBodies[bi]
				} else {
					b := new(Body)
					applyBodySnapshot(b, ta.Detached[-1-bi])
					list[j] = b
				}
			}
			restoredActive[tr] = list
		}
	}

	w.GravityX = snap.GravityX
	w.GravityY = snap.GravityY
	w.Bodies = restoredBodies
	w.Triggers = restoredTriggers
	w.triggerActive = restoredActive

	if snap.Grid != nil {
		w.Tiles = restoreGrid(w.Tiles, snap.Grid)
	}
	return nil
}

// Checksum returns a deterministic 64-bit FNV-1a hash of w's complete state,
// including private trigger overlap tracking.
func (w *World) Checksum() uint64 {
	h := detmath.NewHasher()
	h.WriteString("physics.World.v1")
	if w == nil {
		return h.Sum64()
	}
	h.WriteFloat32(w.GravityX)
	h.WriteFloat32(w.GravityY)

	bodyIdx := make(map[*Body]int, len(w.Bodies))
	bodyCount := 0
	for _, b := range w.Bodies {
		if b != nil {
			bodyIdx[b] = bodyCount
			bodyCount++
		}
	}
	h.WriteInt(bodyCount)
	for _, b := range w.Bodies {
		if b != nil {
			hashBody(&h, b)
		}
	}

	trigCount := 0
	for _, tr := range w.Triggers {
		if tr != nil {
			trigCount++
		}
	}
	h.WriteInt(trigCount)
	for _, tr := range w.Triggers {
		if tr == nil {
			continue
		}
		hashTrigger(&h, tr)
		prev := w.triggerActive[tr]
		h.WriteInt(len(prev))
		for _, b := range prev {
			if b == nil {
				h.WriteInt(-2)
				continue
			}
			if bi, ok := bodyIdx[b]; ok {
				h.WriteInt(bi)
			} else {
				h.WriteInt(-1)
				hashBody(&h, b)
			}
		}
	}

	if w.Tiles != nil {
		h.WriteBool(true)
		tw, th := w.Tiles.TileSize()
		cols, rows := w.Tiles.GridSize()
		h.WriteFloat32(tw)
		h.WriteFloat32(th)
		h.WriteInt(cols)
		h.WriteInt(rows)
		for row := 0; row < rows; row++ {
			for col := 0; col < cols; col++ {
				h.WriteUint32(uint32(w.Tiles.TileFlagsAt(col, row)))
			}
		}
	} else {
		h.WriteBool(false)
	}
	return h.Sum64()
}

func snapshotRigidBody(b *RigidBody) RigidBodySnapshot {
	s := RigidBodySnapshot{
		ID:              b.ID,
		Tag:             b.Tag,
		Shape:           cloneShape(b.Shape),
		X:               b.X,
		Y:               b.Y,
		VX:              b.VX,
		VY:              b.VY,
		Angle:           b.Angle,
		AngularVel:      b.AngularVel,
		Mass:            b.Mass,
		InvMass:         b.InvMass,
		Inertia:         b.Inertia,
		InvInertia:      b.InvInertia,
		Restitution:     b.Restitution,
		Friction:        b.Friction,
		LinearDamping:   b.LinearDamping,
		AngularDamping:  b.AngularDamping,
		GravityScale:    b.GravityScale,
		GravityScaleSet: b.gravityScaleSet,
		Static:          b.Static,
		Layer:           b.Layer,
		Mask:            b.Mask,
		ForceX:          b.forceX,
		ForceY:          b.forceY,
		Torque:          b.torque,
		DerivedInertia:  b.derivedInertia,
		Bullet:          b.Bullet,
		Sleeping:        b.sleeping,
		SleepTime:       b.sleepTime,
		RestValid:       b.restValid,
		RestX:           b.restX,
		RestY:           b.restY,
		RestAngle:       b.restAngle,
		Kinematic:       b.Kinematic,
		Sensor:          b.Sensor,
	}
	if b.targetPending {
		s.TargetPending = true
		s.TargetX, s.TargetY, s.TargetAngle = b.target.x, b.target.y, b.target.angle
	}
	if b.targetDriven {
		s.TargetDriven = true
		s.TargetVX, s.TargetVY, s.TargetAngularVel = b.targetVX, b.targetVY, b.targetAV
	}
	if b.sleeping {
		env := b.sleepEnv
		s.SleepEnv = &env
	}
	if b.hasLocal {
		local := cloneShape(b.local)
		s.Local = &local
		if b.Shape == b.synced {
			s.Synced = true
			s.SyncX, s.SyncY, s.SyncAngle = b.syncX, b.syncY, b.syncAngle
		}
	}
	return s
}

func applyRigidBodySnapshot(dst *RigidBody, src RigidBodySnapshot) {
	ud := dst.UserData
	*dst = RigidBody{
		ID:              src.ID,
		Tag:             src.Tag,
		Shape:           cloneShape(src.Shape),
		X:               src.X,
		Y:               src.Y,
		VX:              src.VX,
		VY:              src.VY,
		Angle:           src.Angle,
		AngularVel:      src.AngularVel,
		Mass:            src.Mass,
		InvMass:         src.InvMass,
		Inertia:         src.Inertia,
		InvInertia:      src.InvInertia,
		Restitution:     src.Restitution,
		Friction:        src.Friction,
		LinearDamping:   src.LinearDamping,
		AngularDamping:  src.AngularDamping,
		GravityScale:    src.GravityScale,
		Static:          src.Static,
		Layer:           src.Layer,
		Mask:            src.Mask,
		UserData:        ud,
		gravityScaleSet: src.GravityScaleSet,
		forceX:          src.ForceX,
		forceY:          src.ForceY,
		torque:          src.Torque,
		derivedInertia:  src.DerivedInertia,
		Bullet:          src.Bullet,
		sleeping:        src.Sleeping,
		sleepTime:       src.SleepTime,
		restValid:       src.RestValid,
		restX:           src.RestX,
		restY:           src.RestY,
		restAngle:       src.RestAngle,
		Kinematic:       src.Kinematic,
		Sensor:          src.Sensor,
		targetPending:   src.TargetPending,
		targetDriven:    src.TargetDriven,
	}
	if src.TargetPending {
		dst.target = rigidPose{x: src.TargetX, y: src.TargetY, angle: src.TargetAngle}
	}
	if src.TargetDriven {
		dst.targetVX, dst.targetVY, dst.targetAV = src.TargetVX, src.TargetVY, src.TargetAngularVel
	}
	if src.SleepEnv != nil {
		dst.sleepEnv = *src.SleepEnv
	}
	if src.Local != nil && hasFreeShape(src.Local.Kind) {
		dst.local = cloneShape(*src.Local)
		dst.hasLocal = true
		if src.Synced {
			dst.synced = dst.Shape
			dst.syncBounds = dst.Shape.Bounds()
			dst.syncX, dst.syncY, dst.syncAngle = src.SyncX, src.SyncY, src.SyncAngle
		}
	}
}

// cloneShape returns s with its Chain copied, so a snapshot and the live world never share chain points.
func cloneShape(s Shape) Shape {
	if s.Chain != nil {
		c := *s.Chain
		c.Points = append([]core.Point(nil), s.Chain.Points...)
		s.Chain = &c
	}
	return s
}

// Save captures a deep, deterministic, JSON-serializable snapshot of w.
func (w *RigidWorld) Save() RigidWorldSnapshot {
	if w == nil {
		return RigidWorldSnapshot{Version: SnapshotVersion}
	}
	snap := RigidWorldSnapshot{
		Version:               SnapshotVersion,
		GravityX:              w.GravityX,
		GravityY:              w.GravityY,
		SolverIterations:      w.SolverIterations,
		DisableSleep:          w.DisableSleep,
		SleepLinearThreshold:  w.SleepLinearThreshold,
		SleepAngularThreshold: w.SleepAngularThreshold,
		TimeToSleep:           w.TimeToSleep,
		LastDt:                w.lastDt,
	}

	bodyIdx := make(map[*RigidBody]int, len(w.Bodies))
	for _, b := range w.Bodies {
		if b == nil {
			continue
		}
		bodyIdx[b] = len(snap.Bodies)
		snap.Bodies = append(snap.Bodies, snapshotRigidBody(b))
	}

	resolveBodyRef := func(b *RigidBody) (int, *RigidBodySnapshot) {
		if b == nil {
			return -1, nil
		}
		if idx, ok := bodyIdx[b]; ok {
			return idx, nil
		}
		s := snapshotRigidBody(b)
		return -1, &s
	}

	for _, j := range w.Joints {
		if j == nil {
			continue
		}
		if js, ok := snapshotRigidJoint(j, resolveBodyRef); ok {
			snap.Joints = append(snap.Joints, js)
		}
	}

	if len(w.jointBreakEvents) > 0 {
		snap.JointBreakEvents = make([]RigidJointBreakEventSnapshot, len(w.jointBreakEvents))
		for i, ev := range w.jointBreakEvents {
			var jsPtr *RigidJointSnapshot
			if ev.Joint != nil {
				if js, ok := snapshotRigidJoint(ev.Joint, resolveBodyRef); ok {
					jsPtr = &js
				}
			}
			idxA, detA := resolveBodyRef(ev.BodyA)
			idxB, detB := resolveBodyRef(ev.BodyB)
			snap.JointBreakEvents[i] = RigidJointBreakEventSnapshot{
				Joint:      jsPtr,
				BodyAIndex: idxA,
				BodyBIndex: idxB,
				DetachedA:  detA,
				DetachedB:  detB,
				ForceX:     ev.ForceX,
				ForceY:     ev.ForceY,
				Torque:     ev.Torque,
			}
		}
	}

	for _, tr := range w.Triggers {
		if tr != nil {
			snap.Triggers = append(snap.Triggers, snapshotTrigger(tr))
		}
	}

	// Cached contacts with a body that has left the world would be dropped by the next step, so they are not saved.
	for _, c := range w.contacts {
		ia, ib, tile, ok := w.pairIndices(bodyIdx, c.key.a, c.key.b)
		if !ok {
			continue
		}
		cs := RigidContactSnapshot{BodyAIndex: ia, BodyBIndex: ib, Tile: tile, Sub: c.key.sub}
		for _, p := range c.points[:c.count] {
			cs.Points = append(cs.Points, RigidContactPointSnapshot{Feature: p.id, NormalImpulse: p.pn, TangentImpulse: p.pt})
		}
		snap.Contacts = append(snap.Contacts, cs)
	}

	snap.Grid = snapshotGrid(w.Tiles)
	snap.CustomTiles = w.Tiles != nil && snap.Grid == nil
	snap.TileLayer, snap.TileMask = w.TileLayer, w.TileMask
	snap.TileFriction, snap.TileRestitution = w.TileFriction, w.TileRestitution
	for _, t := range w.touching {
		ia, ib, tile, ok := w.pairIndices(bodyIdx, t.a, t.b)
		if !ok {
			continue
		}
		snap.Touching = append(snap.Touching, RigidTouchSnapshot{BodyAIndex: ia, BodyBIndex: ib, Tile: tile, Col: t.col, Row: t.row, TileFlags: t.flags})
	}
	for _, s := range w.sensing {
		is, okS := bodyIdx[s.sensor]
		iv, okV := bodyIdx[s.visitor]
		if okS && okV {
			snap.Sensing = append(snap.Sensing, RigidSenseSnapshot{SensorIndex: is, VisitorIndex: iv})
		}
	}
	return snap
}

// pairIndices returns the indices in bodyIdx of a pair of bodies, or of a body and the body that stands for w's Tiles, with -1 and tile set for the latter, and false when a body is not in bodyIdx.
func (w *RigidWorld) pairIndices(bodyIdx map[*RigidBody]int, a, b *RigidBody) (ia, ib int, tile, ok bool) {
	ia, okA := bodyIdx[a]
	if !okA {
		return 0, 0, false, false
	}
	if b != nil && b == w.tileBody {
		return ia, -1, true, true
	}
	ib, okB := bodyIdx[b]
	return ia, ib, false, okB
}

func snapshotJointReaction(r jointReaction, js *RigidJointSnapshot) {
	js.StepImpulseX = r.stepImpulseX
	js.StepImpulseY = r.stepImpulseY
	js.StepAngImpulse = r.stepAngImpulse
	js.LastDt = r.lastDt
	js.Broken = r.broken
}

func restoreJointReaction(js RigidJointSnapshot) jointReaction {
	return jointReaction{
		stepImpulseX:   js.StepImpulseX,
		stepImpulseY:   js.StepImpulseY,
		stepAngImpulse: js.StepAngImpulse,
		lastDt:         js.LastDt,
		broken:         js.Broken,
	}
}

func snapshotRigidJoint(j Joint, resolveBodyRef func(*RigidBody) (int, *RigidBodySnapshot)) (RigidJointSnapshot, bool) {
	switch jt := j.(type) {
	case *DistanceJoint:
		idxA, detA := resolveBodyRef(jt.A)
		idxB, detB := resolveBodyRef(jt.B)
		js := RigidJointSnapshot{
			Kind:             "distance",
			BodyAIndex:       idxA,
			BodyBIndex:       idxB,
			DetachedA:        detA,
			DetachedB:        detB,
			AnchorAX:         float32(jt.AnchorA.X),
			AnchorAY:         float32(jt.AnchorA.Y),
			AnchorBX:         float32(jt.AnchorB.X),
			AnchorBY:         float32(jt.AnchorB.Y),
			Length:           jt.Length,
			MinLength:        jt.MinLength,
			MaxLength:        jt.MaxLength,
			MaxOnly:          jt.MaxOnly,
			Stiffness:        jt.Stiffness,
			Compliance:       jt.Compliance,
			Damping:          jt.Damping,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Initialized:      jt.initialized,
			Active:           jt.active,
			Dt:               jt.dt,
			UX:               jt.ux,
			UY:               jt.uy,
			RAX:              jt.rAx,
			RAY:              jt.rAy,
			RBX:              jt.rBx,
			RBY:              jt.rBy,
			EffMass:          jt.effMass,
			AccImpulse:       jt.accImpulse,
			Mode:             jt.mode,
			TargetLen:        jt.targetLen,
		}
		snapshotJointReaction(jt.jointReaction, &js)
		return js, true
	case *SpringJoint:
		idxA, detA := resolveBodyRef(jt.A)
		idxB, detB := resolveBodyRef(jt.B)
		js := RigidJointSnapshot{
			Kind:             "spring",
			BodyAIndex:       idxA,
			BodyBIndex:       idxB,
			DetachedA:        detA,
			DetachedB:        detB,
			AnchorAX:         float32(jt.AnchorA.X),
			AnchorAY:         float32(jt.AnchorA.Y),
			AnchorBX:         float32(jt.AnchorB.X),
			AnchorBY:         float32(jt.AnchorB.Y),
			RestLength:       jt.RestLength,
			Stiffness:        jt.Stiffness,
			Damping:          jt.Damping,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Initialized:      jt.initialized,
		}
		snapshotJointReaction(jt.jointReaction, &js)
		return js, true
	case *RevoluteJoint:
		idxA, detA := resolveBodyRef(jt.A)
		idxB, detB := resolveBodyRef(jt.B)
		js := RigidJointSnapshot{
			Kind:             "revolute",
			BodyAIndex:       idxA,
			BodyBIndex:       idxB,
			DetachedA:        detA,
			DetachedB:        detB,
			AnchorAX:         float32(jt.AnchorA.X),
			AnchorAY:         float32(jt.AnchorA.Y),
			AnchorBX:         float32(jt.AnchorB.X),
			AnchorBY:         float32(jt.AnchorB.Y),
			ReferenceAngle:   jt.ReferenceAngle,
			EnableLimit:      jt.EnableLimit,
			LowerAngle:       jt.LowerAngle,
			UpperAngle:       jt.UpperAngle,
			EnableMotor:      jt.EnableMotor,
			MotorSpeed:       jt.MotorSpeed,
			MaxMotorTorque:   jt.MaxMotorTorque,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Active:           jt.active,
			Dt:               jt.dt,
			RAX:              jt.rAx,
			RAY:              jt.rAy,
			RBX:              jt.rBx,
			RBY:              jt.rBy,
			M11:              jt.m11,
			M12:              jt.m12,
			M22:              jt.m22,
			MotorMass:        jt.motorMass,
			MotorImpulse:     jt.motorImpulse,
			LimitImpulse:     jt.limitImpulse,
			LimitState:       jt.limitState,
		}
		snapshotJointReaction(jt.jointReaction, &js)
		return js, true
	case *PrismaticJoint:
		idxA, detA := resolveBodyRef(jt.A)
		idxB, detB := resolveBodyRef(jt.B)
		js := RigidJointSnapshot{
			Kind:             "prismatic",
			BodyAIndex:       idxA,
			BodyBIndex:       idxB,
			DetachedA:        detA,
			DetachedB:        detB,
			AnchorAX:         float32(jt.AnchorA.X),
			AnchorAY:         float32(jt.AnchorA.Y),
			AnchorBX:         float32(jt.AnchorB.X),
			AnchorBY:         float32(jt.AnchorB.Y),
			LocalAxisAX:      float32(jt.LocalAxisA.X),
			LocalAxisAY:      float32(jt.LocalAxisA.Y),
			ReferenceAngle:   jt.ReferenceAngle,
			EnableLimit:      jt.EnableLimit,
			LowerTranslation: jt.LowerTranslation,
			UpperTranslation: jt.UpperTranslation,
			EnableMotor:      jt.EnableMotor,
			MotorSpeed:       jt.MotorSpeed,
			MaxMotorForce:    jt.MaxMotorForce,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Active:           jt.active,
			RotLocked:        jt.rotLocked,
			Dt:               jt.dt,
			AX:               jt.ax,
			AY:               jt.ay,
			PX:               jt.px,
			PY:               jt.py,
			S1:               jt.s1,
			S2:               jt.s2,
			A1:               jt.a1,
			A2:               jt.a2,
			M11:              jt.perpM11,
			M12:              jt.perpM12,
			M22:              jt.perpM22,
			MotorMass:        jt.motorMass,
			MotorImpulse:     jt.motorImpulse,
			LimitImpulse:     jt.limitImpulse,
			LimitState:       jt.limitState,
		}
		snapshotJointReaction(jt.jointReaction, &js)
		return js, true
	case *WeldJoint:
		idxA, detA := resolveBodyRef(jt.A)
		idxB, detB := resolveBodyRef(jt.B)
		js := RigidJointSnapshot{
			Kind:             "weld",
			BodyAIndex:       idxA,
			BodyBIndex:       idxB,
			DetachedA:        detA,
			DetachedB:        detB,
			AnchorAX:         float32(jt.AnchorA.X),
			AnchorAY:         float32(jt.AnchorA.Y),
			AnchorBX:         float32(jt.AnchorB.X),
			AnchorBY:         float32(jt.AnchorB.Y),
			ReferenceAngle:   jt.ReferenceAngle,
			FrequencyHz:      jt.FrequencyHz,
			DampingRatio:     jt.DampingRatio,
			Stiffness:        jt.Stiffness,
			Damping:          jt.Damping,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Active:           jt.active,
			Soft:             jt.soft,
			Dt:               jt.dt,
			RAX:              jt.rAx,
			RAY:              jt.rAy,
			RBX:              jt.rBx,
			RBY:              jt.rBy,
			KA:               jt.k.a,
			KB:               jt.k.b,
			KC:               jt.k.c,
			KD:               jt.k.d,
			KE:               jt.k.e,
			KF:               jt.k.f,
			Gamma:            jt.gammaLin,
			GammaAng:         jt.gammaAng,
			BiasX:            jt.biasX,
			BiasY:            jt.biasY,
			BiasAng:          jt.biasAng,
			ImpulseX:         jt.impulseX,
			ImpulseY:         jt.impulseY,
			AngImpulse:       jt.angImpulse,
		}
		snapshotJointReaction(jt.jointReaction, &js)
		return js, true
	case *WheelJoint:
		idxA, detA := resolveBodyRef(jt.A)
		idxB, detB := resolveBodyRef(jt.B)
		js := RigidJointSnapshot{
			Kind:             "wheel",
			BodyAIndex:       idxA,
			BodyBIndex:       idxB,
			DetachedA:        detA,
			DetachedB:        detB,
			AnchorAX:         float32(jt.AnchorA.X),
			AnchorAY:         float32(jt.AnchorA.Y),
			AnchorBX:         float32(jt.AnchorB.X),
			AnchorBY:         float32(jt.AnchorB.Y),
			LocalAxisAX:      float32(jt.LocalAxisA.X),
			LocalAxisAY:      float32(jt.LocalAxisA.Y),
			FrequencyHz:      jt.FrequencyHz,
			DampingRatio:     jt.DampingRatio,
			Stiffness:        jt.Stiffness,
			Damping:          jt.Damping,
			EnableLimit:      jt.EnableLimit,
			LowerTranslation: jt.LowerTranslation,
			UpperTranslation: jt.UpperTranslation,
			EnableMotor:      jt.EnableMotor,
			MotorSpeed:       jt.MotorSpeed,
			MaxMotorTorque:   jt.MaxMotorTorque,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Active:           jt.active,
			SpringActive:     jt.springActive,
			Dt:               jt.dt,
			AX:               jt.ax,
			AY:               jt.ay,
			PX:               jt.px,
			PY:               jt.py,
			S1:               jt.sAx,
			S2:               jt.sBx,
			A1:               jt.a1,
			A2:               jt.a2,
			PerpMass:         jt.perpMass,
			AxisMass:         jt.axisMass,
			SpringMass:       jt.springMass,
			MotorMass:        jt.motorMass,
			Gamma:            jt.gamma,
			Bias:             jt.bias,
			SpringImpulse:    jt.springImpulse,
			MotorImpulse:     jt.motorImpulse,
			LimitImpulse:     jt.limitImpulse,
			LimitState:       jt.limitState,
		}
		snapshotJointReaction(jt.jointReaction, &js)
		return js, true
	case *MouseJoint:
		idxB, detB := resolveBodyRef(jt.Body)
		js := RigidJointSnapshot{
			Kind:         "mouse",
			BodyAIndex:   -1,
			BodyBIndex:   idxB,
			DetachedB:    detB,
			AnchorBX:     float32(jt.Anchor.X),
			AnchorBY:     float32(jt.Anchor.Y),
			TargetX:      float32(jt.Target.X),
			TargetY:      float32(jt.Target.Y),
			MaxForce:     jt.MaxForce,
			FrequencyHz:  jt.FrequencyHz,
			DampingRatio: jt.DampingRatio,
			Stiffness:    jt.Stiffness,
			Damping:      jt.Damping,
			BreakForce:   jt.BreakForce,
			BreakTorque:  jt.BreakTorque,
			Active:       jt.active,
			Dt:           jt.dt,
			RBX:          jt.rX,
			RBY:          jt.rY,
			M11:          jt.m11,
			M12:          jt.m12,
			M22:          jt.m22,
			Gamma:        jt.gamma,
			BiasX:        jt.biasX,
			BiasY:        jt.biasY,
			ImpulseX:     jt.impulseX,
			ImpulseY:     jt.impulseY,
		}
		snapshotJointReaction(jt.jointReaction, &js)
		return js, true
	default:
		return RigidJointSnapshot{}, false
	}
}

func validateRigidJointSnapshot(js RigidJointSnapshot, bodyCount int, label string) error {
	switch js.Kind {
	case "distance", "spring", "revolute", "prismatic", "weld", "wheel", "mouse":
	default:
		return fmt.Errorf("physics: %s has unknown kind %q", label, js.Kind)
	}
	if js.BodyAIndex < -1 || js.BodyAIndex >= bodyCount {
		return fmt.Errorf("physics: %s has out-of-range bodyA index %d", label, js.BodyAIndex)
	}
	if js.BodyBIndex < -1 || js.BodyBIndex >= bodyCount {
		return fmt.Errorf("physics: %s has out-of-range bodyB index %d", label, js.BodyBIndex)
	}
	return nil
}

func restoreRigidJoint(js RigidJointSnapshot, existing Joint, resolveRestoredBody func(int, *RigidBodySnapshot) *RigidBody) Joint {
	bodyA := resolveRestoredBody(js.BodyAIndex, js.DetachedA)
	bodyB := resolveRestoredBody(js.BodyBIndex, js.DetachedB)
	anchorA := core.Point{X: core.Points(js.AnchorAX), Y: core.Points(js.AnchorAY)}
	anchorB := core.Point{X: core.Points(js.AnchorBX), Y: core.Points(js.AnchorBY)}
	rxn := restoreJointReaction(js)

	switch js.Kind {
	case "distance":
		dst, _ := existing.(*DistanceJoint)
		if dst == nil {
			dst = new(DistanceJoint)
		}
		*dst = DistanceJoint{
			A:                bodyA,
			B:                bodyB,
			AnchorA:          anchorA,
			AnchorB:          anchorB,
			Length:           js.Length,
			MinLength:        js.MinLength,
			MaxLength:        js.MaxLength,
			MaxOnly:          js.MaxOnly,
			Stiffness:        js.Stiffness,
			Compliance:       js.Compliance,
			Damping:          js.Damping,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    rxn,
			initialized:      js.Initialized,
			active:           js.Active,
			dt:               js.Dt,
			ux:               js.UX,
			uy:               js.UY,
			rAx:              js.RAX,
			rAy:              js.RAY,
			rBx:              js.RBX,
			rBy:              js.RBY,
			effMass:          js.EffMass,
			accImpulse:       js.AccImpulse,
			mode:             js.Mode,
			targetLen:        js.TargetLen,
		}
		return dst
	case "spring":
		dst, _ := existing.(*SpringJoint)
		if dst == nil {
			dst = new(SpringJoint)
		}
		*dst = SpringJoint{
			A:                bodyA,
			B:                bodyB,
			AnchorA:          anchorA,
			AnchorB:          anchorB,
			RestLength:       js.RestLength,
			Stiffness:        js.Stiffness,
			Damping:          js.Damping,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    rxn,
			initialized:      js.Initialized,
		}
		return dst
	case "revolute":
		dst, _ := existing.(*RevoluteJoint)
		if dst == nil {
			dst = new(RevoluteJoint)
		}
		*dst = RevoluteJoint{
			A:                bodyA,
			B:                bodyB,
			AnchorA:          anchorA,
			AnchorB:          anchorB,
			ReferenceAngle:   js.ReferenceAngle,
			EnableLimit:      js.EnableLimit,
			LowerAngle:       js.LowerAngle,
			UpperAngle:       js.UpperAngle,
			EnableMotor:      js.EnableMotor,
			MotorSpeed:       js.MotorSpeed,
			MaxMotorTorque:   js.MaxMotorTorque,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    rxn,
			active:           js.Active,
			dt:               js.Dt,
			rAx:              js.RAX,
			rAy:              js.RAY,
			rBx:              js.RBX,
			rBy:              js.RBY,
			m11:              js.M11,
			m12:              js.M12,
			m22:              js.M22,
			motorMass:        js.MotorMass,
			motorImpulse:     js.MotorImpulse,
			limitImpulse:     js.LimitImpulse,
			limitState:       js.LimitState,
		}
		return dst
	case "prismatic":
		dst, _ := existing.(*PrismaticJoint)
		if dst == nil {
			dst = new(PrismaticJoint)
		}
		*dst = PrismaticJoint{
			A:                bodyA,
			B:                bodyB,
			AnchorA:          anchorA,
			AnchorB:          anchorB,
			LocalAxisA:       core.Point{X: core.Points(js.LocalAxisAX), Y: core.Points(js.LocalAxisAY)},
			ReferenceAngle:   js.ReferenceAngle,
			EnableLimit:      js.EnableLimit,
			LowerTranslation: js.LowerTranslation,
			UpperTranslation: js.UpperTranslation,
			EnableMotor:      js.EnableMotor,
			MotorSpeed:       js.MotorSpeed,
			MaxMotorForce:    js.MaxMotorForce,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    rxn,
			active:           js.Active,
			dt:               js.Dt,
			ax:               js.AX,
			ay:               js.AY,
			px:               js.PX,
			py:               js.PY,
			s1:               js.S1,
			s2:               js.S2,
			a1:               js.A1,
			a2:               js.A2,
			perpM11:          js.M11,
			perpM12:          js.M12,
			perpM22:          js.M22,
			rotLocked:        js.RotLocked,
			motorMass:        js.MotorMass,
			motorImpulse:     js.MotorImpulse,
			limitImpulse:     js.LimitImpulse,
			limitState:       js.LimitState,
		}
		return dst
	case "weld":
		dst, _ := existing.(*WeldJoint)
		if dst == nil {
			dst = new(WeldJoint)
		}
		*dst = WeldJoint{
			A:                bodyA,
			B:                bodyB,
			AnchorA:          anchorA,
			AnchorB:          anchorB,
			ReferenceAngle:   js.ReferenceAngle,
			FrequencyHz:      js.FrequencyHz,
			DampingRatio:     js.DampingRatio,
			Stiffness:        js.Stiffness,
			Damping:          js.Damping,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    rxn,
			active:           js.Active,
			soft:             js.Soft,
			dt:               js.Dt,
			rAx:              js.RAX,
			rAy:              js.RAY,
			rBx:              js.RBX,
			rBy:              js.RBY,
			k: symMat3_64{
				a: js.KA, b: js.KB, c: js.KC,
				d: js.KD, e: js.KE, f: js.KF,
			},
			gammaLin:   js.Gamma,
			gammaAng:   js.GammaAng,
			biasX:      js.BiasX,
			biasY:      js.BiasY,
			biasAng:    js.BiasAng,
			impulseX:   js.ImpulseX,
			impulseY:   js.ImpulseY,
			angImpulse: js.AngImpulse,
		}
		return dst
	case "wheel":
		dst, _ := existing.(*WheelJoint)
		if dst == nil {
			dst = new(WheelJoint)
		}
		*dst = WheelJoint{
			A:                bodyA,
			B:                bodyB,
			AnchorA:          anchorA,
			AnchorB:          anchorB,
			LocalAxisA:       core.Point{X: core.Points(js.LocalAxisAX), Y: core.Points(js.LocalAxisAY)},
			FrequencyHz:      js.FrequencyHz,
			DampingRatio:     js.DampingRatio,
			Stiffness:        js.Stiffness,
			Damping:          js.Damping,
			EnableLimit:      js.EnableLimit,
			LowerTranslation: js.LowerTranslation,
			UpperTranslation: js.UpperTranslation,
			EnableMotor:      js.EnableMotor,
			MotorSpeed:       js.MotorSpeed,
			MaxMotorTorque:   js.MaxMotorTorque,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    rxn,
			active:           js.Active,
			dt:               js.Dt,
			ax:               js.AX,
			ay:               js.AY,
			px:               js.PX,
			py:               js.PY,
			sAx:              js.S1,
			sBx:              js.S2,
			a1:               js.A1,
			a2:               js.A2,
			perpMass:         js.PerpMass,
			axisMass:         js.AxisMass,
			springMass:       js.SpringMass,
			motorMass:        js.MotorMass,
			gamma:            js.Gamma,
			bias:             js.Bias,
			springActive:     js.SpringActive,
			springImpulse:    js.SpringImpulse,
			motorImpulse:     js.MotorImpulse,
			limitImpulse:     js.LimitImpulse,
			limitState:       js.LimitState,
		}
		return dst
	case "mouse":
		dst, _ := existing.(*MouseJoint)
		if dst == nil {
			dst = new(MouseJoint)
		}
		*dst = MouseJoint{
			Body:          bodyB,
			Anchor:        anchorB,
			Target:        core.Point{X: core.Points(js.TargetX), Y: core.Points(js.TargetY)},
			MaxForce:      js.MaxForce,
			FrequencyHz:   js.FrequencyHz,
			DampingRatio:  js.DampingRatio,
			Stiffness:     js.Stiffness,
			Damping:       js.Damping,
			BreakForce:    js.BreakForce,
			BreakTorque:   js.BreakTorque,
			jointReaction: rxn,
			active:        js.Active,
			dt:            js.Dt,
			rX:            js.RBX,
			rY:            js.RBY,
			m11:           js.M11,
			m12:           js.M12,
			m22:           js.M22,
			gamma:         js.Gamma,
			biasX:         js.BiasX,
			biasY:         js.BiasY,
			impulseX:      js.ImpulseX,
			impulseY:      js.ImpulseY,
		}
		return dst
	default:
		return nil
	}
}

// Restore replaces w's state from snap. Existing *RigidBody, Joint, and
// *Trigger pointers are reused in place when possible so caller handles,
// UserData, and callbacks stay valid across rollback restores.
func (w *RigidWorld) Restore(snap RigidWorldSnapshot) error {
	if w == nil {
		return errors.New("physics: cannot restore nil RigidWorld")
	}
	if snap.Version < oldestSnapshotVersion || snap.Version > SnapshotVersion {
		return fmt.Errorf("physics: unsupported RigidWorldSnapshot version %d (want %d to %d)", snap.Version, oldestSnapshotVersion, SnapshotVersion)
	}
	for i, js := range snap.Joints {
		if err := validateRigidJointSnapshot(js, len(snap.Bodies), fmt.Sprintf("joints[%d]", i)); err != nil {
			return err
		}
	}
	for i, ev := range snap.JointBreakEvents {
		if ev.Joint != nil {
			if err := validateRigidJointSnapshot(*ev.Joint, len(snap.Bodies), fmt.Sprintf("jointBreakEvents[%d].joint", i)); err != nil {
				return err
			}
		}
		if ev.BodyAIndex < -1 || ev.BodyAIndex >= len(snap.Bodies) {
			return fmt.Errorf("physics: jointBreakEvents[%d] has out-of-range bodyA index %d", i, ev.BodyAIndex)
		}
		if ev.BodyBIndex < -1 || ev.BodyBIndex >= len(snap.Bodies) {
			return fmt.Errorf("physics: jointBreakEvents[%d] has out-of-range bodyB index %d", i, ev.BodyBIndex)
		}
	}
	if snap.Grid != nil && snap.CustomTiles {
		return fmt.Errorf("physics: RigidWorldSnapshot has both a grid and customTiles")
	}
	validPair := func(a, b int, tile bool) bool {
		if a < 0 || a >= len(snap.Bodies) {
			return false
		}
		if tile {
			return b == -1
		}
		return b >= 0 && b < len(snap.Bodies) && a != b
	}
	for i, cs := range snap.Contacts {
		if !validPair(cs.BodyAIndex, cs.BodyBIndex, cs.Tile) || (cs.Tile && cs.Sub < 0) {
			return fmt.Errorf("physics: contacts[%d] has out-of-range bodies %d and %d", i, cs.BodyAIndex, cs.BodyBIndex)
		}
		if len(cs.Points) > 2 {
			return fmt.Errorf("physics: contacts[%d] has %d points (at most 2)", i, len(cs.Points))
		}
	}
	for i, ts := range snap.Touching {
		if !validPair(ts.BodyAIndex, ts.BodyBIndex, ts.Tile) {
			return fmt.Errorf("physics: touching[%d] has out-of-range bodies %d and %d", i, ts.BodyAIndex, ts.BodyBIndex)
		}
	}
	for i, ss := range snap.Sensing {
		if !validPair(ss.SensorIndex, ss.VisitorIndex, false) {
			return fmt.Errorf("physics: sensing[%d] has out-of-range bodies %d and %d", i, ss.SensorIndex, ss.VisitorIndex)
		}
	}

	existingBodiesByID := make(map[int]*RigidBody, len(w.Bodies))
	existingBodiesUsed := make(map[*RigidBody]bool, len(w.Bodies))
	for _, b := range w.Bodies {
		if b != nil && b.ID != 0 {
			if _, dup := existingBodiesByID[b.ID]; !dup {
				existingBodiesByID[b.ID] = b
			}
		}
	}

	restoredBodies := make([]*RigidBody, len(snap.Bodies))
	for i, bs := range snap.Bodies {
		var dst *RigidBody
		if bs.ID != 0 {
			if cand, ok := existingBodiesByID[bs.ID]; ok && !existingBodiesUsed[cand] {
				dst = cand
			}
		}
		if dst == nil && i < len(w.Bodies) && w.Bodies[i] != nil && !existingBodiesUsed[w.Bodies[i]] {
			dst = w.Bodies[i]
		}
		if dst == nil {
			dst = new(RigidBody)
		}
		existingBodiesUsed[dst] = true
		applyRigidBodySnapshot(dst, bs)
		restoredBodies[i] = dst
	}

	resolveRestoredBody := func(idx int, det *RigidBodySnapshot) *RigidBody {
		if idx >= 0 {
			return restoredBodies[idx]
		}
		if det != nil {
			b := new(RigidBody)
			applyRigidBodySnapshot(b, *det)
			return b
		}
		return nil
	}

	restoredJoints := make([]Joint, len(snap.Joints))
	for i, js := range snap.Joints {
		var existing Joint
		if i < len(w.Joints) {
			existing = w.Joints[i]
		}
		restoredJoints[i] = restoreRigidJoint(js, existing, resolveRestoredBody)
	}

	var restoredBreakEvents []JointBreakEvent
	if len(snap.JointBreakEvents) > 0 {
		restoredBreakEvents = make([]JointBreakEvent, len(snap.JointBreakEvents))
		for i, ev := range snap.JointBreakEvents {
			var j Joint
			if ev.Joint != nil {
				j = restoreRigidJoint(*ev.Joint, nil, resolveRestoredBody)
			}
			restoredBreakEvents[i] = JointBreakEvent{
				Joint:  j,
				BodyA:  resolveRestoredBody(ev.BodyAIndex, ev.DetachedA),
				BodyB:  resolveRestoredBody(ev.BodyBIndex, ev.DetachedB),
				ForceX: ev.ForceX,
				ForceY: ev.ForceY,
				Torque: ev.Torque,
			}
		}
	}

	existingTriggersByID := make(map[int]*Trigger, len(w.Triggers))
	existingTriggersUsed := make(map[*Trigger]bool, len(w.Triggers))
	for _, tr := range w.Triggers {
		if tr != nil && tr.ID != 0 {
			if _, dup := existingTriggersByID[tr.ID]; !dup {
				existingTriggersByID[tr.ID] = tr
			}
		}
	}

	restoredTriggers := make([]*Trigger, len(snap.Triggers))
	for i, ts := range snap.Triggers {
		var dst *Trigger
		if ts.ID != 0 {
			if cand, ok := existingTriggersByID[ts.ID]; ok && !existingTriggersUsed[cand] {
				dst = cand
			}
		}
		if dst == nil && i < len(w.Triggers) && w.Triggers[i] != nil && !existingTriggersUsed[w.Triggers[i]] {
			dst = w.Triggers[i]
		}
		if dst == nil {
			dst = new(Trigger)
		}
		existingTriggersUsed[dst] = true
		applyTriggerSnapshot(dst, ts)
		restoredTriggers[i] = dst
	}

	if w.tileBody == nil {
		w.tileBody = &RigidBody{Static: true}
	}
	pairBody := func(i int, tile bool) *RigidBody {
		if tile {
			return w.tileBody
		}
		return restoredBodies[i]
	}
	contacts := make([]rigidCachedManifold, 0, len(snap.Contacts))
	for _, cs := range snap.Contacts {
		c := rigidCachedManifold{
			key:   rigidPairKey{a: restoredBodies[cs.BodyAIndex], b: pairBody(cs.BodyBIndex, cs.Tile), sub: cs.Sub},
			count: len(cs.Points),
		}
		for k, p := range cs.Points {
			c.points[k] = rigidCachedPoint{id: p.Feature, pn: p.NormalImpulse, pt: p.TangentImpulse}
		}
		contacts = append(contacts, c)
	}

	w.GravityX = snap.GravityX
	w.GravityY = snap.GravityY
	w.SolverIterations = snap.SolverIterations
	w.DisableSleep = snap.DisableSleep
	w.SleepLinearThreshold = snap.SleepLinearThreshold
	w.SleepAngularThreshold = snap.SleepAngularThreshold
	w.TimeToSleep = snap.TimeToSleep
	w.Bodies = restoredBodies
	w.Joints = restoredJoints
	w.jointBreakEvents = restoredBreakEvents
	w.Triggers = restoredTriggers
	w.contacts = contacts
	w.lastDt = snap.LastDt

	switch {
	case snap.Grid != nil:
		w.Tiles = restoreGrid(w.Tiles, snap.Grid)
	case !snap.CustomTiles:
		w.Tiles = nil
	}
	w.TileLayer, w.TileMask = snap.TileLayer, snap.TileMask
	w.TileFriction, w.TileRestitution = snap.TileFriction, snap.TileRestitution
	// The cache's tiles name cells of the restored grid, so the next step must not see its geometry as changed.
	w.tileGeom = tileGeomOf(w.Tiles)
	w.touching = make([]rigidTouch, 0, len(snap.Touching))
	for _, ts := range snap.Touching {
		w.touching = append(w.touching, rigidTouch{a: restoredBodies[ts.BodyAIndex], b: pairBody(ts.BodyBIndex, ts.Tile), col: ts.Col, row: ts.Row, flags: ts.TileFlags})
	}
	w.sensing = make([]rigidSense, 0, len(snap.Sensing))
	for _, ss := range snap.Sensing {
		w.sensing = append(w.sensing, rigidSense{sensor: restoredBodies[ss.SensorIndex], visitor: restoredBodies[ss.VisitorIndex]})
	}
	w.contactEvents = nil
	w.sensorEvents = nil
	return nil
}

// Checksum returns a deterministic 64-bit FNV-1a hash of w's complete state,
// including private body forces/torques and joint solver state.
func (w *RigidWorld) Checksum() uint64 {
	h := detmath.NewHasher()
	h.WriteString("physics.RigidWorld.v1")
	if w == nil {
		return h.Sum64()
	}
	h.WriteFloat32(w.GravityX)
	h.WriteFloat32(w.GravityY)
	h.WriteInt(w.SolverIterations)
	h.WriteBool(w.DisableSleep)
	h.WriteFloat32(w.SleepLinearThreshold)
	h.WriteFloat32(w.SleepAngularThreshold)
	h.WriteFloat32(w.TimeToSleep)
	h.WriteFloat64(w.lastDt)

	bodyIdx := make(map[*RigidBody]int, len(w.Bodies))
	bodyCount := 0
	for _, b := range w.Bodies {
		if b != nil {
			bodyIdx[b] = bodyCount
			bodyCount++
		}
	}
	h.WriteInt(bodyCount)
	for _, b := range w.Bodies {
		if b != nil {
			hashRigidBody(&h, b)
		}
	}

	jointCount := 0
	for _, j := range w.Joints {
		if j != nil {
			jointCount++
		}
	}
	h.WriteInt(jointCount)
	hashBodyRef := func(b *RigidBody) {
		if b == nil {
			h.WriteInt(-2)
			return
		}
		if idx, ok := bodyIdx[b]; ok {
			h.WriteInt(idx)
			return
		}
		h.WriteInt(-1)
		hashRigidBody(&h, b)
	}
	for _, j := range w.Joints {
		if j == nil {
			continue
		}
		hashRigidJoint(&h, j, hashBodyRef)
	}

	h.WriteInt(len(w.jointBreakEvents))
	for _, ev := range w.jointBreakEvents {
		h.WriteBool(ev.Joint != nil)
		if ev.Joint != nil {
			hashRigidJoint(&h, ev.Joint, hashBodyRef)
		}
		hashBodyRef(ev.BodyA)
		hashBodyRef(ev.BodyB)
		h.WriteFloat32(ev.ForceX)
		h.WriteFloat32(ev.ForceY)
		h.WriteFloat32(ev.Torque)
	}

	trigCount := 0
	for _, tr := range w.Triggers {
		if tr != nil {
			trigCount++
		}
	}
	h.WriteInt(trigCount)
	for _, tr := range w.Triggers {
		if tr != nil {
			hashTrigger(&h, tr)
		}
	}
	// The contact cache, in order, with the same rule as Save for contacts whose bodies left the world.
	contactCount := 0
	for _, c := range w.contacts {
		if _, _, _, ok := w.pairIndices(bodyIdx, c.key.a, c.key.b); ok {
			contactCount++
		}
	}
	h.WriteInt(contactCount)
	for _, c := range w.contacts {
		ia, ib, _, ok := w.pairIndices(bodyIdx, c.key.a, c.key.b)
		if !ok {
			continue
		}
		h.WriteInt(ia)
		h.WriteInt(ib)
		h.WriteInt(c.key.sub)
		h.WriteInt(c.count)
		for _, p := range c.points[:c.count] {
			h.WriteUint32(p.id)
			h.WriteFloat64(p.pn)
			h.WriteFloat64(p.pt)
		}
	}
	w.hashTilesAndEvents(&h, bodyIdx)
	return h.Sum64()
}

// hashTilesAndEvents hashes the Tiles, their settings and the contact and sensor state events are reported from, with the same rule as Save for entries whose bodies left the world. A world with no Tiles, default tile settings, no sensor overlap and no bodies in contact hashes nothing more, so its digest is the one it had before they existed; any world whose bodies touch hashes its contact list, so its digest differs from earlier releases.
func (w *RigidWorld) hashTilesAndEvents(h *detmath.Hasher, bodyIdx map[*RigidBody]int) {
	if w.Tiles == nil && w.TileLayer == 0 && w.TileMask == 0 && w.TileFriction == 0 && w.TileRestitution == 0 && len(w.touching) == 0 && len(w.sensing) == 0 {
		return
	}
	h.WriteString("physics.RigidWorld.tiles-events")
	h.WriteBool(w.Tiles != nil)
	if w.Tiles != nil {
		tw, th := w.Tiles.TileSize()
		cols, rows := w.Tiles.GridSize()
		ox, oy := tileGridOrigin(w.Tiles)
		h.WriteFloat32(tw)
		h.WriteFloat32(th)
		h.WriteFloat64(ox)
		h.WriteFloat64(oy)
		h.WriteInt(cols)
		h.WriteInt(rows)
		for row := range rows {
			for col := range cols {
				h.WriteUint32(uint32(w.Tiles.TileFlagsAt(col, row)))
			}
		}
	}
	h.WriteUint32(uint32(w.TileLayer))
	h.WriteUint32(uint32(w.TileMask))
	h.WriteFloat32(w.TileFriction)
	h.WriteFloat32(w.TileRestitution)
	touchCount := 0
	for _, t := range w.touching {
		if _, _, _, ok := w.pairIndices(bodyIdx, t.a, t.b); ok {
			touchCount++
		}
	}
	h.WriteInt(touchCount)
	for _, t := range w.touching {
		ia, ib, _, ok := w.pairIndices(bodyIdx, t.a, t.b)
		if !ok {
			continue
		}
		h.WriteInt(ia)
		h.WriteInt(ib)
		h.WriteInt(t.col)
		h.WriteInt(t.row)
		h.WriteUint32(uint32(t.flags))
	}
	senseCount := 0
	for _, s := range w.sensing {
		_, okS := bodyIdx[s.sensor]
		_, okV := bodyIdx[s.visitor]
		if okS && okV {
			senseCount++
		}
	}
	h.WriteInt(senseCount)
	for _, s := range w.sensing {
		is, okS := bodyIdx[s.sensor]
		iv, okV := bodyIdx[s.visitor]
		if okS && okV {
			h.WriteInt(is)
			h.WriteInt(iv)
		}
	}
}

func hashJointReaction(h *detmath.Hasher, r jointReaction) {
	h.WriteFloat64(r.stepImpulseX)
	h.WriteFloat64(r.stepImpulseY)
	h.WriteFloat64(r.stepAngImpulse)
	h.WriteFloat64(r.lastDt)
	h.WriteBool(r.broken)
}

func hashRigidJoint(h *detmath.Hasher, j Joint, hashBodyRef func(*RigidBody)) {
	switch jt := j.(type) {
	case *DistanceJoint:
		h.WriteUint8(1)
		hashBodyRef(jt.A)
		hashBodyRef(jt.B)
		h.WriteFloat32(float32(jt.AnchorA.X))
		h.WriteFloat32(float32(jt.AnchorA.Y))
		h.WriteFloat32(float32(jt.AnchorB.X))
		h.WriteFloat32(float32(jt.AnchorB.Y))
		h.WriteFloat32(jt.Length)
		h.WriteFloat32(jt.MinLength)
		h.WriteFloat32(jt.MaxLength)
		h.WriteBool(jt.MaxOnly)
		h.WriteFloat32(jt.Stiffness)
		h.WriteFloat32(jt.Compliance)
		h.WriteFloat32(jt.Damping)
		h.WriteFloat32(jt.BreakForce)
		h.WriteFloat32(jt.BreakTorque)
		h.WriteBool(jt.CollideConnected)
		hashJointReaction(h, jt.jointReaction)
		h.WriteBool(jt.initialized)
		h.WriteBool(jt.active)
		h.WriteFloat64(jt.dt)
		h.WriteFloat64(jt.ux)
		h.WriteFloat64(jt.uy)
		h.WriteFloat64(jt.rAx)
		h.WriteFloat64(jt.rAy)
		h.WriteFloat64(jt.rBx)
		h.WriteFloat64(jt.rBy)
		h.WriteFloat64(jt.effMass)
		h.WriteFloat64(jt.accImpulse)
		h.WriteInt(jt.mode)
		h.WriteFloat64(jt.targetLen)
	case *SpringJoint:
		h.WriteUint8(2)
		hashBodyRef(jt.A)
		hashBodyRef(jt.B)
		h.WriteFloat32(float32(jt.AnchorA.X))
		h.WriteFloat32(float32(jt.AnchorA.Y))
		h.WriteFloat32(float32(jt.AnchorB.X))
		h.WriteFloat32(float32(jt.AnchorB.Y))
		h.WriteFloat32(jt.RestLength)
		h.WriteFloat32(jt.Stiffness)
		h.WriteFloat32(jt.Damping)
		h.WriteFloat32(jt.BreakForce)
		h.WriteFloat32(jt.BreakTorque)
		h.WriteBool(jt.CollideConnected)
		hashJointReaction(h, jt.jointReaction)
		h.WriteBool(jt.initialized)
	case *RevoluteJoint:
		h.WriteUint8(3)
		hashBodyRef(jt.A)
		hashBodyRef(jt.B)
		h.WriteFloat32(float32(jt.AnchorA.X))
		h.WriteFloat32(float32(jt.AnchorA.Y))
		h.WriteFloat32(float32(jt.AnchorB.X))
		h.WriteFloat32(float32(jt.AnchorB.Y))
		h.WriteFloat32(jt.ReferenceAngle)
		h.WriteBool(jt.EnableLimit)
		h.WriteFloat32(jt.LowerAngle)
		h.WriteFloat32(jt.UpperAngle)
		h.WriteBool(jt.EnableMotor)
		h.WriteFloat32(jt.MotorSpeed)
		h.WriteFloat32(jt.MaxMotorTorque)
		h.WriteFloat32(jt.BreakForce)
		h.WriteFloat32(jt.BreakTorque)
		h.WriteBool(jt.CollideConnected)
		hashJointReaction(h, jt.jointReaction)
		h.WriteBool(jt.active)
		h.WriteFloat64(jt.dt)
		h.WriteFloat64(jt.rAx)
		h.WriteFloat64(jt.rAy)
		h.WriteFloat64(jt.rBx)
		h.WriteFloat64(jt.rBy)
		h.WriteFloat64(jt.m11)
		h.WriteFloat64(jt.m12)
		h.WriteFloat64(jt.m22)
		h.WriteFloat64(jt.motorMass)
		h.WriteFloat64(jt.motorImpulse)
		h.WriteFloat64(jt.limitImpulse)
		h.WriteInt(jt.limitState)
	case *PrismaticJoint:
		h.WriteUint8(4)
		hashBodyRef(jt.A)
		hashBodyRef(jt.B)
		h.WriteFloat32(float32(jt.AnchorA.X))
		h.WriteFloat32(float32(jt.AnchorA.Y))
		h.WriteFloat32(float32(jt.AnchorB.X))
		h.WriteFloat32(float32(jt.AnchorB.Y))
		h.WriteFloat32(float32(jt.LocalAxisA.X))
		h.WriteFloat32(float32(jt.LocalAxisA.Y))
		h.WriteFloat32(jt.ReferenceAngle)
		h.WriteBool(jt.EnableLimit)
		h.WriteFloat32(jt.LowerTranslation)
		h.WriteFloat32(jt.UpperTranslation)
		h.WriteBool(jt.EnableMotor)
		h.WriteFloat32(jt.MotorSpeed)
		h.WriteFloat32(jt.MaxMotorForce)
		h.WriteFloat32(jt.BreakForce)
		h.WriteFloat32(jt.BreakTorque)
		h.WriteBool(jt.CollideConnected)
		hashJointReaction(h, jt.jointReaction)
		h.WriteBool(jt.active)
		h.WriteBool(jt.rotLocked)
		h.WriteFloat64(jt.dt)
		h.WriteFloat64(jt.ax)
		h.WriteFloat64(jt.ay)
		h.WriteFloat64(jt.px)
		h.WriteFloat64(jt.py)
		h.WriteFloat64(jt.s1)
		h.WriteFloat64(jt.s2)
		h.WriteFloat64(jt.a1)
		h.WriteFloat64(jt.a2)
		h.WriteFloat64(jt.perpM11)
		h.WriteFloat64(jt.perpM12)
		h.WriteFloat64(jt.perpM22)
		h.WriteFloat64(jt.motorMass)
		h.WriteFloat64(jt.motorImpulse)
		h.WriteFloat64(jt.limitImpulse)
		h.WriteInt(jt.limitState)
	case *WeldJoint:
		h.WriteUint8(5)
		hashBodyRef(jt.A)
		hashBodyRef(jt.B)
		h.WriteFloat32(float32(jt.AnchorA.X))
		h.WriteFloat32(float32(jt.AnchorA.Y))
		h.WriteFloat32(float32(jt.AnchorB.X))
		h.WriteFloat32(float32(jt.AnchorB.Y))
		h.WriteFloat32(jt.ReferenceAngle)
		h.WriteFloat32(jt.FrequencyHz)
		h.WriteFloat32(jt.DampingRatio)
		h.WriteFloat32(jt.Stiffness)
		h.WriteFloat32(jt.Damping)
		h.WriteFloat32(jt.BreakForce)
		h.WriteFloat32(jt.BreakTorque)
		h.WriteBool(jt.CollideConnected)
		hashJointReaction(h, jt.jointReaction)
		h.WriteBool(jt.active)
		h.WriteBool(jt.soft)
		h.WriteFloat64(jt.dt)
		h.WriteFloat64(jt.rAx)
		h.WriteFloat64(jt.rAy)
		h.WriteFloat64(jt.rBx)
		h.WriteFloat64(jt.rBy)
		h.WriteFloat64(jt.k.a)
		h.WriteFloat64(jt.k.b)
		h.WriteFloat64(jt.k.c)
		h.WriteFloat64(jt.k.d)
		h.WriteFloat64(jt.k.e)
		h.WriteFloat64(jt.k.f)
		h.WriteFloat64(jt.gammaLin)
		h.WriteFloat64(jt.gammaAng)
		h.WriteFloat64(jt.biasX)
		h.WriteFloat64(jt.biasY)
		h.WriteFloat64(jt.biasAng)
		h.WriteFloat64(jt.impulseX)
		h.WriteFloat64(jt.impulseY)
		h.WriteFloat64(jt.angImpulse)
	case *WheelJoint:
		h.WriteUint8(6)
		hashBodyRef(jt.A)
		hashBodyRef(jt.B)
		h.WriteFloat32(float32(jt.AnchorA.X))
		h.WriteFloat32(float32(jt.AnchorA.Y))
		h.WriteFloat32(float32(jt.AnchorB.X))
		h.WriteFloat32(float32(jt.AnchorB.Y))
		h.WriteFloat32(float32(jt.LocalAxisA.X))
		h.WriteFloat32(float32(jt.LocalAxisA.Y))
		h.WriteFloat32(jt.FrequencyHz)
		h.WriteFloat32(jt.DampingRatio)
		h.WriteFloat32(jt.Stiffness)
		h.WriteFloat32(jt.Damping)
		h.WriteBool(jt.EnableLimit)
		h.WriteFloat32(jt.LowerTranslation)
		h.WriteFloat32(jt.UpperTranslation)
		h.WriteBool(jt.EnableMotor)
		h.WriteFloat32(jt.MotorSpeed)
		h.WriteFloat32(jt.MaxMotorTorque)
		h.WriteFloat32(jt.BreakForce)
		h.WriteFloat32(jt.BreakTorque)
		h.WriteBool(jt.CollideConnected)
		hashJointReaction(h, jt.jointReaction)
		h.WriteBool(jt.active)
		h.WriteBool(jt.springActive)
		h.WriteFloat64(jt.dt)
		h.WriteFloat64(jt.ax)
		h.WriteFloat64(jt.ay)
		h.WriteFloat64(jt.px)
		h.WriteFloat64(jt.py)
		h.WriteFloat64(jt.sAx)
		h.WriteFloat64(jt.sBx)
		h.WriteFloat64(jt.a1)
		h.WriteFloat64(jt.a2)
		h.WriteFloat64(jt.perpMass)
		h.WriteFloat64(jt.axisMass)
		h.WriteFloat64(jt.springMass)
		h.WriteFloat64(jt.motorMass)
		h.WriteFloat64(jt.gamma)
		h.WriteFloat64(jt.bias)
		h.WriteFloat64(jt.springImpulse)
		h.WriteFloat64(jt.motorImpulse)
		h.WriteFloat64(jt.limitImpulse)
		h.WriteInt(jt.limitState)
	case *MouseJoint:
		h.WriteUint8(7)
		hashBodyRef(jt.Body)
		h.WriteFloat32(float32(jt.Anchor.X))
		h.WriteFloat32(float32(jt.Anchor.Y))
		h.WriteFloat32(float32(jt.Target.X))
		h.WriteFloat32(float32(jt.Target.Y))
		h.WriteFloat32(jt.MaxForce)
		h.WriteFloat32(jt.FrequencyHz)
		h.WriteFloat32(jt.DampingRatio)
		h.WriteFloat32(jt.Stiffness)
		h.WriteFloat32(jt.Damping)
		h.WriteFloat32(jt.BreakForce)
		h.WriteFloat32(jt.BreakTorque)
		hashJointReaction(h, jt.jointReaction)
		h.WriteBool(jt.active)
		h.WriteFloat64(jt.dt)
		h.WriteFloat64(jt.rX)
		h.WriteFloat64(jt.rY)
		h.WriteFloat64(jt.m11)
		h.WriteFloat64(jt.m12)
		h.WriteFloat64(jt.m22)
		h.WriteFloat64(jt.gamma)
		h.WriteFloat64(jt.biasX)
		h.WriteFloat64(jt.biasY)
		h.WriteFloat64(jt.impulseX)
		h.WriteFloat64(jt.impulseY)
	}
}

func hashShape(h *detmath.Hasher, s Shape) {
	h.WriteUint8(uint8(s.Kind))
	switch s.Kind {
	case ShapeBox:
		h.WriteFloat32(s.Box.X)
		h.WriteFloat32(s.Box.Y)
		h.WriteFloat32(s.Box.W)
		h.WriteFloat32(s.Box.H)
	case ShapeCircle:
		h.WriteFloat32(s.Circle.X)
		h.WriteFloat32(s.Circle.Y)
		h.WriteFloat32(s.Circle.R)
	case ShapePolygon:
		h.WriteInt(s.Polygon.Count)
		for _, v := range s.Polygon.Vertices {
			h.WriteFloat32(float32(v.X))
			h.WriteFloat32(float32(v.Y))
		}
		h.WriteFloat32(s.Polygon.Radius)
	case ShapeCapsule:
		h.WriteFloat32(s.Capsule.X1)
		h.WriteFloat32(s.Capsule.Y1)
		h.WriteFloat32(s.Capsule.X2)
		h.WriteFloat32(s.Capsule.Y2)
		h.WriteFloat32(s.Capsule.R)
	case ShapeSegment:
		h.WriteFloat32(s.Segment.X1)
		h.WriteFloat32(s.Segment.Y1)
		h.WriteFloat32(s.Segment.X2)
		h.WriteFloat32(s.Segment.Y2)
	case ShapeChain:
		if s.Chain == nil {
			h.WriteBool(false)
			return
		}
		h.WriteBool(true)
		h.WriteBool(s.Chain.Loop)
		h.WriteInt(len(s.Chain.Points))
		for _, p := range s.Chain.Points {
			h.WriteFloat32(float32(p.X))
			h.WriteFloat32(float32(p.Y))
		}
	}
}

func hashArea(h *detmath.Hasher, a AreaSettings) {
	h.WriteInt(a.Priority)
	h.WriteUint8(uint8(a.GravityMode))
	h.WriteFloat32(a.GravityX)
	h.WriteFloat32(a.GravityY)
	h.WriteBool(a.PointGravity)
	h.WriteFloat32(a.GravityPointX)
	h.WriteFloat32(a.GravityPointY)
	h.WriteFloat32(a.GravityStrength)
	h.WriteUint8(uint8(a.GravityFalloff))
	h.WriteFloat32(a.GravityUnitDistance)
	h.WriteUint8(uint8(a.LinearDampingMode))
	h.WriteFloat32(a.LinearDamping)
	h.WriteUint8(uint8(a.AngularDampingMode))
	h.WriteFloat32(a.AngularDamping)
	h.WriteFloat32(a.FluidDensity)
	h.WriteFloat32(a.FluidLinearDrag)
	h.WriteFloat32(a.FluidAngularDrag)
	h.WriteFloat32(a.FluidVelX)
	h.WriteFloat32(a.FluidVelY)
}

func hashBody(h *detmath.Hasher, b *Body) {
	h.WriteInt(b.ID)
	h.WriteString(b.Tag)
	hashShape(h, b.Shape)
	h.WriteFloat32(b.VX)
	h.WriteFloat32(b.VY)
	h.WriteUint32(uint32(b.Layer))
	h.WriteUint32(uint32(b.Mask))
	h.WriteBool(b.Grounded)
	h.WriteBool(b.OnCeiling)
	h.WriteBool(b.OnLeftWall)
	h.WriteBool(b.OnRightWall)
	h.WriteBool(b.OneWayDrop)
	// Hash OneWay only when set so worlds without one-way bodies keep their existing checksums.
	if b.OneWay {
		h.WriteBool(true)
	}
}

func hashTrigger(h *detmath.Hasher, tr *Trigger) {
	h.WriteInt(tr.ID)
	h.WriteString(tr.Tag)
	hashShape(h, tr.Shape)
	h.WriteUint32(uint32(tr.Layer))
	h.WriteUint32(uint32(tr.Mask))
	hashArea(h, tr.Area)
}

func hashRigidBody(h *detmath.Hasher, b *RigidBody) {
	h.WriteInt(b.ID)
	h.WriteString(b.Tag)
	hashShape(h, b.Shape)
	h.WriteFloat32(b.X)
	h.WriteFloat32(b.Y)
	h.WriteFloat32(b.VX)
	h.WriteFloat32(b.VY)
	h.WriteFloat32(b.Angle)
	h.WriteFloat32(b.AngularVel)
	h.WriteFloat32(b.Mass)
	h.WriteFloat32(b.InvMass)
	h.WriteFloat32(b.Inertia)
	h.WriteFloat32(b.InvInertia)
	h.WriteFloat32(b.Restitution)
	h.WriteFloat32(b.Friction)
	h.WriteFloat32(b.LinearDamping)
	h.WriteFloat32(b.AngularDamping)
	h.WriteFloat32(b.GravityScale)
	h.WriteBool(b.gravityScaleSet)
	h.WriteBool(b.Static)
	h.WriteUint32(uint32(b.Layer))
	h.WriteUint32(uint32(b.Mask))
	h.WriteFloat64(b.forceX)
	h.WriteFloat64(b.forceY)
	h.WriteFloat64(b.torque)
	h.WriteBool(b.Bullet)
	h.WriteBool(b.sleeping)
	h.WriteFloat64(b.sleepTime)
	if b.sleeping {
		hashAreaEffect(h, b.sleepEnv)
	}
	h.WriteBool(b.restValid)
	if b.restValid {
		h.WriteFloat32(b.restX)
		h.WriteFloat32(b.restY)
		h.WriteFloat32(b.restAngle)
	}
	// Kinematic and sensor state is hashed only when set, so other bodies keep the digest they had before it existed.
	if b.Kinematic || b.Sensor || b.targetPending || b.targetDriven {
		h.WriteString("rigid.kinematic-sensor")
		h.WriteBool(b.Kinematic)
		h.WriteBool(b.Sensor)
		h.WriteBool(b.targetPending)
		if b.targetPending {
			h.WriteFloat32(b.target.x)
			h.WriteFloat32(b.target.y)
			h.WriteFloat32(b.target.angle)
		}
		h.WriteBool(b.targetDriven)
		if b.targetDriven {
			h.WriteFloat32(b.targetVX)
			h.WriteFloat32(b.targetVY)
			h.WriteFloat32(b.targetAV)
		}
	}
	// Shape-derived state is hashed only when it carries information beyond the fields above, so Box and Circle bodies with derived inertia keep the digest they had before free shapes existed.
	if !b.hasLocal && b.derivedInertia == b.Inertia {
		return
	}
	h.WriteBool(true)
	h.WriteFloat32(b.derivedInertia)
	h.WriteBool(b.hasLocal)
	if b.hasLocal {
		hashShape(h, b.local)
		synced := b.Shape == b.synced
		h.WriteBool(synced)
		if synced {
			h.WriteFloat32(b.syncX)
			h.WriteFloat32(b.syncY)
			h.WriteFloat32(b.syncAngle)
		}
	}
}

func hashAreaEffect(h *detmath.Hasher, e AreaEffect) {
	h.WriteFloat32(e.GravityX)
	h.WriteFloat32(e.GravityY)
	h.WriteFloat32(e.LinearDamping)
	h.WriteFloat32(e.AngularDamping)
	h.WriteBool(e.HasLinearDamping)
	h.WriteBool(e.HasAngularDamping)
	h.WriteFloat32(e.SubmergedFraction)
	h.WriteFloat32(e.BuoyancyAccelX)
	h.WriteFloat32(e.BuoyancyAccelY)
	h.WriteFloat32(e.FluidLinearDrag)
	h.WriteFloat32(e.FluidAngularDrag)
	h.WriteFloat32(e.FluidVelX)
	h.WriteFloat32(e.FluidVelY)
}

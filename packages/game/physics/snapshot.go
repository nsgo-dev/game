package physics

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

// SnapshotVersion is the schema version stored in WorldSnapshot and RigidWorldSnapshot.
const SnapshotVersion = 1

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
	Cells      []TileFlags `json:"cells,omitempty"`
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
}

// RigidJointSnapshot captures the complete public and private solver state of one RigidWorld Joint.
type RigidJointSnapshot struct {
	Kind        string             `json:"kind"` // "distance", "spring", "revolute"
	BodyAIndex  int                `json:"bodyAIndex"`
	BodyBIndex  int                `json:"bodyBIndex"`
	DetachedA   *RigidBodySnapshot `json:"detachedA,omitempty"`
	DetachedB   *RigidBodySnapshot `json:"detachedB,omitempty"`
	AnchorAX    float32            `json:"anchorAX"`
	AnchorAY    float32            `json:"anchorAY"`
	AnchorBX    float32            `json:"anchorBX"`
	AnchorBY    float32            `json:"anchorBY"`
	Length      float32            `json:"length,omitempty"`
	RestLength  float32            `json:"restLength,omitempty"`
	Stiffness   float32            `json:"stiffness,omitempty"`
	Compliance  float32            `json:"compliance,omitempty"`
	Damping     float32            `json:"damping,omitempty"`
	Initialized bool               `json:"initialized,omitempty"`
	Active      bool               `json:"active,omitempty"`
	Dt          float64            `json:"dt,omitempty"`
	UX          float64            `json:"ux,omitempty"`
	UY          float64            `json:"uy,omitempty"`
	RAX         float64            `json:"rAx,omitempty"`
	RAY         float64            `json:"rAy,omitempty"`
	RBX         float64            `json:"rBx,omitempty"`
	RBY         float64            `json:"rBy,omitempty"`
	EffMass     float64            `json:"effMass,omitempty"`
	M11         float64            `json:"m11,omitempty"`
	M12         float64            `json:"m12,omitempty"`
	M22         float64            `json:"m22,omitempty"`
}

// RigidWorldSnapshot captures the complete deterministic state of a RigidWorld,
// including all rigid bodies (and pending forces/torques), joints and their
// private constraint state, area triggers, gravity, and solver configuration.
// It marshals cleanly with encoding/json.
type RigidWorldSnapshot struct {
	Version          int                  `json:"version"`
	GravityX         float32              `json:"gravityX"`
	GravityY         float32              `json:"gravityY"`
	SolverIterations int                  `json:"solverIterations"`
	Bodies           []RigidBodySnapshot  `json:"bodies,omitempty"`
	Joints           []RigidJointSnapshot `json:"joints,omitempty"`
	Triggers         []TriggerSnapshot    `json:"triggers,omitempty"`
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

	if g, ok := w.Tiles.(*Grid); ok && g != nil {
		cells := make([]TileFlags, len(g.Cells))
		copy(cells, g.Cells)
		snap.Grid = &GridSnapshot{
			Cols:       g.Cols,
			Rows:       g.Rows,
			TileWidth:  g.TileWidth,
			TileHeight: g.TileHeight,
			Cells:      cells,
		}
	}

	return snap
}

// Restore replaces w's state from snap. Existing *Body and *Trigger pointers
// are reused in place (matching by non-zero ID first, then by slot index) so
// caller references, UserData, and Trigger callbacks remain intact.
func (w *World) Restore(snap WorldSnapshot) error {
	if w == nil {
		return errors.New("physics: cannot restore nil World")
	}
	if snap.Version != SnapshotVersion {
		return fmt.Errorf("physics: unsupported WorldSnapshot version %d (want %d)", snap.Version, SnapshotVersion)
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
		cells := make([]TileFlags, len(snap.Grid.Cells))
		copy(cells, snap.Grid.Cells)
		if g, ok := w.Tiles.(*Grid); ok && g != nil {
			g.Cols = snap.Grid.Cols
			g.Rows = snap.Grid.Rows
			g.TileWidth = snap.Grid.TileWidth
			g.TileHeight = snap.Grid.TileHeight
			g.Cells = cells
		} else {
			w.Tiles = &Grid{
				Cols:       snap.Grid.Cols,
				Rows:       snap.Grid.Rows,
				TileWidth:  snap.Grid.TileWidth,
				TileHeight: snap.Grid.TileHeight,
				Cells:      cells,
			}
		}
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
		Version:          SnapshotVersion,
		GravityX:         w.GravityX,
		GravityY:         w.GravityY,
		SolverIterations: w.SolverIterations,
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
		switch jt := j.(type) {
		case *DistanceJoint:
			idxA, detA := resolveBodyRef(jt.A)
			idxB, detB := resolveBodyRef(jt.B)
			snap.Joints = append(snap.Joints, RigidJointSnapshot{
				Kind:        "distance",
				BodyAIndex:  idxA,
				BodyBIndex:  idxB,
				DetachedA:   detA,
				DetachedB:   detB,
				AnchorAX:    float32(jt.AnchorA.X),
				AnchorAY:    float32(jt.AnchorA.Y),
				AnchorBX:    float32(jt.AnchorB.X),
				AnchorBY:    float32(jt.AnchorB.Y),
				Length:      jt.Length,
				Stiffness:   jt.Stiffness,
				Compliance:  jt.Compliance,
				Damping:     jt.Damping,
				Initialized: jt.initialized,
				Active:      jt.active,
				Dt:          jt.dt,
				UX:          jt.ux,
				UY:          jt.uy,
				RAX:         jt.rAx,
				RAY:         jt.rAy,
				RBX:         jt.rBx,
				RBY:         jt.rBy,
				EffMass:     jt.effMass,
			})
		case *SpringJoint:
			idxA, detA := resolveBodyRef(jt.A)
			idxB, detB := resolveBodyRef(jt.B)
			snap.Joints = append(snap.Joints, RigidJointSnapshot{
				Kind:        "spring",
				BodyAIndex:  idxA,
				BodyBIndex:  idxB,
				DetachedA:   detA,
				DetachedB:   detB,
				AnchorAX:    float32(jt.AnchorA.X),
				AnchorAY:    float32(jt.AnchorA.Y),
				AnchorBX:    float32(jt.AnchorB.X),
				AnchorBY:    float32(jt.AnchorB.Y),
				RestLength:  jt.RestLength,
				Stiffness:   jt.Stiffness,
				Damping:     jt.Damping,
				Initialized: jt.initialized,
			})
		case *RevoluteJoint:
			idxA, detA := resolveBodyRef(jt.A)
			idxB, detB := resolveBodyRef(jt.B)
			snap.Joints = append(snap.Joints, RigidJointSnapshot{
				Kind:       "revolute",
				BodyAIndex: idxA,
				BodyBIndex: idxB,
				DetachedA:  detA,
				DetachedB:  detB,
				AnchorAX:   float32(jt.AnchorA.X),
				AnchorAY:   float32(jt.AnchorA.Y),
				AnchorBX:   float32(jt.AnchorB.X),
				AnchorBY:   float32(jt.AnchorB.Y),
				Active:     jt.active,
				RAX:        jt.rAx,
				RAY:        jt.rAy,
				RBX:        jt.rBx,
				RBY:        jt.rBy,
				M11:        jt.m11,
				M12:        jt.m12,
				M22:        jt.m22,
			})
		}
	}

	for _, tr := range w.Triggers {
		if tr != nil {
			snap.Triggers = append(snap.Triggers, snapshotTrigger(tr))
		}
	}
	return snap
}

// Restore replaces w's state from snap. Existing *RigidBody, Joint, and
// *Trigger pointers are reused in place when possible so caller handles,
// UserData, and callbacks stay valid across rollback restores.
func (w *RigidWorld) Restore(snap RigidWorldSnapshot) error {
	if w == nil {
		return errors.New("physics: cannot restore nil RigidWorld")
	}
	if snap.Version != SnapshotVersion {
		return fmt.Errorf("physics: unsupported RigidWorldSnapshot version %d (want %d)", snap.Version, SnapshotVersion)
	}
	for i, js := range snap.Joints {
		switch js.Kind {
		case "distance", "spring", "revolute":
		default:
			return fmt.Errorf("physics: joints[%d] has unknown kind %q", i, js.Kind)
		}
		if js.BodyAIndex < -1 || js.BodyAIndex >= len(snap.Bodies) {
			return fmt.Errorf("physics: joints[%d] has out-of-range bodyA index %d", i, js.BodyAIndex)
		}
		if js.BodyBIndex < -1 || js.BodyBIndex >= len(snap.Bodies) {
			return fmt.Errorf("physics: joints[%d] has out-of-range bodyB index %d", i, js.BodyBIndex)
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
		bodyA := resolveRestoredBody(js.BodyAIndex, js.DetachedA)
		bodyB := resolveRestoredBody(js.BodyBIndex, js.DetachedB)
		anchorA := core.Point{X: core.Points(js.AnchorAX), Y: core.Points(js.AnchorAY)}
		anchorB := core.Point{X: core.Points(js.AnchorBX), Y: core.Points(js.AnchorBY)}

		switch js.Kind {
		case "distance":
			var dst *DistanceJoint
			if i < len(w.Joints) {
				dst, _ = w.Joints[i].(*DistanceJoint)
			}
			if dst == nil {
				dst = new(DistanceJoint)
			}
			*dst = DistanceJoint{
				A:           bodyA,
				B:           bodyB,
				AnchorA:     anchorA,
				AnchorB:     anchorB,
				Length:      js.Length,
				Stiffness:   js.Stiffness,
				Compliance:  js.Compliance,
				Damping:     js.Damping,
				initialized: js.Initialized,
				active:      js.Active,
				dt:          js.Dt,
				ux:          js.UX,
				uy:          js.UY,
				rAx:         js.RAX,
				rAy:         js.RAY,
				rBx:         js.RBX,
				rBy:         js.RBY,
				effMass:     js.EffMass,
			}
			restoredJoints[i] = dst
		case "spring":
			var dst *SpringJoint
			if i < len(w.Joints) {
				dst, _ = w.Joints[i].(*SpringJoint)
			}
			if dst == nil {
				dst = new(SpringJoint)
			}
			*dst = SpringJoint{
				A:           bodyA,
				B:           bodyB,
				AnchorA:     anchorA,
				AnchorB:     anchorB,
				RestLength:  js.RestLength,
				Stiffness:   js.Stiffness,
				Damping:     js.Damping,
				initialized: js.Initialized,
			}
			restoredJoints[i] = dst
		case "revolute":
			var dst *RevoluteJoint
			if i < len(w.Joints) {
				dst, _ = w.Joints[i].(*RevoluteJoint)
			}
			if dst == nil {
				dst = new(RevoluteJoint)
			}
			*dst = RevoluteJoint{
				A:       bodyA,
				B:       bodyB,
				AnchorA: anchorA,
				AnchorB: anchorB,
				active:  js.Active,
				rAx:     js.RAX,
				rAy:     js.RAY,
				rBx:     js.RBX,
				rBy:     js.RBY,
				m11:     js.M11,
				m12:     js.M12,
				m22:     js.M22,
			}
			restoredJoints[i] = dst
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

	w.GravityX = snap.GravityX
	w.GravityY = snap.GravityY
	w.SolverIterations = snap.SolverIterations
	w.Bodies = restoredBodies
	w.Joints = restoredJoints
	w.Triggers = restoredTriggers
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
			h.WriteFloat32(jt.Stiffness)
			h.WriteFloat32(jt.Compliance)
			h.WriteFloat32(jt.Damping)
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
			h.WriteBool(jt.initialized)
		case *RevoluteJoint:
			h.WriteUint8(3)
			hashBodyRef(jt.A)
			hashBodyRef(jt.B)
			h.WriteFloat32(float32(jt.AnchorA.X))
			h.WriteFloat32(float32(jt.AnchorA.Y))
			h.WriteFloat32(float32(jt.AnchorB.X))
			h.WriteFloat32(float32(jt.AnchorB.Y))
			h.WriteBool(jt.active)
			h.WriteFloat64(jt.rAx)
			h.WriteFloat64(jt.rAy)
			h.WriteFloat64(jt.rBx)
			h.WriteFloat64(jt.rBy)
			h.WriteFloat64(jt.m11)
			h.WriteFloat64(jt.m12)
			h.WriteFloat64(jt.m22)
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
		if tr != nil {
			hashTrigger(&h, tr)
		}
	}
	return h.Sum64()
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

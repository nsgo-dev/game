package physics3d

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/nsgo-dev/game/internal/detmath"
)

// SnapshotVersion is the schema version stored in WorldSnapshot.
const SnapshotVersion = 1

// BodySnapshot captures the complete public and private simulation state of a physics3d.Body.
type BodySnapshot struct {
	ID                 int              `json:"id"`
	Tag                string           `json:"tag,omitempty"`
	Shape              Shape            `json:"shape"`
	Pos                Vec3             `json:"pos"`
	Vel                Vec3             `json:"vel"`
	Rot                Quat             `json:"rot"`
	AngVel             Vec3             `json:"angVel"`
	Mass               float32          `json:"mass"`
	InvMass            float32          `json:"invMass"`
	InvInertia         float32          `json:"invInertia"`
	CenterOfMass       Vec3             `json:"centerOfMass"`
	InvInertiaTensor   SymMat3          `json:"invInertiaTensor"`
	CustomCenterOfMass bool             `json:"customCenterOfMass,omitempty"`
	CustomInertia      bool             `json:"customInertia,omitempty"`
	LockRotation       bool             `json:"lockRotation,omitempty"`
	Restitution        float32          `json:"restitution"`
	Friction           float32          `json:"friction"`
	LinearDamping      float32          `json:"linearDamping"`
	AngularDamping     float32          `json:"angularDamping"`
	Material           *PhysicsMaterial `json:"material,omitempty"`
	GravityScale       float32          `json:"gravityScale"`
	GravityScaleSet    bool             `json:"gravityScaleSet,omitempty"`
	Static             bool             `json:"static,omitempty"`
	Kinematic          bool             `json:"kinematic,omitempty"`
	Sleeping           bool             `json:"sleeping,omitempty"`
	Layer              LayerMask        `json:"layer"`
	Mask               LayerMask        `json:"mask"`
	IsTrigger          bool             `json:"isTrigger,omitempty"`
	Area               AreaSettings     `json:"area"`

	SleepTimer     float32 `json:"sleepTimer,omitempty"`
	ResidualDepth  float32 `json:"residualDepth,omitempty"`
	LastPos        Vec3    `json:"lastPos"`
	LastRot        Quat    `json:"lastRot"`
	HavePose       bool    `json:"havePose,omitempty"`
	RestingOnVoxel bool    `json:"restingOnVoxel,omitempty"`
}

// PairKeySnapshot is a JSON-serializable body ID pair.
type PairKeySnapshot struct {
	A int `json:"a"`
	B int `json:"b"`
}

// CachedPointSnapshot captures one warm-start contact point inside a manifold.
type CachedPointSnapshot struct {
	LocalA         Vec3    `json:"localA"`
	LocalB         Vec3    `json:"localB"`
	FeatureID      uint32  `json:"featureID,omitempty"`
	NormalImpulse  float32 `json:"normalImpulse"`
	TangentImpulse Vec3    `json:"tangentImpulse"`
}

// CachedImpulseSnapshot captures one warm-start contact manifold cache entry.
type CachedImpulseSnapshot struct {
	BodyAID        int                   `json:"bodyAID"`
	BodyBID        int                   `json:"bodyBID"`
	ChildA         int                   `json:"childA"`
	ChildB         int                   `json:"childB"`
	Normal         Vec3                  `json:"normal"`
	NormalImpulse  float32               `json:"normalImpulse,omitempty"`
	TangentImpulse Vec3                  `json:"tangentImpulse"`
	Points         []CachedPointSnapshot `json:"points,omitempty"`
}

// UnmarshalJSON defaults ChildA and ChildB to -1 when omitted in pre-compound v1 snapshots.
func (c *CachedImpulseSnapshot) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	type alias CachedImpulseSnapshot
	aux := alias{ChildA: -1, ChildB: -1}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&aux); err != nil {
		return err
	}
	*c = CachedImpulseSnapshot(aux)
	return nil
}

// ActiveContactSnapshot captures one active contact or trigger overlap record.
type ActiveContactSnapshot struct {
	KeyA        int     `json:"keyA"`
	KeyB        int     `json:"keyB"`
	KeyChildA   int     `json:"keyChildA"`
	KeyChildB   int     `json:"keyChildB"`
	BodyAID     int     `json:"bodyAID"`
	BodyBID     int     `json:"bodyBID"`
	ChildIndexA int     `json:"childIndexA"`
	ChildIndexB int     `json:"childIndexB"`
	Point       Vec3    `json:"point"`
	Normal      Vec3    `json:"normal"`
	Depth       float32 `json:"depth"`
	IsTrigger   bool    `json:"isTrigger,omitempty"`
}

// UnmarshalJSON defaults child indices to -1 when omitted in pre-compound v1 snapshots.
func (a *ActiveContactSnapshot) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	type alias ActiveContactSnapshot
	aux := alias{KeyChildA: -1, KeyChildB: -1, ChildIndexA: -1, ChildIndexB: -1}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&aux); err != nil {
		return err
	}
	*a = ActiveContactSnapshot(aux)
	return nil
}

// ContactEventSnapshot captures one ContactEvent produced by the last Step.
type ContactEventSnapshot struct {
	Kind        ContactEventKind `json:"kind"`
	BodyAID     int              `json:"bodyAID"`
	BodyBID     int              `json:"bodyBID"`
	ChildIndexA int              `json:"childIndexA"`
	ChildIndexB int              `json:"childIndexB"`
	Point       Vec3             `json:"point"`
	Normal      Vec3             `json:"normal"`
	Depth       float32          `json:"depth"`
	IsTrigger   bool             `json:"isTrigger,omitempty"`
}

// UnmarshalJSON defaults ChildIndexA and ChildIndexB to -1 when omitted in pre-compound v1 snapshots.
func (e *ContactEventSnapshot) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	type alias ContactEventSnapshot
	aux := alias{ChildIndexA: -1, ChildIndexB: -1}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&aux); err != nil {
		return err
	}
	*e = ContactEventSnapshot(aux)
	return nil
}

// JointReactionSnapshot captures the per-step reaction accumulator shared by built-in joints.
type JointReactionSnapshot struct {
	StepLinearImpulse  Vec3    `json:"stepLinearImpulse"`
	StepAngularImpulse Vec3    `json:"stepAngularImpulse"`
	LastDt             float32 `json:"lastDt,omitempty"`
	Broken             bool    `json:"broken,omitempty"`
}

// SixDOFAxisStateSnapshot captures private per-axis solver state of a SixDOFJoint.
type SixDOFAxisStateSnapshot struct {
	LimitState   int     `json:"limitState,omitempty"`
	LimitImpulse float32 `json:"limitImpulse,omitempty"`
	DriveImpulse float32 `json:"driveImpulse,omitempty"`
	DriveGamma   float32 `json:"driveGamma,omitempty"`
	DriveBias    float32 `json:"driveBias,omitempty"`
	DriveMode    int     `json:"driveMode,omitempty"`
	DriveVel     float32 `json:"driveVel,omitempty"`
}

// JointSnapshot captures the complete public and private solver state of any built-in Joint3D.
type JointSnapshot struct {
	Kind             string                `json:"kind"` // "distance", "ball", "hinge", "slider", "fixed", "swingtwist", "sixdof"
	BodyAID          int                   `json:"bodyAID"`
	BodyBID          int                   `json:"bodyBID"`
	LocalAnchorA     Vec3                  `json:"localAnchorA"`
	LocalAnchorB     Vec3                  `json:"localAnchorB"`
	BreakForce       float32               `json:"breakForce,omitempty"`
	BreakTorque      float32               `json:"breakTorque,omitempty"`
	CollideConnected bool                  `json:"collideConnected,omitempty"`
	Reaction         JointReactionSnapshot `json:"reaction"`

	// DistanceJoint fields
	MinDistance float32 `json:"minDistance,omitempty"`
	MaxDistance float32 `json:"maxDistance,omitempty"`
	Stiffness   float32 `json:"stiffness,omitempty"`
	Damping     float32 `json:"damping,omitempty"`
	AccImpulse  float32 `json:"accImpulse,omitempty"`
	Mode        int     `json:"mode,omitempty"`
	TargetDist  float32 `json:"targetDist,omitempty"`
	Gamma       float32 `json:"gamma,omitempty"`
	Bias        float32 `json:"bias,omitempty"`

	// BallJoint / HingeJoint / SliderJoint fields
	ConeLimit        float32 `json:"coneLimit,omitempty"`
	LocalAxisA       Vec3    `json:"localAxisA"`
	LocalAxisB       Vec3    `json:"localAxisB"`
	EnableLimits     bool    `json:"enableLimits,omitempty"`
	MinAngle         float32 `json:"minAngle,omitempty"`
	MaxAngle         float32 `json:"maxAngle,omitempty"`
	MinTranslation   float32 `json:"minTranslation,omitempty"`
	MaxTranslation   float32 `json:"maxTranslation,omitempty"`
	MotorTargetVel   float32 `json:"motorTargetVel,omitempty"`
	MotorMaxTorque   float32 `json:"motorMaxTorque,omitempty"`
	MotorMaxForce    float32 `json:"motorMaxForce,omitempty"`
	DriveTargetAngle float32 `json:"driveTargetAngle,omitempty"`
	DriveTargetPos   float32 `json:"driveTargetPos,omitempty"`
	DriveStiffness   float32 `json:"driveStiffness,omitempty"`
	DriveDamping     float32 `json:"driveDamping,omitempty"`
	DriveMaxTorque   float32 `json:"driveMaxTorque,omitempty"`
	DriveMaxForce    float32 `json:"driveMaxForce,omitempty"`

	// Shared private reference/impulse state
	RefInitialized bool    `json:"refInitialized,omitempty"`
	RefPerpA       Vec3    `json:"refPerpA"`
	RefPerpB       Vec3    `json:"refPerpB"`
	RefRelRot      Quat    `json:"refRelRot"`
	MotorImpulse   float32 `json:"motorImpulse,omitempty"`
	DriveImpulse   float32 `json:"driveImpulse,omitempty"`
	DriveGamma     float32 `json:"driveGamma,omitempty"`
	DriveBias      float32 `json:"driveBias,omitempty"`
	DriveActive    bool    `json:"driveActive,omitempty"`
	LimitImpulse   float32 `json:"limitImpulse,omitempty"`
	LimitState     int     `json:"limitState,omitempty"`
	Angle          float32 `json:"angle,omitempty"`
	SwingImpulse   float32 `json:"swingImpulse,omitempty"`

	// SwingTwistJoint fields
	TwistAxisA        Vec3    `json:"twistAxisA"`
	TwistAxisB        Vec3    `json:"twistAxisB"`
	PlaneAxisA        Vec3    `json:"planeAxisA"`
	PlaneAxisB        Vec3    `json:"planeAxisB"`
	EnableSwingLimits bool    `json:"enableSwingLimits,omitempty"`
	SwingMin1         float32 `json:"swingMin1,omitempty"`
	SwingMax1         float32 `json:"swingMax1,omitempty"`
	SwingMin2         float32 `json:"swingMin2,omitempty"`
	SwingMax2         float32 `json:"swingMax2,omitempty"`
	EnableTwistLimits bool    `json:"enableTwistLimits,omitempty"`
	MinTwist          float32 `json:"minTwist,omitempty"`
	MaxTwist          float32 `json:"maxTwist,omitempty"`
	TwistImpulse      float32 `json:"twistImpulse,omitempty"`
	TwistLimitState   int     `json:"twistLimitState,omitempty"`

	// SixDOFJoint fields
	LocalFrameA     Quat                       `json:"localFrameA"`
	LocalFrameB     Quat                       `json:"localFrameB"`
	Linear          [3]AxisConfig              `json:"linear"`
	Angular         [3]AxisConfig              `json:"angular"`
	LinState        [3]SixDOFAxisStateSnapshot `json:"linState"`
	AngState        [3]SixDOFAxisStateSnapshot `json:"angState"`
	SingleAxisAngle float32                    `json:"singleAxisAngle,omitempty"`
	SingleAxisIdx   int                        `json:"singleAxisIdx,omitempty"`
}

// JointBreakEventSnapshot captures one JointBreakEvent from the last Step.
type JointBreakEventSnapshot struct {
	Joint   *JointSnapshot `json:"joint,omitempty"`
	BodyAID int            `json:"bodyAID"`
	BodyBID int            `json:"bodyBID"`
	Force   Vec3           `json:"force"`
	Torque  Vec3           `json:"torque"`
}

// TriMeshSnapshot captures a static TriMeshCollider.
type TriMeshSnapshot struct {
	ID          int        `json:"id"`
	Tag         string     `json:"tag,omitempty"`
	Layer       LayerMask  `json:"layer"`
	Mask        LayerMask  `json:"mask"`
	Friction    float32    `json:"friction"`
	Restitution float32    `json:"restitution"`
	Triangles   []Triangle `json:"triangles,omitempty"`
}

// HeightfieldSnapshot captures a static HeightfieldCollider.
type HeightfieldSnapshot struct {
	ID          int       `json:"id"`
	Tag         string    `json:"tag,omitempty"`
	Layer       LayerMask `json:"layer"`
	Mask        LayerMask `json:"mask"`
	Friction    float32   `json:"friction"`
	Restitution float32   `json:"restitution"`
	Cols        int       `json:"cols"`
	Rows        int       `json:"rows"`
	CellSize    float32   `json:"cellSize"`
	Origin      Vec3      `json:"origin"`
	Heights     []float32 `json:"heights,omitempty"`
}

// VoxelColliderSnapshot captures the metadata of a VoxelCollider (its backing
// VoxelGrid interface is matched by ID on Restore).
type VoxelColliderSnapshot struct {
	ID          int       `json:"id"`
	Tag         string    `json:"tag,omitempty"`
	Layer       LayerMask `json:"layer"`
	Mask        LayerMask `json:"mask"`
	Friction    float32   `json:"friction"`
	Restitution float32   `json:"restitution"`
	Origin      Vec3      `json:"origin"`
}

// WorldSnapshot captures the complete deterministic state of a physics3d.World,
// including all bodies, sleep timers, previous poses, warm-start contact cache,
// active contacts, contact/joint-break events, ignored pairs, joints and their
// private solver state, and static colliders. It marshals cleanly with encoding/json.
type WorldSnapshot struct {
	Version          int                       `json:"version"`
	Gravity          Vec3                      `json:"gravity"`
	SolverIterations int                       `json:"solverIterations"`
	NextID           int                       `json:"nextID"`
	NextColliderID   int                       `json:"nextColliderID"`
	Bodies           []BodySnapshot            `json:"bodies,omitempty"`
	WarmStartCache   []CachedImpulseSnapshot   `json:"warmStartCache,omitempty"`
	ActiveContacts   []ActiveContactSnapshot   `json:"activeContacts,omitempty"`
	ContactEvents    []ContactEventSnapshot    `json:"contactEvents,omitempty"`
	JointBreakEvents []JointBreakEventSnapshot `json:"jointBreakEvents,omitempty"`
	IgnoredPairs     []PairKeySnapshot         `json:"ignoredPairs,omitempty"`
	Joints           []JointSnapshot           `json:"joints,omitempty"`
	TriMeshes        []TriMeshSnapshot         `json:"triMeshes,omitempty"`
	Heightfields     []HeightfieldSnapshot     `json:"heightfields,omitempty"`
	Voxels           []VoxelColliderSnapshot   `json:"voxels,omitempty"`
}

func bodyIDOrZero(b *Body) int {
	if b == nil {
		return 0
	}
	return b.ID
}

func snapshotBody3D(b *Body) BodySnapshot {
	var mat *PhysicsMaterial
	if b.Material != nil {
		m := *b.Material
		mat = &m
	}
	return BodySnapshot{
		ID:                 b.ID,
		Tag:                b.Tag,
		Shape:              cloneShape(b.Shape),
		Pos:                b.Pos,
		Vel:                b.Vel,
		Rot:                b.Rot,
		AngVel:             b.AngVel,
		Mass:               b.Mass,
		InvMass:            b.InvMass,
		InvInertia:         b.InvInertia,
		CenterOfMass:       b.CenterOfMass,
		InvInertiaTensor:   b.InvInertiaTensor,
		CustomCenterOfMass: b.customCenterOfMass,
		CustomInertia:      b.customInertia,
		LockRotation:       b.LockRotation,
		Restitution:        b.Restitution,
		Friction:           b.Friction,
		LinearDamping:      b.LinearDamping,
		AngularDamping:     b.AngularDamping,
		Material:           mat,
		GravityScale:       b.GravityScale,
		GravityScaleSet:    b.gravityScaleSet,
		Static:             b.Static,
		Kinematic:          b.Kinematic,
		Sleeping:           b.Sleeping,
		Layer:              b.Layer,
		Mask:               b.Mask,
		IsTrigger:          b.IsTrigger,
		Area:               b.Area,
		SleepTimer:         b.sleepTimer,
		ResidualDepth:      b.residualDepth,
		LastPos:            b.lastPos,
		LastRot:            b.lastRot,
		HavePose:           b.havePose,
		RestingOnVoxel:     b.restingOnVoxel,
	}
}

func applyBody3DSnapshot(dst *Body, src BodySnapshot) {
	var mat *PhysicsMaterial
	if src.Material != nil {
		m := *src.Material
		mat = &m
	}
	*dst = Body{
		ID:                 src.ID,
		Tag:                src.Tag,
		Shape:              cloneShape(src.Shape),
		Pos:                src.Pos,
		Vel:                src.Vel,
		Rot:                src.Rot,
		AngVel:             src.AngVel,
		Mass:               src.Mass,
		InvMass:            src.InvMass,
		InvInertia:         src.InvInertia,
		CenterOfMass:       src.CenterOfMass,
		InvInertiaTensor:   src.InvInertiaTensor,
		LockRotation:       src.LockRotation,
		Restitution:        src.Restitution,
		Friction:           src.Friction,
		LinearDamping:      src.LinearDamping,
		AngularDamping:     src.AngularDamping,
		Material:           mat,
		GravityScale:       src.GravityScale,
		Static:             src.Static,
		Kinematic:          src.Kinematic,
		Sleeping:           src.Sleeping,
		Layer:              src.Layer,
		Mask:               src.Mask,
		IsTrigger:          src.IsTrigger,
		Area:               src.Area,
		gravityScaleSet:    src.GravityScaleSet,
		customCenterOfMass: src.CustomCenterOfMass,
		customInertia:      src.CustomInertia,
		sleepTimer:         src.SleepTimer,
		residualDepth:      src.ResidualDepth,
		lastPos:            src.LastPos,
		lastRot:            src.LastRot,
		havePose:           src.HavePose,
		restingOnVoxel:     src.RestingOnVoxel,
	}
}

func snapshotReaction(r jointReaction) JointReactionSnapshot {
	return JointReactionSnapshot{
		StepLinearImpulse:  r.stepLinearImpulse,
		StepAngularImpulse: r.stepAngularImpulse,
		LastDt:             r.lastDt,
		Broken:             r.broken,
	}
}

func restoreReaction(s JointReactionSnapshot) jointReaction {
	return jointReaction{
		stepLinearImpulse:  s.StepLinearImpulse,
		stepAngularImpulse: s.StepAngularImpulse,
		lastDt:             s.LastDt,
		broken:             s.Broken,
	}
}

func snapshotAxisStates(src [3]sixDOFAxisState) [3]SixDOFAxisStateSnapshot {
	var out [3]SixDOFAxisStateSnapshot
	for i := range 3 {
		out[i] = SixDOFAxisStateSnapshot{
			LimitState:   src[i].limitState,
			LimitImpulse: src[i].limitImpulse,
			DriveImpulse: src[i].driveImpulse,
			DriveGamma:   src[i].driveGamma,
			DriveBias:    src[i].driveBias,
			DriveMode:    src[i].driveMode,
			DriveVel:     src[i].driveVel,
		}
	}
	return out
}

func restoreAxisStates(src [3]SixDOFAxisStateSnapshot) [3]sixDOFAxisState {
	var out [3]sixDOFAxisState
	for i := range 3 {
		out[i] = sixDOFAxisState{
			limitState:   src[i].LimitState,
			limitImpulse: src[i].LimitImpulse,
			driveImpulse: src[i].DriveImpulse,
			driveGamma:   src[i].DriveGamma,
			driveBias:    src[i].DriveBias,
			driveMode:    src[i].DriveMode,
			driveVel:     src[i].DriveVel,
		}
	}
	return out
}

func snapshotJoint3D(j Joint3D) (JointSnapshot, bool) {
	switch jt := j.(type) {
	case *DistanceJoint:
		return JointSnapshot{
			Kind:             "distance",
			BodyAID:          bodyIDOrZero(jt.BodyA),
			BodyBID:          bodyIDOrZero(jt.BodyB),
			LocalAnchorA:     jt.LocalAnchorA,
			LocalAnchorB:     jt.LocalAnchorB,
			MinDistance:      jt.MinDistance,
			MaxDistance:      jt.MaxDistance,
			Stiffness:        jt.Stiffness,
			Damping:          jt.Damping,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Reaction:         snapshotReaction(jt.jointReaction),
			AccImpulse:       jt.accImpulse,
			Mode:             jt.mode,
			TargetDist:       jt.targetDist,
			Gamma:            jt.gamma,
			Bias:             jt.bias,
		}, true
	case *BallJoint:
		return JointSnapshot{
			Kind:             "ball",
			BodyAID:          bodyIDOrZero(jt.BodyA),
			BodyBID:          bodyIDOrZero(jt.BodyB),
			LocalAnchorA:     jt.LocalAnchorA,
			LocalAnchorB:     jt.LocalAnchorB,
			ConeLimit:        jt.ConeLimit,
			LocalAxisA:       jt.LocalAxisA,
			LocalAxisB:       jt.LocalAxisB,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Reaction:         snapshotReaction(jt.jointReaction),
			SwingImpulse:     jt.swingImpulse,
		}, true
	case *HingeJoint:
		return JointSnapshot{
			Kind:             "hinge",
			BodyAID:          bodyIDOrZero(jt.BodyA),
			BodyBID:          bodyIDOrZero(jt.BodyB),
			LocalAnchorA:     jt.LocalAnchorA,
			LocalAnchorB:     jt.LocalAnchorB,
			LocalAxisA:       jt.LocalAxisA,
			LocalAxisB:       jt.LocalAxisB,
			EnableLimits:     jt.EnableLimits,
			MinAngle:         jt.MinAngle,
			MaxAngle:         jt.MaxAngle,
			MotorTargetVel:   jt.MotorTargetVel,
			MotorMaxTorque:   jt.MotorMaxTorque,
			DriveTargetAngle: jt.DriveTargetAngle,
			DriveStiffness:   jt.DriveStiffness,
			DriveDamping:     jt.DriveDamping,
			DriveMaxTorque:   jt.DriveMaxTorque,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Reaction:         snapshotReaction(jt.jointReaction),
			RefInitialized:   jt.refInitialized,
			RefPerpA:         jt.refPerpA,
			RefPerpB:         jt.refPerpB,
			MotorImpulse:     jt.motorImpulse,
			DriveImpulse:     jt.driveImpulse,
			DriveGamma:       jt.driveGamma,
			DriveBias:        jt.driveBias,
			DriveActive:      jt.driveActive,
			LimitImpulse:     jt.limitImpulse,
			LimitState:       jt.limitState,
			Angle:            jt.angle,
		}, true
	case *SliderJoint:
		return JointSnapshot{
			Kind:             "slider",
			BodyAID:          bodyIDOrZero(jt.BodyA),
			BodyBID:          bodyIDOrZero(jt.BodyB),
			LocalAnchorA:     jt.LocalAnchorA,
			LocalAnchorB:     jt.LocalAnchorB,
			LocalAxisA:       jt.LocalAxisA,
			EnableLimits:     jt.EnableLimits,
			MinTranslation:   jt.MinTranslation,
			MaxTranslation:   jt.MaxTranslation,
			MotorTargetVel:   jt.MotorTargetVel,
			MotorMaxForce:    jt.MotorMaxForce,
			DriveTargetPos:   jt.DriveTargetPos,
			DriveStiffness:   jt.DriveStiffness,
			DriveDamping:     jt.DriveDamping,
			DriveMaxForce:    jt.DriveMaxForce,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Reaction:         snapshotReaction(jt.jointReaction),
			RefRelRot:        jt.refRelRot,
			RefInitialized:   jt.refInitialized,
			MotorImpulse:     jt.motorImpulse,
			DriveImpulse:     jt.driveImpulse,
			DriveGamma:       jt.driveGamma,
			DriveBias:        jt.driveBias,
			DriveActive:      jt.driveActive,
			LimitImpulse:     jt.limitImpulse,
			LimitState:       jt.limitState,
		}, true
	case *FixedJoint:
		return JointSnapshot{
			Kind:             "fixed",
			BodyAID:          bodyIDOrZero(jt.BodyA),
			BodyBID:          bodyIDOrZero(jt.BodyB),
			LocalAnchorA:     jt.LocalAnchorA,
			LocalAnchorB:     jt.LocalAnchorB,
			RefRelRot:        jt.RefRelRot,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Reaction:         snapshotReaction(jt.jointReaction),
			RefInitialized:   jt.refInitialized,
		}, true
	case *SwingTwistJoint:
		return JointSnapshot{
			Kind:              "swingtwist",
			BodyAID:           bodyIDOrZero(jt.BodyA),
			BodyBID:           bodyIDOrZero(jt.BodyB),
			LocalAnchorA:      jt.LocalAnchorA,
			LocalAnchorB:      jt.LocalAnchorB,
			TwistAxisA:        jt.TwistAxisA,
			TwistAxisB:        jt.TwistAxisB,
			PlaneAxisA:        jt.PlaneAxisA,
			PlaneAxisB:        jt.PlaneAxisB,
			EnableSwingLimits: jt.EnableSwingLimits,
			SwingMin1:         jt.SwingMin1,
			SwingMax1:         jt.SwingMax1,
			SwingMin2:         jt.SwingMin2,
			SwingMax2:         jt.SwingMax2,
			EnableTwistLimits: jt.EnableTwistLimits,
			MinTwist:          jt.MinTwist,
			MaxTwist:          jt.MaxTwist,
			BreakForce:        jt.BreakForce,
			BreakTorque:       jt.BreakTorque,
			CollideConnected:  jt.CollideConnected,
			Reaction:          snapshotReaction(jt.jointReaction),
			RefInitialized:    jt.refInitialized,
			SwingImpulse:      jt.swingImpulse,
			TwistImpulse:      jt.twistImpulse,
			TwistLimitState:   jt.twistLimitState,
		}, true
	case *SixDOFJoint:
		return JointSnapshot{
			Kind:             "sixdof",
			BodyAID:          bodyIDOrZero(jt.BodyA),
			BodyBID:          bodyIDOrZero(jt.BodyB),
			LocalAnchorA:     jt.LocalAnchorA,
			LocalAnchorB:     jt.LocalAnchorB,
			LocalFrameA:      jt.LocalFrameA,
			LocalFrameB:      jt.LocalFrameB,
			Linear:           jt.Linear,
			Angular:          jt.Angular,
			BreakForce:       jt.BreakForce,
			BreakTorque:      jt.BreakTorque,
			CollideConnected: jt.CollideConnected,
			Reaction:         snapshotReaction(jt.jointReaction),
			RefInitialized:   jt.refInitialized,
			LinState:         snapshotAxisStates(jt.linState),
			AngState:         snapshotAxisStates(jt.angState),
			SingleAxisAngle:  jt.singleAxisAngle,
			SingleAxisIdx:    jt.singleAxisIdx,
		}, true
	default:
		return JointSnapshot{}, false
	}
}

func restoreJoint3D(js JointSnapshot, existing Joint3D, lookup func(int) *Body) (Joint3D, error) {
	bodyA := lookup(js.BodyAID)
	bodyB := lookup(js.BodyBID)
	switch js.Kind {
	case "distance":
		dst, _ := existing.(*DistanceJoint)
		if dst == nil {
			dst = new(DistanceJoint)
		}
		*dst = DistanceJoint{
			BodyA:            bodyA,
			BodyB:            bodyB,
			LocalAnchorA:     js.LocalAnchorA,
			LocalAnchorB:     js.LocalAnchorB,
			MinDistance:      js.MinDistance,
			MaxDistance:      js.MaxDistance,
			Stiffness:        js.Stiffness,
			Damping:          js.Damping,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    restoreReaction(js.Reaction),
			accImpulse:       js.AccImpulse,
			mode:             js.Mode,
			targetDist:       js.TargetDist,
			gamma:            js.Gamma,
			bias:             js.Bias,
		}
		return dst, nil
	case "ball":
		dst, _ := existing.(*BallJoint)
		if dst == nil {
			dst = new(BallJoint)
		}
		*dst = BallJoint{
			BodyA:            bodyA,
			BodyB:            bodyB,
			LocalAnchorA:     js.LocalAnchorA,
			LocalAnchorB:     js.LocalAnchorB,
			ConeLimit:        js.ConeLimit,
			LocalAxisA:       js.LocalAxisA,
			LocalAxisB:       js.LocalAxisB,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    restoreReaction(js.Reaction),
			swingImpulse:     js.SwingImpulse,
		}
		return dst, nil
	case "hinge":
		dst, _ := existing.(*HingeJoint)
		if dst == nil {
			dst = new(HingeJoint)
		}
		*dst = HingeJoint{
			BodyA:            bodyA,
			BodyB:            bodyB,
			LocalAnchorA:     js.LocalAnchorA,
			LocalAnchorB:     js.LocalAnchorB,
			LocalAxisA:       js.LocalAxisA,
			LocalAxisB:       js.LocalAxisB,
			EnableLimits:     js.EnableLimits,
			MinAngle:         js.MinAngle,
			MaxAngle:         js.MaxAngle,
			MotorTargetVel:   js.MotorTargetVel,
			MotorMaxTorque:   js.MotorMaxTorque,
			DriveTargetAngle: js.DriveTargetAngle,
			DriveStiffness:   js.DriveStiffness,
			DriveDamping:     js.DriveDamping,
			DriveMaxTorque:   js.DriveMaxTorque,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    restoreReaction(js.Reaction),
			refInitialized:   js.RefInitialized,
			refPerpA:         js.RefPerpA,
			refPerpB:         js.RefPerpB,
			motorImpulse:     js.MotorImpulse,
			driveImpulse:     js.DriveImpulse,
			driveGamma:       js.DriveGamma,
			driveBias:        js.DriveBias,
			driveActive:      js.DriveActive,
			limitImpulse:     js.LimitImpulse,
			limitState:       js.LimitState,
			angle:            js.Angle,
		}
		return dst, nil
	case "slider":
		dst, _ := existing.(*SliderJoint)
		if dst == nil {
			dst = new(SliderJoint)
		}
		*dst = SliderJoint{
			BodyA:            bodyA,
			BodyB:            bodyB,
			LocalAnchorA:     js.LocalAnchorA,
			LocalAnchorB:     js.LocalAnchorB,
			LocalAxisA:       js.LocalAxisA,
			EnableLimits:     js.EnableLimits,
			MinTranslation:   js.MinTranslation,
			MaxTranslation:   js.MaxTranslation,
			MotorTargetVel:   js.MotorTargetVel,
			MotorMaxForce:    js.MotorMaxForce,
			DriveTargetPos:   js.DriveTargetPos,
			DriveStiffness:   js.DriveStiffness,
			DriveDamping:     js.DriveDamping,
			DriveMaxForce:    js.DriveMaxForce,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    restoreReaction(js.Reaction),
			refRelRot:        js.RefRelRot,
			refInitialized:   js.RefInitialized,
			motorImpulse:     js.MotorImpulse,
			driveImpulse:     js.DriveImpulse,
			driveGamma:       js.DriveGamma,
			driveBias:        js.DriveBias,
			driveActive:      js.DriveActive,
			limitImpulse:     js.LimitImpulse,
			limitState:       js.LimitState,
		}
		return dst, nil
	case "fixed":
		dst, _ := existing.(*FixedJoint)
		if dst == nil {
			dst = new(FixedJoint)
		}
		*dst = FixedJoint{
			BodyA:            bodyA,
			BodyB:            bodyB,
			LocalAnchorA:     js.LocalAnchorA,
			LocalAnchorB:     js.LocalAnchorB,
			RefRelRot:        js.RefRelRot,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    restoreReaction(js.Reaction),
			refInitialized:   js.RefInitialized,
		}
		return dst, nil
	case "swingtwist":
		dst, _ := existing.(*SwingTwistJoint)
		if dst == nil {
			dst = new(SwingTwistJoint)
		}
		*dst = SwingTwistJoint{
			BodyA:             bodyA,
			BodyB:             bodyB,
			LocalAnchorA:      js.LocalAnchorA,
			LocalAnchorB:      js.LocalAnchorB,
			TwistAxisA:        js.TwistAxisA,
			TwistAxisB:        js.TwistAxisB,
			PlaneAxisA:        js.PlaneAxisA,
			PlaneAxisB:        js.PlaneAxisB,
			EnableSwingLimits: js.EnableSwingLimits,
			SwingMin1:         js.SwingMin1,
			SwingMax1:         js.SwingMax1,
			SwingMin2:         js.SwingMin2,
			SwingMax2:         js.SwingMax2,
			EnableTwistLimits: js.EnableTwistLimits,
			MinTwist:          js.MinTwist,
			MaxTwist:          js.MaxTwist,
			BreakForce:        js.BreakForce,
			BreakTorque:       js.BreakTorque,
			CollideConnected:  js.CollideConnected,
			jointReaction:     restoreReaction(js.Reaction),
			refInitialized:    js.RefInitialized,
			swingImpulse:      js.SwingImpulse,
			twistImpulse:      js.TwistImpulse,
			twistLimitState:   js.TwistLimitState,
		}
		return dst, nil
	case "sixdof":
		dst, _ := existing.(*SixDOFJoint)
		if dst == nil {
			dst = new(SixDOFJoint)
		}
		*dst = SixDOFJoint{
			BodyA:            bodyA,
			BodyB:            bodyB,
			LocalAnchorA:     js.LocalAnchorA,
			LocalAnchorB:     js.LocalAnchorB,
			LocalFrameA:      js.LocalFrameA,
			LocalFrameB:      js.LocalFrameB,
			Linear:           js.Linear,
			Angular:          js.Angular,
			BreakForce:       js.BreakForce,
			BreakTorque:      js.BreakTorque,
			CollideConnected: js.CollideConnected,
			jointReaction:    restoreReaction(js.Reaction),
			refInitialized:   js.RefInitialized,
			linState:         restoreAxisStates(js.LinState),
			angState:         restoreAxisStates(js.AngState),
			singleAxisAngle:  js.SingleAxisAngle,
			singleAxisIdx:    js.SingleAxisIdx,
		}
		return dst, nil
	default:
		return nil, fmt.Errorf("physics3d: unknown joint kind %q", js.Kind)
	}
}

func newTriMeshFromTriangles(tris []Triangle) *TriMeshCollider {
	copied := make([]Triangle, len(tris))
	copy(copied, tris)
	tm := &TriMeshCollider{
		Friction:  0.5,
		Triangles: copied,
	}
	if len(copied) == 0 {
		return tm
	}
	n := len(copied)
	triBounds := make([]Box, n)
	triCentroids := make([]Vec3, n)
	tm.triOrder = make([]int, n)
	for i, tri := range copied {
		triBounds[i] = tri.Bounds()
		triCentroids[i] = tri.Centroid()
		tm.triOrder[i] = i
	}
	tm.nodes = make([]bvhNode, 0, 2*n)
	tm.buildBVH(0, n, triBounds, triCentroids)
	tm.Bounds = tm.nodes[0].bounds
	return tm
}

// Save captures a deep, deterministic, JSON-serializable snapshot of w,
// including all private solver state (warm-start impulse cache, sleep timers,
// previous poses, active contact/trigger pairs, joint state, and colliders).
func (w *World) Save() WorldSnapshot {
	if w == nil {
		return WorldSnapshot{Version: SnapshotVersion}
	}
	snap := WorldSnapshot{
		Version:          SnapshotVersion,
		Gravity:          w.Gravity,
		SolverIterations: w.SolverIterations,
		NextID:           w.nextID,
		NextColliderID:   w.nextColliderID,
	}

	if len(w.bodies) > 0 {
		snap.Bodies = make([]BodySnapshot, 0, len(w.bodies))
		for _, b := range w.bodies {
			if b != nil {
				snap.Bodies = append(snap.Bodies, snapshotBody3D(b))
			}
		}
	}

	if len(w.cache) > 0 {
		keys := make([]contactKey, 0, len(w.cache))
		for k := range w.cache {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, compareContactKeys)
		snap.WarmStartCache = make([]CachedImpulseSnapshot, len(keys))
		for i, k := range keys {
			c := w.cache[k]
			pts := make([]CachedPointSnapshot, c.count)
			var totalN float32
			var totalT Vec3
			for p := 0; p < c.count; p++ {
				cp := c.points[p]
				pts[p] = CachedPointSnapshot{
					LocalA:         cp.localA,
					LocalB:         cp.localB,
					FeatureID:      cp.featureID,
					NormalImpulse:  cp.normalImpulse,
					TangentImpulse: cp.tangentImpulse,
				}
				totalN += cp.normalImpulse
				totalT = totalT.Add(cp.tangentImpulse)
			}
			snap.WarmStartCache[i] = CachedImpulseSnapshot{
				BodyAID:        k.a,
				BodyBID:        k.b,
				ChildA:         k.childA,
				ChildB:         k.childB,
				Normal:         c.normal,
				NormalImpulse:  totalN,
				TangentImpulse: totalT,
				Points:         pts,
			}
		}
	}

	if len(w.activeContacts) > 0 {
		keys := make([]contactKey, 0, len(w.activeContacts))
		for k := range w.activeContacts {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, compareContactKeys)
		snap.ActiveContacts = make([]ActiveContactSnapshot, len(keys))
		for i, k := range keys {
			rec := w.activeContacts[k]
			snap.ActiveContacts[i] = ActiveContactSnapshot{
				KeyA:        k.a,
				KeyB:        k.b,
				KeyChildA:   k.childA,
				KeyChildB:   k.childB,
				BodyAID:     bodyIDOrZero(rec.a),
				BodyBID:     bodyIDOrZero(rec.b),
				ChildIndexA: rec.childA,
				ChildIndexB: rec.childB,
				Point:       rec.point,
				Normal:      rec.normal,
				Depth:       rec.depth,
				IsTrigger:   rec.isTrigger,
			}
		}
	}

	if len(w.contactEvents) > 0 {
		snap.ContactEvents = make([]ContactEventSnapshot, len(w.contactEvents))
		for i, ev := range w.contactEvents {
			snap.ContactEvents[i] = ContactEventSnapshot{
				Kind:        ev.Kind,
				BodyAID:     bodyIDOrZero(ev.BodyA),
				BodyBID:     bodyIDOrZero(ev.BodyB),
				ChildIndexA: ev.ChildIndexA,
				ChildIndexB: ev.ChildIndexB,
				Point:       ev.Point,
				Normal:      ev.Normal,
				Depth:       ev.Depth,
				IsTrigger:   ev.IsTrigger,
			}
		}
	}

	if len(w.jointBreakEvents) > 0 {
		snap.JointBreakEvents = make([]JointBreakEventSnapshot, len(w.jointBreakEvents))
		for i, ev := range w.jointBreakEvents {
			var jsPtr *JointSnapshot
			if js, ok := snapshotJoint3D(ev.Joint); ok {
				jsPtr = &js
			}
			snap.JointBreakEvents[i] = JointBreakEventSnapshot{
				Joint:   jsPtr,
				BodyAID: bodyIDOrZero(ev.BodyA),
				BodyBID: bodyIDOrZero(ev.BodyB),
				Force:   ev.Force,
				Torque:  ev.Torque,
			}
		}
	}

	if len(w.ignoredPairs) > 0 {
		keys := make([]pairKey, 0, len(w.ignoredPairs))
		for k := range w.ignoredPairs {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, comparePairKeys)
		snap.IgnoredPairs = make([]PairKeySnapshot, len(keys))
		for i, k := range keys {
			snap.IgnoredPairs[i] = PairKeySnapshot{A: k.a, B: k.b}
		}
	}

	if len(w.joints) > 0 {
		snap.Joints = make([]JointSnapshot, 0, len(w.joints))
		for _, j := range w.joints {
			if js, ok := snapshotJoint3D(j); ok {
				snap.Joints = append(snap.Joints, js)
			}
		}
	}

	if len(w.triMeshes) > 0 {
		snap.TriMeshes = make([]TriMeshSnapshot, 0, len(w.triMeshes))
		for _, tm := range w.triMeshes {
			if tm == nil {
				continue
			}
			tris := make([]Triangle, len(tm.Triangles))
			copy(tris, tm.Triangles)
			snap.TriMeshes = append(snap.TriMeshes, TriMeshSnapshot{
				ID:          tm.ID,
				Tag:         tm.Tag,
				Layer:       tm.Layer,
				Mask:        tm.Mask,
				Friction:    tm.Friction,
				Restitution: tm.Restitution,
				Triangles:   tris,
			})
		}
	}

	if len(w.heightfields) > 0 {
		snap.Heightfields = make([]HeightfieldSnapshot, 0, len(w.heightfields))
		for _, hf := range w.heightfields {
			if hf == nil {
				continue
			}
			heights := make([]float32, len(hf.Heights))
			copy(heights, hf.Heights)
			snap.Heightfields = append(snap.Heightfields, HeightfieldSnapshot{
				ID:          hf.ID,
				Tag:         hf.Tag,
				Layer:       hf.Layer,
				Mask:        hf.Mask,
				Friction:    hf.Friction,
				Restitution: hf.Restitution,
				Cols:        hf.Cols,
				Rows:        hf.Rows,
				CellSize:    hf.CellSize,
				Origin:      hf.Origin,
				Heights:     heights,
			})
		}
	}

	if len(w.voxels) > 0 {
		snap.Voxels = make([]VoxelColliderSnapshot, 0, len(w.voxels))
		for _, vc := range w.voxels {
			if vc == nil {
				continue
			}
			snap.Voxels = append(snap.Voxels, VoxelColliderSnapshot{
				ID:          vc.ID,
				Tag:         vc.Tag,
				Layer:       vc.Layer,
				Mask:        vc.Mask,
				Friction:    vc.Friction,
				Restitution: vc.Restitution,
				Origin:      vc.Origin,
			})
		}
	}

	return snap
}

// Restore replaces w's state from snap. Existing *Body, Joint3D, and collider
// pointers are reused in place by ID/slot when possible so external handles
// (such as vehicle chassis pointers) and World callbacks stay valid across
// rollback restores.
func (w *World) Restore(snap WorldSnapshot) error {
	if w == nil {
		return errors.New("physics3d: cannot restore nil World")
	}
	if snap.Version != SnapshotVersion {
		return fmt.Errorf("physics3d: unsupported WorldSnapshot version %d (want %d)", snap.Version, SnapshotVersion)
	}
	for i, js := range snap.Joints {
		switch js.Kind {
		case "distance", "ball", "hinge", "slider", "fixed", "swingtwist", "sixdof":
		default:
			return fmt.Errorf("physics3d: joints[%d] has unknown kind %q", i, js.Kind)
		}
	}
	for i, ev := range snap.JointBreakEvents {
		if ev.Joint != nil {
			switch ev.Joint.Kind {
			case "distance", "ball", "hinge", "slider", "fixed", "swingtwist", "sixdof":
			default:
				return fmt.Errorf("physics3d: jointBreakEvents[%d] has unknown joint kind %q", i, ev.Joint.Kind)
			}
		}
	}

	// Reuse existing *Body pointers by ID so external handles stay valid.
	existingByID := make(map[int]*Body, len(w.bodies))
	for _, b := range w.bodies {
		if b != nil {
			existingByID[b.ID] = b
		}
	}
	restoredBodies := make([]*Body, len(snap.Bodies))
	bodyByID := make(map[int]*Body, len(snap.Bodies))
	for i, bs := range snap.Bodies {
		dst := existingByID[bs.ID]
		if dst == nil {
			dst = new(Body)
		}
		applyBody3DSnapshot(dst, bs)
		restoredBodies[i] = dst
		bodyByID[dst.ID] = dst
	}

	lookupBody := func(id int) *Body {
		if id == 0 {
			return nil
		}
		return bodyByID[id]
	}

	restoredCache := make(map[contactKey]cachedManifold, len(snap.WarmStartCache))
	for _, c := range snap.WarmStartCache {
		cm := cachedManifold{normal: c.Normal}
		if len(c.Points) > 0 {
			n := min(len(c.Points), maxManifoldPoints)
			cm.count = n
			for p := 0; p < n; p++ {
				cp := c.Points[p]
				cm.points[p] = cachedPoint{
					localA:         cp.LocalA,
					localB:         cp.LocalB,
					featureID:      cp.FeatureID,
					normalImpulse:  cp.NormalImpulse,
					tangentImpulse: cp.TangentImpulse,
				}
			}
		} else {
			cm.count = 1
			cm.points[0] = cachedPoint{
				normalImpulse:  c.NormalImpulse,
				tangentImpulse: c.TangentImpulse,
			}
		}
		restoredCache[contactKey{a: c.BodyAID, b: c.BodyBID, childA: c.ChildA, childB: c.ChildB}] = cm
	}

	restoredActive := make(map[contactKey]activeContactRecord, len(snap.ActiveContacts))
	for _, ac := range snap.ActiveContacts {
		restoredActive[contactKey{a: ac.KeyA, b: ac.KeyB, childA: ac.KeyChildA, childB: ac.KeyChildB}] = activeContactRecord{
			a:         lookupBody(ac.BodyAID),
			b:         lookupBody(ac.BodyBID),
			childA:    ac.ChildIndexA,
			childB:    ac.ChildIndexB,
			point:     ac.Point,
			normal:    ac.Normal,
			depth:     ac.Depth,
			isTrigger: ac.IsTrigger,
		}
	}

	var restoredContactEvents []ContactEvent
	if len(snap.ContactEvents) > 0 {
		restoredContactEvents = make([]ContactEvent, len(snap.ContactEvents))
		for i, ev := range snap.ContactEvents {
			restoredContactEvents[i] = ContactEvent{
				Kind:        ev.Kind,
				BodyA:       lookupBody(ev.BodyAID),
				BodyB:       lookupBody(ev.BodyBID),
				ChildIndexA: ev.ChildIndexA,
				ChildIndexB: ev.ChildIndexB,
				Point:       ev.Point,
				Normal:      ev.Normal,
				Depth:       ev.Depth,
				IsTrigger:   ev.IsTrigger,
			}
		}
	}

	var restoredBreakEvents []JointBreakEvent
	if len(snap.JointBreakEvents) > 0 {
		restoredBreakEvents = make([]JointBreakEvent, len(snap.JointBreakEvents))
		for i, ev := range snap.JointBreakEvents {
			var j Joint3D
			if ev.Joint != nil {
				var err error
				j, err = restoreJoint3D(*ev.Joint, nil, lookupBody)
				if err != nil {
					return err
				}
			}
			restoredBreakEvents[i] = JointBreakEvent{
				Joint:  j,
				BodyA:  lookupBody(ev.BodyAID),
				BodyB:  lookupBody(ev.BodyBID),
				Force:  ev.Force,
				Torque: ev.Torque,
			}
		}
	}

	var restoredIgnored map[pairKey]struct{}
	if len(snap.IgnoredPairs) > 0 {
		restoredIgnored = make(map[pairKey]struct{}, len(snap.IgnoredPairs))
		for _, p := range snap.IgnoredPairs {
			restoredIgnored[pairKey{a: p.A, b: p.B}] = struct{}{}
		}
	}

	var restoredJoints []Joint3D
	if len(snap.Joints) > 0 {
		restoredJoints = make([]Joint3D, len(snap.Joints))
		for i, js := range snap.Joints {
			var existing Joint3D
			if i < len(w.joints) {
				existing = w.joints[i]
			}
			j, err := restoreJoint3D(js, existing, lookupBody)
			if err != nil {
				return err
			}
			restoredJoints[i] = j
		}
	}

	existingTriByID := make(map[int]*TriMeshCollider, len(w.triMeshes))
	for _, tm := range w.triMeshes {
		if tm != nil {
			existingTriByID[tm.ID] = tm
		}
	}
	var restoredTriMeshes []*TriMeshCollider
	if len(snap.TriMeshes) > 0 {
		restoredTriMeshes = make([]*TriMeshCollider, len(snap.TriMeshes))
		for i, tms := range snap.TriMeshes {
			if existing, ok := existingTriByID[tms.ID]; ok && slices.Equal(existing.Triangles, tms.Triangles) {
				existing.Tag = tms.Tag
				existing.Layer = tms.Layer
				existing.Mask = tms.Mask
				existing.Friction = tms.Friction
				existing.Restitution = tms.Restitution
				restoredTriMeshes[i] = existing
				continue
			}
			tm := newTriMeshFromTriangles(tms.Triangles)
			tm.ID = tms.ID
			tm.Tag = tms.Tag
			tm.Layer = tms.Layer
			tm.Mask = tms.Mask
			tm.Friction = tms.Friction
			tm.Restitution = tms.Restitution
			restoredTriMeshes[i] = tm
		}
	}

	existingHFByID := make(map[int]*HeightfieldCollider, len(w.heightfields))
	for _, hf := range w.heightfields {
		if hf != nil {
			existingHFByID[hf.ID] = hf
		}
	}
	var restoredHFs []*HeightfieldCollider
	if len(snap.Heightfields) > 0 {
		restoredHFs = make([]*HeightfieldCollider, len(snap.Heightfields))
		for i, hfs := range snap.Heightfields {
			if existing, ok := existingHFByID[hfs.ID]; ok &&
				existing.Cols == hfs.Cols &&
				existing.Rows == hfs.Rows &&
				existing.CellSize == hfs.CellSize &&
				existing.Origin == hfs.Origin &&
				slices.Equal(existing.Heights, hfs.Heights) {
				existing.Tag = hfs.Tag
				existing.Layer = hfs.Layer
				existing.Mask = hfs.Mask
				existing.Friction = hfs.Friction
				existing.Restitution = hfs.Restitution
				restoredHFs[i] = existing
				continue
			}
			hf := NewHeightfieldCollider(hfs.Cols, hfs.Rows, hfs.CellSize, hfs.Origin, hfs.Heights)
			hf.ID = hfs.ID
			hf.Tag = hfs.Tag
			hf.Layer = hfs.Layer
			hf.Mask = hfs.Mask
			hf.Friction = hfs.Friction
			hf.Restitution = hfs.Restitution
			restoredHFs[i] = hf
		}
	}

	existingVoxelByID := make(map[int]*VoxelCollider, len(w.voxels))
	for _, vc := range w.voxels {
		if vc != nil {
			existingVoxelByID[vc.ID] = vc
		}
	}
	var restoredVoxels []*VoxelCollider
	keptVoxels := make(map[*VoxelCollider]bool, len(snap.Voxels))
	if len(snap.Voxels) > 0 {
		restoredVoxels = make([]*VoxelCollider, 0, len(snap.Voxels))
		for _, vcs := range snap.Voxels {
			vc := existingVoxelByID[vcs.ID]
			if vc == nil {
				vc = &VoxelCollider{}
			}
			vc.ID = vcs.ID
			vc.Tag = vcs.Tag
			vc.Layer = vcs.Layer
			vc.Mask = vcs.Mask
			vc.Friction = vcs.Friction
			vc.Restitution = vcs.Restitution
			vc.Origin = vcs.Origin
			vc.attachWorld(w)
			keptVoxels[vc] = true
			restoredVoxels = append(restoredVoxels, vc)
		}
	}
	for _, vc := range w.voxels {
		if vc != nil && !keptVoxels[vc] {
			vc.detachWorld(w)
		}
	}

	w.Gravity = snap.Gravity
	w.SolverIterations = snap.SolverIterations
	w.nextID = snap.NextID
	w.nextColliderID = snap.NextColliderID
	w.bodies = restoredBodies
	w.cache = restoredCache
	w.activeContacts = restoredActive
	if w.nextActiveContacts != nil {
		clear(w.nextActiveContacts)
	}
	w.contactEvents = restoredContactEvents
	w.jointBreakEvents = restoredBreakEvents
	w.ignoredPairs = restoredIgnored
	w.joints = restoredJoints
	w.triMeshes = restoredTriMeshes
	w.heightfields = restoredHFs
	w.voxels = restoredVoxels
	w.bp.rebuild(w)
	return nil
}

// Checksum returns a deterministic 64-bit FNV-1a hash of w's complete state,
// including private solver caches, sleep timers, previous poses, active
// contacts, joints, and colliders.
func (w *World) Checksum() uint64 {
	h := detmath.NewHasher()
	h.WriteString("physics3d.World.v1")
	if w == nil {
		return h.Sum64()
	}
	hashVec3(&h, w.Gravity)
	h.WriteInt(w.SolverIterations)
	h.WriteInt(w.nextID)
	h.WriteInt(w.nextColliderID)

	h.WriteInt(len(w.bodies))
	for _, b := range w.bodies {
		if b == nil {
			h.WriteBool(false)
			continue
		}
		h.WriteBool(true)
		hashBody3D(&h, b)
	}

	if len(w.cache) > 0 {
		keys := make([]contactKey, 0, len(w.cache))
		for k := range w.cache {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, compareContactKeys)
		h.WriteInt(len(keys))
		for _, k := range keys {
			c := w.cache[k]
			h.WriteInt(k.a)
			h.WriteInt(k.b)
			if k.childA >= 0 || k.childB >= 0 {
				h.WriteInt(k.childA)
				h.WriteInt(k.childB)
			}
			hashVec3(&h, c.normal)
			h.WriteInt(c.count)
			for p := 0; p < c.count; p++ {
				cp := c.points[p]
				hashVec3(&h, cp.localA)
				hashVec3(&h, cp.localB)
				h.WriteUint32(cp.featureID)
				h.WriteFloat32(cp.normalImpulse)
				hashVec3(&h, cp.tangentImpulse)
			}
		}
	} else {
		h.WriteInt(0)
	}

	if len(w.activeContacts) > 0 {
		keys := make([]contactKey, 0, len(w.activeContacts))
		for k := range w.activeContacts {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, compareContactKeys)
		h.WriteInt(len(keys))
		for _, k := range keys {
			rec := w.activeContacts[k]
			h.WriteInt(k.a)
			h.WriteInt(k.b)
			if k.childA >= 0 || k.childB >= 0 {
				h.WriteInt(k.childA)
				h.WriteInt(k.childB)
			}
			h.WriteInt(bodyIDOrZero(rec.a))
			h.WriteInt(bodyIDOrZero(rec.b))
			if rec.childA >= 0 || rec.childB >= 0 {
				h.WriteInt(rec.childA)
				h.WriteInt(rec.childB)
			}
			hashVec3(&h, rec.point)
			hashVec3(&h, rec.normal)
			h.WriteFloat32(rec.depth)
			h.WriteBool(rec.isTrigger)
		}
	} else {
		h.WriteInt(0)
	}

	h.WriteInt(len(w.contactEvents))
	for _, ev := range w.contactEvents {
		h.WriteInt(int(ev.Kind))
		h.WriteInt(bodyIDOrZero(ev.BodyA))
		h.WriteInt(bodyIDOrZero(ev.BodyB))
		if ev.ChildIndexA >= 0 || ev.ChildIndexB >= 0 {
			h.WriteInt(ev.ChildIndexA)
			h.WriteInt(ev.ChildIndexB)
		}
		hashVec3(&h, ev.Point)
		hashVec3(&h, ev.Normal)
		h.WriteFloat32(ev.Depth)
		h.WriteBool(ev.IsTrigger)
	}

	h.WriteInt(len(w.jointBreakEvents))
	for _, ev := range w.jointBreakEvents {
		h.WriteInt(bodyIDOrZero(ev.BodyA))
		h.WriteInt(bodyIDOrZero(ev.BodyB))
		hashVec3(&h, ev.Force)
		hashVec3(&h, ev.Torque)
		if js, ok := snapshotJoint3D(ev.Joint); ok {
			h.WriteBool(true)
			hashJointSnapshot(&h, &js)
		} else {
			h.WriteBool(false)
		}
	}

	if len(w.ignoredPairs) > 0 {
		keys := make([]pairKey, 0, len(w.ignoredPairs))
		for k := range w.ignoredPairs {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, comparePairKeys)
		h.WriteInt(len(keys))
		for _, k := range keys {
			h.WriteInt(k.a)
			h.WriteInt(k.b)
		}
	} else {
		h.WriteInt(0)
	}

	h.WriteInt(len(w.joints))
	for _, j := range w.joints {
		if js, ok := snapshotJoint3D(j); ok {
			h.WriteBool(true)
			hashJointSnapshot(&h, &js)
		} else {
			h.WriteBool(false)
		}
	}

	h.WriteInt(len(w.triMeshes))
	for _, tm := range w.triMeshes {
		if tm == nil {
			h.WriteBool(false)
			continue
		}
		h.WriteBool(true)
		h.WriteInt(tm.ID)
		h.WriteString(tm.Tag)
		h.WriteUint32(uint32(tm.Layer))
		h.WriteUint32(uint32(tm.Mask))
		h.WriteFloat32(tm.Friction)
		h.WriteFloat32(tm.Restitution)
		h.WriteInt(len(tm.Triangles))
		for _, tri := range tm.Triangles {
			hashVec3(&h, tri.A)
			hashVec3(&h, tri.B)
			hashVec3(&h, tri.C)
		}
	}

	h.WriteInt(len(w.heightfields))
	for _, hf := range w.heightfields {
		if hf == nil {
			h.WriteBool(false)
			continue
		}
		h.WriteBool(true)
		h.WriteInt(hf.ID)
		h.WriteString(hf.Tag)
		h.WriteUint32(uint32(hf.Layer))
		h.WriteUint32(uint32(hf.Mask))
		h.WriteFloat32(hf.Friction)
		h.WriteFloat32(hf.Restitution)
		h.WriteInt(hf.Cols)
		h.WriteInt(hf.Rows)
		h.WriteFloat32(hf.CellSize)
		hashVec3(&h, hf.Origin)
		h.WriteInt(len(hf.Heights))
		for _, v := range hf.Heights {
			h.WriteFloat32(v)
		}
	}

	h.WriteInt(len(w.voxels))
	for _, vc := range w.voxels {
		if vc == nil {
			h.WriteBool(false)
			continue
		}
		h.WriteBool(true)
		h.WriteInt(vc.ID)
		h.WriteString(vc.Tag)
		h.WriteUint32(uint32(vc.Layer))
		h.WriteUint32(uint32(vc.Mask))
		h.WriteFloat32(vc.Friction)
		h.WriteFloat32(vc.Restitution)
		hashVec3(&h, vc.Origin)
	}

	return h.Sum64()
}

func hashVec3(h *detmath.Hasher, v Vec3) {
	h.WriteFloat32(v.X)
	h.WriteFloat32(v.Y)
	h.WriteFloat32(v.Z)
}

func hashQuat(h *detmath.Hasher, q Quat) {
	h.WriteFloat32(q.X)
	h.WriteFloat32(q.Y)
	h.WriteFloat32(q.Z)
	h.WriteFloat32(q.W)
}

func hashShape3D(h *detmath.Hasher, s Shape) {
	h.WriteInt(int(s.Kind))
	switch s.Kind {
	case ShapeBox:
		hashVec3(h, s.Box.Center)
		hashVec3(h, s.Box.HalfExtents)
		hashQuat(h, s.Rot)
	case ShapeSphere:
		hashVec3(h, s.Sphere.Center)
		h.WriteFloat32(s.Sphere.Radius)
	case ShapeCapsule:
		hashVec3(h, s.Capsule.A)
		hashVec3(h, s.Capsule.B)
		h.WriteFloat32(s.Capsule.Radius)
	case ShapeConvexHull:
		hashVec3(h, s.Hull.Center)
		hashQuat(h, s.Rot)
		g := s.Hull.geom
		if g == nil {
			h.WriteInt(0)
			return
		}
		h.WriteInt(len(g.v))
		for _, v := range g.v {
			h.WriteFloat64(v.x)
			h.WriteFloat64(v.y)
			h.WriteFloat64(v.z)
		}
		h.WriteInt(len(g.faces))
		for _, f := range g.faces {
			h.WriteInt(len(f.idx))
			for _, ix := range f.idx {
				h.WriteInt(ix)
			}
			h.WriteFloat64(f.d)
		}
	case ShapeCylinder:
		hashVec3(h, s.Cylinder.A)
		hashVec3(h, s.Cylinder.B)
		h.WriteFloat32(s.Cylinder.Radius)
	case ShapeCompound:
		h.WriteInt(len(s.Children))
		for _, c := range s.Children {
			hashShape3D(h, c.Shape)
			hashVec3(h, c.Offset)
			hashQuat(h, c.Rot)
			h.WriteFloat32(c.Mass)
			h.WriteFloat32(c.Density)
			h.WriteBool(c.Trigger)
			h.WriteUint32(uint32(c.Layer))
			h.WriteUint32(uint32(c.Mask))
		}
	}
}

func hashArea3D(h *detmath.Hasher, a AreaSettings) {
	h.WriteInt(a.Priority)
	h.WriteUint8(uint8(a.GravityMode))
	hashVec3(h, a.Gravity)
	h.WriteBool(a.PointGravity)
	hashVec3(h, a.GravityPoint)
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
	hashVec3(h, a.FluidVelocity)
}

func hashBody3D(h *detmath.Hasher, b *Body) {
	h.WriteInt(b.ID)
	h.WriteString(b.Tag)
	hashShape3D(h, b.Shape)
	hashVec3(h, b.Pos)
	hashVec3(h, b.Vel)
	hashQuat(h, b.Rot)
	hashVec3(h, b.AngVel)
	h.WriteFloat32(b.Mass)
	h.WriteFloat32(b.InvMass)
	h.WriteFloat32(b.InvInertia)
	h.WriteBool(b.LockRotation)
	h.WriteFloat32(b.Restitution)
	h.WriteFloat32(b.Friction)
	h.WriteFloat32(b.LinearDamping)
	h.WriteFloat32(b.AngularDamping)
	if b.Material != nil {
		h.WriteBool(true)
		h.WriteFloat32(b.Material.Friction)
		h.WriteFloat32(b.Material.Restitution)
		h.WriteFloat32(b.Material.LinearDamping)
		h.WriteFloat32(b.Material.AngularDamping)
	}
	h.WriteFloat32(b.GravityScale)
	h.WriteBool(b.gravityScaleSet)
	h.WriteBool(b.Static)
	h.WriteBool(b.Kinematic)
	h.WriteBool(b.Sleeping)
	h.WriteUint32(uint32(b.Layer))
	h.WriteUint32(uint32(b.Mask))
	h.WriteBool(b.IsTrigger)
	hashArea3D(h, b.Area)
	h.WriteFloat32(b.sleepTimer)
	h.WriteFloat32(b.residualDepth)
	hashVec3(h, b.lastPos)
	hashQuat(h, b.lastRot)
	h.WriteBool(b.havePose)
	h.WriteBool(b.restingOnVoxel)
	if b.CenterOfMass != (Vec3{}) {
		h.WriteUint8(1)
		hashVec3(h, b.CenterOfMass)
	}
	if !b.InvInertiaTensor.IsZero() {
		h.WriteUint8(2)
		h.WriteFloat32(b.InvInertiaTensor.XX)
		h.WriteFloat32(b.InvInertiaTensor.YY)
		h.WriteFloat32(b.InvInertiaTensor.ZZ)
		h.WriteFloat32(b.InvInertiaTensor.XY)
		h.WriteFloat32(b.InvInertiaTensor.XZ)
		h.WriteFloat32(b.InvInertiaTensor.YZ)
	}
	if b.customCenterOfMass {
		h.WriteUint8(3)
	}
	if b.customInertia {
		h.WriteUint8(4)
	}
}

func hashAxisConfig(h *detmath.Hasher, c AxisConfig) {
	h.WriteInt(int(c.Mode))
	h.WriteFloat32(c.Min)
	h.WriteFloat32(c.Max)
	h.WriteInt(int(c.Drive))
	h.WriteFloat32(c.Stiffness)
	h.WriteFloat32(c.Damping)
	h.WriteFloat32(c.TargetPos)
	h.WriteFloat32(c.TargetVel)
	h.WriteFloat32(c.MaxForce)
}

func hashAxisState(h *detmath.Hasher, s SixDOFAxisStateSnapshot) {
	h.WriteInt(s.LimitState)
	h.WriteFloat32(s.LimitImpulse)
	h.WriteFloat32(s.DriveImpulse)
	h.WriteFloat32(s.DriveGamma)
	h.WriteFloat32(s.DriveBias)
	h.WriteInt(s.DriveMode)
	h.WriteFloat32(s.DriveVel)
}

func hashJointSnapshot(h *detmath.Hasher, js *JointSnapshot) {
	h.WriteString(js.Kind)
	h.WriteInt(js.BodyAID)
	h.WriteInt(js.BodyBID)
	hashVec3(h, js.LocalAnchorA)
	hashVec3(h, js.LocalAnchorB)
	h.WriteFloat32(js.BreakForce)
	h.WriteFloat32(js.BreakTorque)
	h.WriteBool(js.CollideConnected)
	hashVec3(h, js.Reaction.StepLinearImpulse)
	hashVec3(h, js.Reaction.StepAngularImpulse)
	h.WriteFloat32(js.Reaction.LastDt)
	h.WriteBool(js.Reaction.Broken)
	switch js.Kind {
	case "distance":
		h.WriteFloat32(js.MinDistance)
		h.WriteFloat32(js.MaxDistance)
		h.WriteFloat32(js.Stiffness)
		h.WriteFloat32(js.Damping)
		h.WriteFloat32(js.AccImpulse)
		h.WriteInt(js.Mode)
		h.WriteFloat32(js.TargetDist)
		h.WriteFloat32(js.Gamma)
		h.WriteFloat32(js.Bias)
	case "ball":
		h.WriteFloat32(js.ConeLimit)
		hashVec3(h, js.LocalAxisA)
		hashVec3(h, js.LocalAxisB)
		h.WriteFloat32(js.SwingImpulse)
	case "hinge":
		hashVec3(h, js.LocalAxisA)
		hashVec3(h, js.LocalAxisB)
		h.WriteBool(js.EnableLimits)
		h.WriteFloat32(js.MinAngle)
		h.WriteFloat32(js.MaxAngle)
		h.WriteFloat32(js.MotorTargetVel)
		h.WriteFloat32(js.MotorMaxTorque)
		h.WriteFloat32(js.DriveTargetAngle)
		h.WriteFloat32(js.DriveStiffness)
		h.WriteFloat32(js.DriveDamping)
		h.WriteFloat32(js.DriveMaxTorque)
		h.WriteBool(js.RefInitialized)
		hashVec3(h, js.RefPerpA)
		hashVec3(h, js.RefPerpB)
		h.WriteFloat32(js.MotorImpulse)
		h.WriteFloat32(js.DriveImpulse)
		h.WriteFloat32(js.DriveGamma)
		h.WriteFloat32(js.DriveBias)
		h.WriteBool(js.DriveActive)
		h.WriteFloat32(js.LimitImpulse)
		h.WriteInt(js.LimitState)
		h.WriteFloat32(js.Angle)
	case "slider":
		hashVec3(h, js.LocalAxisA)
		h.WriteBool(js.EnableLimits)
		h.WriteFloat32(js.MinTranslation)
		h.WriteFloat32(js.MaxTranslation)
		h.WriteFloat32(js.MotorTargetVel)
		h.WriteFloat32(js.MotorMaxForce)
		h.WriteFloat32(js.DriveTargetPos)
		h.WriteFloat32(js.DriveStiffness)
		h.WriteFloat32(js.DriveDamping)
		h.WriteFloat32(js.DriveMaxForce)
		hashQuat(h, js.RefRelRot)
		h.WriteBool(js.RefInitialized)
		h.WriteFloat32(js.MotorImpulse)
		h.WriteFloat32(js.DriveImpulse)
		h.WriteFloat32(js.DriveGamma)
		h.WriteFloat32(js.DriveBias)
		h.WriteBool(js.DriveActive)
		h.WriteFloat32(js.LimitImpulse)
		h.WriteInt(js.LimitState)
	case "fixed":
		hashQuat(h, js.RefRelRot)
		h.WriteBool(js.RefInitialized)
	case "swingtwist":
		hashVec3(h, js.TwistAxisA)
		hashVec3(h, js.TwistAxisB)
		hashVec3(h, js.PlaneAxisA)
		hashVec3(h, js.PlaneAxisB)
		h.WriteBool(js.EnableSwingLimits)
		h.WriteFloat32(js.SwingMin1)
		h.WriteFloat32(js.SwingMax1)
		h.WriteFloat32(js.SwingMin2)
		h.WriteFloat32(js.SwingMax2)
		h.WriteBool(js.EnableTwistLimits)
		h.WriteFloat32(js.MinTwist)
		h.WriteFloat32(js.MaxTwist)
		h.WriteBool(js.RefInitialized)
		h.WriteFloat32(js.SwingImpulse)
		h.WriteFloat32(js.TwistImpulse)
		h.WriteInt(js.TwistLimitState)
	case "sixdof":
		hashQuat(h, js.LocalFrameA)
		hashQuat(h, js.LocalFrameB)
		for i := range 3 {
			hashAxisConfig(h, js.Linear[i])
			hashAxisConfig(h, js.Angular[i])
			hashAxisState(h, js.LinState[i])
			hashAxisState(h, js.AngState[i])
		}
		h.WriteBool(js.RefInitialized)
		h.WriteFloat32(js.SingleAxisAngle)
		h.WriteInt(js.SingleAxisIdx)
	}
}

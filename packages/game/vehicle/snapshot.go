package vehicle

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
	"github.com/nsgo-dev/game/packages/game/physics3d"
)

// SnapshotVersion is the schema version stored in Snapshot.
const SnapshotVersion = 1

// WheelSnapshot captures one wheel's dynamic runtime state.
type WheelSnapshot struct {
	Contact          bool           `json:"contact,omitempty"`
	ContactPoint     physics3d.Vec3 `json:"contactPoint"`
	ContactNormal    physics3d.Vec3 `json:"contactNormal"`
	GroundID         int            `json:"groundID,omitempty"`
	SuspensionLength float32        `json:"suspensionLength"`
	SuspensionForce  float32        `json:"suspensionForce"`
	SteerAngle       float32        `json:"steerAngle"`
	SpinSpeed        float32        `json:"spinSpeed"`
	SpinAngle        float32        `json:"spinAngle"`
	Sliding          bool           `json:"sliding,omitempty"`
}

// Snapshot captures the complete deterministic runtime state of a Vehicle
// (steering, input, ray mask, and every wheel's suspension and spin state).
// Pair it with physics3d.World.Save/Restore to save and restore the full car
// simulation cleanly as JSON.
type Snapshot struct {
	Version int                 `json:"version"`
	BodyID  int                 `json:"bodyID"`
	RayMask physics3d.LayerMask `json:"rayMask"`
	Input   Input               `json:"input"`
	Steer   float32             `json:"steer"`
	Wheels  []WheelSnapshot     `json:"wheels,omitempty"`
}

// Save captures a deep, deterministic, JSON-serializable snapshot of v.
func (v *Vehicle) Save() Snapshot {
	if v == nil {
		return Snapshot{Version: SnapshotVersion}
	}
	snap := Snapshot{
		Version: SnapshotVersion,
		RayMask: v.RayMask,
		Input:   v.input,
		Steer:   v.steer,
		Wheels:  make([]WheelSnapshot, len(v.Wheels)),
	}
	if v.Body != nil {
		snap.BodyID = v.Body.ID
	}
	for i := range v.Wheels {
		w := &v.Wheels[i]
		ws := WheelSnapshot{
			Contact:          w.Contact,
			ContactPoint:     w.ContactPoint,
			ContactNormal:    w.ContactNormal,
			SuspensionLength: w.SuspensionLength,
			SuspensionForce:  w.SuspensionForce,
			SteerAngle:       w.SteerAngle,
			SpinSpeed:        w.SpinSpeed,
			SpinAngle:        w.SpinAngle,
			Sliding:          w.Sliding,
		}
		if w.Ground != nil {
			ws.GroundID = w.Ground.ID
		}
		snap.Wheels[i] = ws
	}
	return snap
}

// Restore restores v's steering, input, ray mask, and wheel states from snap.
// When world is non-nil, v.Body and each wheel's Ground pointer are relinked by
// body ID against world.Bodies().
func (v *Vehicle) Restore(snap Snapshot, world *physics3d.World) error {
	if v == nil {
		return errors.New("vehicle: cannot restore nil Vehicle")
	}
	if snap.Version != SnapshotVersion {
		return fmt.Errorf("vehicle: unsupported Snapshot version %d (want %d)", snap.Version, SnapshotVersion)
	}
	if len(snap.Wheels) != len(v.Wheels) {
		return fmt.Errorf("vehicle: snapshot has %d wheels (want %d)", len(snap.Wheels), len(v.Wheels))
	}

	var bodyByID map[int]*physics3d.Body
	if world != nil {
		bodies := world.Bodies()
		bodyByID = make(map[int]*physics3d.Body, len(bodies))
		for _, b := range bodies {
			if b != nil {
				bodyByID[b.ID] = b
			}
		}
		if snap.BodyID != 0 {
			if b := bodyByID[snap.BodyID]; b != nil {
				v.Body = b
			}
		}
	}

	v.RayMask = snap.RayMask
	v.input = snap.Input
	v.steer = snap.Steer
	for i := range v.Wheels {
		ws := snap.Wheels[i]
		w := &v.Wheels[i]
		w.Contact = ws.Contact
		w.ContactPoint = ws.ContactPoint
		w.ContactNormal = ws.ContactNormal
		if bodyByID != nil && ws.GroundID != 0 {
			w.Ground = bodyByID[ws.GroundID]
		} else {
			w.Ground = nil
		}
		w.SuspensionLength = ws.SuspensionLength
		w.SuspensionForce = ws.SuspensionForce
		w.SteerAngle = ws.SteerAngle
		w.SpinSpeed = ws.SpinSpeed
		w.SpinAngle = ws.SpinAngle
		w.Sliding = ws.Sliding
	}
	return nil
}

// Checksum returns a deterministic 64-bit FNV-1a hash of v's chassis pose,
// velocities, steering, input, and all wheel states.
func (v *Vehicle) Checksum() uint64 {
	h := detmath.NewHasher()
	h.WriteString("vehicle.Vehicle.v1")
	if v == nil {
		return h.Sum64()
	}
	h.WriteUint32(uint32(v.RayMask))
	h.WriteFloat32(v.input.Throttle)
	h.WriteFloat32(v.input.Brake)
	h.WriteFloat32(v.input.Steer)
	h.WriteBool(v.input.Handbrake)
	h.WriteFloat32(v.steer)
	if v.Body != nil {
		h.WriteBool(true)
		h.WriteInt(v.Body.ID)
		h.WriteFloat32(v.Body.Pos.X)
		h.WriteFloat32(v.Body.Pos.Y)
		h.WriteFloat32(v.Body.Pos.Z)
		h.WriteFloat32(v.Body.Vel.X)
		h.WriteFloat32(v.Body.Vel.Y)
		h.WriteFloat32(v.Body.Vel.Z)
		h.WriteFloat32(v.Body.Rot.X)
		h.WriteFloat32(v.Body.Rot.Y)
		h.WriteFloat32(v.Body.Rot.Z)
		h.WriteFloat32(v.Body.Rot.W)
		h.WriteFloat32(v.Body.AngVel.X)
		h.WriteFloat32(v.Body.AngVel.Y)
		h.WriteFloat32(v.Body.AngVel.Z)
		h.WriteBool(v.Body.Sleeping)
	} else {
		h.WriteBool(false)
	}
	h.WriteInt(len(v.Wheels))
	for i := range v.Wheels {
		w := &v.Wheels[i]
		h.WriteBool(w.Contact)
		h.WriteFloat32(w.ContactPoint.X)
		h.WriteFloat32(w.ContactPoint.Y)
		h.WriteFloat32(w.ContactPoint.Z)
		h.WriteFloat32(w.ContactNormal.X)
		h.WriteFloat32(w.ContactNormal.Y)
		h.WriteFloat32(w.ContactNormal.Z)
		if w.Ground != nil {
			h.WriteInt(w.Ground.ID)
		} else {
			h.WriteInt(0)
		}
		h.WriteFloat32(w.SuspensionLength)
		h.WriteFloat32(w.SuspensionForce)
		h.WriteFloat32(w.SteerAngle)
		h.WriteFloat32(w.SpinSpeed)
		h.WriteFloat32(w.SpinAngle)
		h.WriteBool(w.Sliding)
	}
	return h.Sum64()
}

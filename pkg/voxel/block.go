package voxel

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// BlockID identifies a block type stored in a voxel chunk.
// ID 0 is always [Air].
type BlockID uint16

// Air is the reserved empty block ID (0).
const Air BlockID = 0

// MaxLightLevel is the maximum per-voxel sky or block light level (0..15).
const MaxLightLevel uint8 = 15

// Face identifies one of the six axis-aligned faces of a voxel cube.
type Face uint8

const (
	FacePosX Face = iota // +X (east)
	FaceNegX             // -X (west)
	FacePosY             // +Y (up / top)
	FaceNegY             // -Y (down / bottom)
	FacePosZ             // +Z (south)
	FaceNegZ             // -Z (north)
	// FaceCount is the number of cube face directions.
	FaceCount = 6
)

var faceNormals = [FaceCount]math3d.Vec3{
	FacePosX: {X: 1, Y: 0, Z: 0},
	FaceNegX: {X: -1, Y: 0, Z: 0},
	FacePosY: {X: 0, Y: 1, Z: 0},
	FaceNegY: {X: 0, Y: -1, Z: 0},
	FacePosZ: {X: 0, Y: 0, Z: 1},
	FaceNegZ: {X: 0, Y: 0, Z: -1},
}

var faceSteps = [FaceCount]BlockCoord{
	FacePosX: {X: 1, Y: 0, Z: 0},
	FaceNegX: {X: -1, Y: 0, Z: 0},
	FacePosY: {X: 0, Y: 1, Z: 0},
	FaceNegY: {X: 0, Y: -1, Z: 0},
	FacePosZ: {X: 0, Y: 0, Z: 1},
	FaceNegZ: {X: 0, Y: 0, Z: -1},
}

var faceOpposites = [FaceCount]Face{
	FacePosX: FaceNegX,
	FaceNegX: FacePosX,
	FacePosY: FaceNegY,
	FaceNegY: FacePosY,
	FacePosZ: FaceNegZ,
	FaceNegZ: FacePosZ,
}

var faceNames = [FaceCount]string{
	FacePosX: "+X",
	FaceNegX: "-X",
	FacePosY: "+Y",
	FaceNegY: "-Y",
	FacePosZ: "+Z",
	FaceNegZ: "-Z",
}

// Normal returns the outward unit normal of face f.
func (f Face) Normal() math3d.Vec3 {
	if int(f) < FaceCount {
		return faceNormals[f]
	}
	return math3d.Vec3{}
}

// Step returns the integer neighbor offset in the direction of face f.
func (f Face) Step() BlockCoord {
	if int(f) < FaceCount {
		return faceSteps[f]
	}
	return BlockCoord{}
}

// Opposite returns the face pointing in the opposite direction.
func (f Face) Opposite() Face {
	if int(f) < FaceCount {
		return faceOpposites[f]
	}
	return f
}

// String returns a short human-readable label such as "+Y".
func (f Face) String() string {
	if int(f) < FaceCount {
		return faceNames[f]
	}
	return fmt.Sprintf("Face(%d)", f)
}

// BlockCoord is an integer voxel coordinate in world space.
type BlockCoord struct {
	X, Y, Z int
}

// Add returns c + d.
func (c BlockCoord) Add(d BlockCoord) BlockCoord {
	return BlockCoord{X: c.X + d.X, Y: c.Y + d.Y, Z: c.Z + d.Z}
}

// Sub returns c - d.
func (c BlockCoord) Sub(d BlockCoord) BlockCoord {
	return BlockCoord{X: c.X - d.X, Y: c.Y - d.Y, Z: c.Z - d.Z}
}

// ChunkCoord is an integer chunk index in chunk-grid space.
type ChunkCoord struct {
	X, Y, Z int
}

// Add returns c + d.
func (c ChunkCoord) Add(d ChunkCoord) ChunkCoord {
	return ChunkCoord{X: c.X + d.X, Y: c.Y + d.Y, Z: c.Z + d.Z}
}

// Sub returns c - d.
func (c ChunkCoord) Sub(d ChunkCoord) ChunkCoord {
	return ChunkCoord{X: c.X - d.X, Y: c.Y - d.Y, Z: c.Z - d.Z}
}

// CompareChunkCoords orders two chunk coordinates lexicographically by X, Y, Z.
func CompareChunkCoords(a, b ChunkCoord) int {
	if c := cmp.Compare(a.X, b.X); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Y, b.Y); c != 0 {
		return c
	}
	return cmp.Compare(a.Z, b.Z)
}

// BlockDef describes a registered voxel block type.
//
// Zero values represent an opaque, solid block with MaterialID 0, so
// BlockDef{Name: "stone"} works with minimal configuration.
type BlockDef struct {
	// ID is the numeric block ID. Pass 0 to Register to assign the next free ID.
	ID BlockID
	// Name is the unique human-readable block name used in YAML saves.
	Name string
	// Transparent marks blocks that do not hide opaque neighbors behind them
	// (such as glass or water). Faces between two identical transparent blocks
	// are culled.
	Transparent bool
	// NonSolid marks blocks skipped by default solid raycasts (such as water or tall grass).
	NonSolid bool
	// MaterialID groups emitted faces into SubMesh ranges during meshing.
	MaterialID uint32
	// Emissive is the block light level (0..15) emitted by this block into the
	// block-light channel (for example 14 for a torch or 15 for lava/glowstone).
	// Zero means the block emits no light.
	Emissive uint8
	// LightOpacity is the light attenuation (0..15) applied to light passing
	// through a Transparent block (for example 1 for leaves or 2 for water).
	// Zero lets direct sky light pass unattenuated and attenuates propagated
	// light by 1 per voxel step. Opaque blocks (Transparent == false) always
	// block incoming light completely (effective opacity 15).
	LightOpacity uint8
}

// Sentinel errors returned by Registry operations.
var (
	ErrInvalidBlock   = errors.New("voxel: invalid block definition")
	ErrDuplicateBlock = errors.New("voxel: duplicate block definition")
	ErrRegistryFull   = errors.New("voxel: block registry is full")
)

type blockFlags uint8

const (
	flagRegistered blockFlags = 1 << iota
	flagTransparent
	flagNonSolid
)

type blockEntry struct {
	flags        blockFlags
	emissive     uint8
	lightOpacity uint8
	materialID   uint32
}

// Registry maps block IDs and names to their [BlockDef] properties.
// Block ID 0 is always pre-registered as "air" (transparent and non-solid).
// Unregistered non-zero block IDs are treated as opaque, solid, MaterialID 0
// so worlds can also use raw block IDs without pre-registration.
type Registry struct {
	byID   map[BlockID]BlockDef
	byName map[string]BlockID
	fast   []blockEntry // indexed directly by BlockID for zero-map-lookup meshing
	nextID BlockID
}

// NewRegistry returns a Registry containing only [Air] (ID 0, name "air").
func NewRegistry() *Registry {
	air := BlockDef{
		ID:          Air,
		Name:        "air",
		Transparent: true,
		NonSolid:    true,
	}
	r := &Registry{
		byID:   map[BlockID]BlockDef{Air: air},
		byName: map[string]BlockID{"air": Air},
		fast: []blockEntry{
			{flags: flagRegistered | flagTransparent | flagNonSolid},
		},
		nextID: 1,
	}
	return r
}

// Register adds def to the registry and returns its assigned non-zero BlockID.
// When def.ID is 0, the lowest unused ID starting from 1 is assigned.
func (r *Registry) Register(def BlockDef) (BlockID, error) {
	if r == nil {
		return 0, fmt.Errorf("%w: nil registry", ErrInvalidBlock)
	}
	if def.Name == "" {
		return 0, fmt.Errorf("%w: block name must not be empty", ErrInvalidBlock)
	}
	if def.Name == "air" {
		return 0, fmt.Errorf("%w: name %q is reserved for Air (ID 0)", ErrDuplicateBlock, def.Name)
	}
	if def.Emissive > MaxLightLevel {
		return 0, fmt.Errorf("%w: emissive light level %d exceeds max %d", ErrInvalidBlock, def.Emissive, MaxLightLevel)
	}
	if def.LightOpacity > MaxLightLevel {
		return 0, fmt.Errorf("%w: light opacity %d exceeds max %d", ErrInvalidBlock, def.LightOpacity, MaxLightLevel)
	}
	if _, exists := r.byName[def.Name]; exists {
		return 0, fmt.Errorf("%w: block name %q already registered", ErrDuplicateBlock, def.Name)
	}
	if def.ID == Air {
		if len(r.byID)-1 >= math.MaxUint16 {
			return 0, ErrRegistryFull
		}
		for r.nextID == Air || r.hasID(r.nextID) {
			r.nextID++
		}
		def.ID = r.nextID
		r.nextID++
	} else if r.hasID(def.ID) {
		return 0, fmt.Errorf("%w: block ID %d already registered", ErrDuplicateBlock, def.ID)
	}

	r.byID[def.ID] = def
	r.byName[def.Name] = def.ID
	r.setFast(def)
	return def.ID, nil
}

// MustRegister calls Register and panics on error.
func (r *Registry) MustRegister(def BlockDef) BlockID {
	id, err := r.Register(def)
	if err != nil {
		panic(err)
	}
	return id
}

func (r *Registry) hasID(id BlockID) bool {
	_, ok := r.byID[id]
	return ok
}

func (r *Registry) setFast(def BlockDef) {
	idx := int(def.ID)
	if idx >= len(r.fast) {
		grown := make([]blockEntry, idx+1)
		copy(grown, r.fast)
		r.fast = grown
	}
	flags := flagRegistered
	if def.Transparent {
		flags |= flagTransparent
	}
	if def.NonSolid {
		flags |= flagNonSolid
	}
	r.fast[idx] = blockEntry{
		flags:        flags,
		emissive:     def.Emissive,
		lightOpacity: def.LightOpacity,
		materialID:   def.MaterialID,
	}
}

// Get returns the BlockDef for id and whether it was registered.
func (r *Registry) Get(id BlockID) (BlockDef, bool) {
	if r == nil {
		return BlockDef{}, false
	}
	def, ok := r.byID[id]
	return def, ok
}

// ByName returns the BlockDef registered under name.
func (r *Registry) ByName(name string) (BlockDef, bool) {
	if r == nil {
		return BlockDef{}, false
	}
	id, ok := r.byName[name]
	if !ok {
		return BlockDef{}, false
	}
	return r.byID[id], true
}

// IsOpaque reports whether id blocks visibility of adjacent faces and casts
// ambient occlusion. Air is never opaque; unregistered non-zero IDs are opaque.
func (r *Registry) IsOpaque(id BlockID) bool {
	if id == Air {
		return false
	}
	if r == nil || int(id) >= len(r.fast) {
		return true
	}
	e := r.fast[id]
	if e.flags&flagRegistered == 0 {
		return true
	}
	return e.flags&flagTransparent == 0
}

// IsTransparent reports whether id is air or registered with Transparent=true.
func (r *Registry) IsTransparent(id BlockID) bool {
	return !r.IsOpaque(id)
}

// IsSolid reports whether id is solid for collision and default raycasts.
// Air is never solid; unregistered non-zero IDs are solid.
func (r *Registry) IsSolid(id BlockID) bool {
	if id == Air {
		return false
	}
	if r == nil || int(id) >= len(r.fast) {
		return true
	}
	e := r.fast[id]
	if e.flags&flagRegistered == 0 {
		return true
	}
	return e.flags&flagNonSolid == 0
}

// MaterialID returns the material group ID of id, or 0 if unregistered.
func (r *Registry) MaterialID(id BlockID) uint32 {
	if r == nil || int(id) >= len(r.fast) {
		return 0
	}
	return r.fast[id].materialID
}

// Emissive returns the block light emission level (0..15) of id, or 0 if Air or unregistered.
func (r *Registry) Emissive(id BlockID) uint8 {
	if id == Air || r == nil || int(id) >= len(r.fast) {
		return 0
	}
	return r.fast[id].emissive
}

// LightOpacity returns the light attenuation (0..15) of id.
// Air returns 0; opaque blocks (including unregistered non-zero IDs) return
// [MaxLightLevel] (15); transparent blocks return their configured LightOpacity.
func (r *Registry) LightOpacity(id BlockID) uint8 {
	if id == Air {
		return 0
	}
	if r == nil || int(id) >= len(r.fast) {
		return MaxLightLevel
	}
	e := r.fast[id]
	if e.flags&flagRegistered == 0 || e.flags&flagTransparent == 0 {
		return MaxLightLevel
	}
	return e.lightOpacity
}

// All returns all registered non-air block definitions sorted by BlockID.
func (r *Registry) All() []BlockDef {
	if r == nil || len(r.byID) <= 1 {
		return nil
	}
	out := make([]BlockDef, 0, len(r.byID)-1)
	for id, def := range r.byID {
		if id == Air {
			continue
		}
		out = append(out, def)
	}
	slices.SortFunc(out, func(a, b BlockDef) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

// Clone returns an independent copy of r.
func (r *Registry) Clone() *Registry {
	c := NewRegistry()
	if r == nil {
		return c
	}
	for _, def := range r.All() {
		_, _ = c.Register(def)
	}
	return c
}

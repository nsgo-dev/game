package physics3d

import (
	"math"
	"time"
)

const (
	// DefaultSoftSubsteps is the default number of XPBD substeps per SoftWorld.Step.
	DefaultSoftSubsteps = 8
	// DefaultSoftIterations is the default number of constraint solver iterations per substep.
	DefaultSoftIterations = 6
)

// Particle is a 3D point mass simulated by SoftWorld using Extended Position-Based Dynamics (XPBD).
// Set Pinned to true or InvMass to 0 to fix a particle in place. Zero Layer or Mask values
// default to LayerAll.
type Particle struct {
	Pos         Vec3
	PrevPos     Vec3
	Vel         Vec3
	InvMass     float32
	Radius      float32
	Friction    float32
	Restitution float32
	Pinned      bool
	Layer       LayerMask
	Mask        LayerMask

	force vec3d
}

// IsPinned reports whether p has infinite mass (Pinned is true or InvMass <= 0).
func (p *Particle) IsPinned() bool {
	return p == nil || p.Pinned || p.InvMass <= 0
}

// SetMass sets p's inverse mass to 1/mass when mass > 0, or 0 when mass <= 0.
func (p *Particle) SetMass(mass float32) {
	if p == nil {
		return
	}
	if mass <= 0 || !isFinite32(mass) {
		p.InvMass = 0
		return
	}
	p.InvMass = 1 / mass
}

// ApplyForce accumulates a world-space force applied to p during the next SoftWorld.Step.
func (p *Particle) ApplyForce(f Vec3) {
	if p == nil || p.IsPinned() || !isFiniteVec(f) {
		return
	}
	p.force.x += float64(f.X)
	p.force.y += float64(f.Y)
	p.force.z += float64(f.Z)
}

func (p *Particle) effectiveInvMass() float64 {
	if p.Pinned || p.InvMass <= 0 {
		return 0
	}
	return float64(p.InvMass)
}

// DistanceConstraint constrains the Euclidean distance between particles A and B.
// When MaxOnly is true, the constraint acts as a unilateral tether that only pulls
// particles together when their separation exceeds RestLength.
type DistanceConstraint struct {
	A, B       int
	RestLength float32
	Compliance float32
	MaxOnly    bool

	lambda float64
}

// BendingConstraint constrains the signed dihedral angle between triangles (A, B, C)
// and (B, A, D) sharing edge (A, B) toward RestAngle (in radians; 0 is coplanar/flat).
type BendingConstraint struct {
	A, B, C, D int
	RestAngle  float32
	Compliance float32

	lambda float64
}

// VolumeConstraint preserves the signed volume of tetrahedron (A, B, C, D).
type VolumeConstraint struct {
	A, B, C, D int
	RestVolume float32
	Compliance float32

	lambda float64
}

// RigidAttachment couples particle Particle to rigid Body at LocalAnchor.
// Compliance == 0 acts as a rigid two-way attachment; positive Compliance acts as
// a compliant elastic attachment while still transferring reaction impulses to Body.
type RigidAttachment struct {
	Particle    int
	Body        *Body
	LocalAnchor Vec3
	Compliance  float32

	lambda float64
}

// vec3d is an internal float64 3-vector used inside the XPBD constraint solver for precision.
type vec3d struct {
	x, y, z float64
}

func toVec3d(v Vec3) vec3d { return vec3d{float64(v.X), float64(v.Y), float64(v.Z)} }
func (v vec3d) toVec3() Vec3 {
	return Vec3{X: float32(v.x), Y: float32(v.y), Z: float32(v.z)}
}
func (v vec3d) add(b vec3d) vec3d {
	return vec3d{float64(v.x) + float64(b.x), float64(v.y) + float64(b.y), float64(v.z) + float64(b.z)}
}
func (v vec3d) sub(b vec3d) vec3d {
	return vec3d{float64(v.x) - float64(b.x), float64(v.y) - float64(b.y), float64(v.z) - float64(b.z)}
}
func (v vec3d) scale(s float64) vec3d {
	return vec3d{float64(v.x * s), float64(v.y * s), float64(v.z * s)}
}
func (v vec3d) dot(b vec3d) float64 {
	return float64(float64(v.x*b.x)+float64(v.y*b.y)) + float64(v.z*b.z)
}
func (v vec3d) cross(b vec3d) vec3d {
	return vec3d{
		x: float64(v.y*b.z) - float64(v.z*b.y),
		y: float64(v.z*b.x) - float64(v.x*b.z),
		z: float64(v.x*b.y) - float64(v.y*b.x),
	}
}
func (v vec3d) lenSq() float64 {
	return float64(float64(v.x*v.x)+float64(v.y*v.y)) + float64(v.z*v.z)
}
func (v vec3d) len() float64 { return math.Sqrt(v.lenSq()) }

type quatd struct {
	x, y, z, w float64
}

func toQuatd(q Quat) quatd {
	u := normalizedQuat(q)
	return quatd{float64(u.X), float64(u.Y), float64(u.Z), float64(u.W)}
}

func (q quatd) toQuat() Quat {
	return Quat{X: float32(q.x), Y: float32(q.y), Z: float32(q.z), W: float32(q.w)}.Norm()
}

func (q quatd) norm() quatd {
	ls := q.x*q.x + q.y*q.y + q.z*q.z + q.w*q.w
	if ls <= 1e-18 {
		return quatd{w: 1}
	}
	inv := 1.0 / math.Sqrt(ls)
	return quatd{q.x * inv, q.y * inv, q.z * inv, q.w * inv}
}

func (q quatd) mul(b quatd) quatd {
	return quatd{
		x: q.w*b.x + q.x*b.w + q.y*b.z - q.z*b.y,
		y: q.w*b.y - q.x*b.z + q.y*b.w + q.z*b.x,
		z: q.w*b.z + q.x*b.y - q.y*b.x + q.z*b.w,
		w: q.w*b.w - q.x*b.x - q.y*b.y - q.z*b.z,
	}
}

func (q quatd) rotate(v vec3d) vec3d {
	u := vec3d{q.x, q.y, q.z}
	t := u.cross(v).scale(2)
	return v.add(t.scale(q.w)).add(u.cross(t))
}

func integrateQuatd(q quatd, rotVec vec3d) quatd {
	angle := rotVec.len()
	if angle <= 1e-12 {
		return q
	}
	half := angle * 0.5
	s := math.Sin(half) / angle
	c := math.Cos(half)
	dq := quatd{x: rotVec.x * s, y: rotVec.y * s, z: rotVec.z * s, w: c}
	return dq.mul(q).norm()
}

type rigidBodyPose3D struct {
	body *Body
	pos  vec3d
	rot  quatd
}

type trackedBody3D struct {
	body        *Body
	staticOrKin bool
	targetPos   vec3d
	targetRot   quatd
	stepDPos    vec3d
	stepDRot    vec3d // axis * angle per substep
	curPos      vec3d
	curRot      quatd
	corrPos     vec3d
	corrAng     vec3d
	invMass     float64
	invInertia  float64
}

func (tb *trackedBody3D) worldAnchor(local Vec3) (anchor, r vec3d) {
	r = tb.curRot.rotate(toVec3d(local))
	return tb.curPos.add(r), r
}

func (tb *trackedBody3D) generalizedInvMass(r, n vec3d) float64 {
	if tb.staticOrKin {
		return 0
	}
	rxn := r.cross(n)
	return tb.invMass + tb.invInertia*rxn.lenSq()
}

func (tb *trackedBody3D) applyPosImpulse(p, r vec3d) {
	if tb.staticOrKin {
		return
	}
	dp := p.scale(tb.invMass)
	da := r.cross(p).scale(tb.invInertia)
	tb.curPos = tb.curPos.add(dp)
	tb.corrPos = tb.corrPos.add(dp)
	if da.lenSq() > 0 {
		tb.curRot = integrateQuatd(tb.curRot, da)
		tb.corrAng = tb.corrAng.add(da)
	}
}

// SoftWorld simulates 3D XPBD particles, constraints (distance, dihedral bending,
// tetrahedral volume), two-way rigid-body attachments, and collisions against World
// bodies, triangle meshes, and heightfields in deterministic order.
type SoftWorld struct {
	Particles   []Particle
	Distances   []DistanceConstraint
	Bendings    []BendingConstraint
	Volumes     []VolumeConstraint
	Attachments []RigidAttachment

	// World provides optional 3D bodies, triangle meshes, and heightfields to collide against.
	World *World
	// Gravity is the constant acceleration applied to unpinned particles. NewSoftWorld sets (0, -9.81, 0).
	Gravity Vec3
	// Wind is an optional constant external acceleration applied to unpinned particles.
	Wind Vec3
	// Damping is linear velocity damping per second (e.g. 0.02).
	Damping    float32
	Substeps   int
	Iterations int

	bodyHistory []rigidBodyPose3D
}

// NewSoftWorld returns a SoftWorld with standard Y-up gravity (0, -9.81, 0),
// 8 substeps, 6 solver iterations, and mild linear damping.
func NewSoftWorld() *SoftWorld {
	return &SoftWorld{
		Gravity:    Vec3{Y: -9.81},
		Damping:    0.02,
		Substeps:   DefaultSoftSubsteps,
		Iterations: DefaultSoftIterations,
	}
}

// AddParticle appends p to sw.Particles, initializes PrevPos to p.Pos, and returns its index.
func (sw *SoftWorld) AddParticle(p Particle) int {
	p.PrevPos = p.Pos
	if p.Friction < 0 {
		p.Friction = 0
	}
	if p.Restitution < 0 {
		p.Restitution = 0
	} else if p.Restitution > 1 {
		p.Restitution = 1
	}
	idx := len(sw.Particles)
	sw.Particles = append(sw.Particles, p)
	return idx
}

// PinParticle marks particle idx as pinned (immovable).
func (sw *SoftWorld) PinParticle(idx int) {
	if sw.validParticle(idx) {
		sw.Particles[idx].Pinned = true
		sw.Particles[idx].Vel = Vec3{}
	}
}

// UnpinParticle clears particle idx's Pinned flag.
func (sw *SoftWorld) UnpinParticle(idx int) {
	if sw.validParticle(idx) {
		sw.Particles[idx].Pinned = false
	}
}

// AddDistance adds a bilateral distance constraint between particles a and b.
// If restLength < 0, the current Euclidean distance between a and b is used.
func (sw *SoftWorld) AddDistance(a, b int, restLength, compliance float32) int {
	if restLength < 0 && sw.validParticle(a) && sw.validParticle(b) {
		restLength = sw.Particles[a].Pos.Dist(sw.Particles[b].Pos)
	}
	if restLength < 0 {
		restLength = 0
	}
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Distances)
	sw.Distances = append(sw.Distances, DistanceConstraint{
		A:          a,
		B:          b,
		RestLength: restLength,
		Compliance: compliance,
	})
	return idx
}

// AddTether adds a unilateral maximum-distance constraint between particles a and b
// that activates only when their separation exceeds maxLength.
func (sw *SoftWorld) AddTether(a, b int, maxLength, compliance float32) int {
	if maxLength < 0 && sw.validParticle(a) && sw.validParticle(b) {
		maxLength = sw.Particles[a].Pos.Dist(sw.Particles[b].Pos)
	}
	if maxLength < 0 {
		maxLength = 0
	}
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Distances)
	sw.Distances = append(sw.Distances, DistanceConstraint{
		A:          a,
		B:          b,
		RestLength: maxLength,
		Compliance: compliance,
		MaxOnly:    true,
	})
	return idx
}

// AddBending adds a dihedral bending constraint across shared edge (a, b) between
// triangles (a, b, c) and (b, a, d), capturing the current dihedral angle as RestAngle.
func (sw *SoftWorld) AddBending(a, b, c, d int, compliance float32) int {
	var restAngle float32
	if sw.validParticle(a) && sw.validParticle(b) && sw.validParticle(c) && sw.validParticle(d) {
		if ang, ok := dihedralAngle3D(
			toVec3d(sw.Particles[a].Pos),
			toVec3d(sw.Particles[b].Pos),
			toVec3d(sw.Particles[c].Pos),
			toVec3d(sw.Particles[d].Pos),
		); ok {
			restAngle = float32(ang)
		}
	}
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Bendings)
	sw.Bendings = append(sw.Bendings, BendingConstraint{
		A:          a,
		B:          b,
		C:          c,
		D:          d,
		RestAngle:  restAngle,
		Compliance: compliance,
	})
	return idx
}

// AddVolume adds a tetrahedral volume preservation constraint for particles (a, b, c, d),
// capturing their current signed volume as RestVolume.
func (sw *SoftWorld) AddVolume(a, b, c, d int, compliance float32) int {
	var restVol float32
	if sw.validParticle(a) && sw.validParticle(b) && sw.validParticle(c) && sw.validParticle(d) {
		restVol = float32(tetVolume3D(
			toVec3d(sw.Particles[a].Pos),
			toVec3d(sw.Particles[b].Pos),
			toVec3d(sw.Particles[c].Pos),
			toVec3d(sw.Particles[d].Pos),
		))
	}
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Volumes)
	sw.Volumes = append(sw.Volumes, VolumeConstraint{
		A:          a,
		B:          b,
		C:          c,
		D:          d,
		RestVolume: restVol,
		Compliance: compliance,
	})
	return idx
}

// AttachBody couples particleIdx to body at localAnchor with two-way reaction impulses.
// Set compliance to 0 for a rigid pin attachment or > 0 for an elastic attachment.
func (sw *SoftWorld) AttachBody(particleIdx int, body *Body, localAnchor Vec3, compliance float32) int {
	if compliance < 0 {
		compliance = 0
	}
	idx := len(sw.Attachments)
	sw.Attachments = append(sw.Attachments, RigidAttachment{
		Particle:    particleIdx,
		Body:        body,
		LocalAnchor: localAnchor,
		Compliance:  compliance,
	})
	return idx
}

// RopeLength returns the total Euclidean polyline length along the ordered particle indices.
func (sw *SoftWorld) RopeLength(particles []int) float32 {
	if len(particles) < 2 {
		return 0
	}
	var total float64
	for i := 0; i < len(particles)-1; i++ {
		a, b := particles[i], particles[i+1]
		if !sw.validParticle(a) || !sw.validParticle(b) {
			continue
		}
		total += toVec3d(sw.Particles[b].Pos).sub(toVec3d(sw.Particles[a].Pos)).len()
	}
	return float32(total)
}

// Edges returns wireframe segments for all structural (non-MaxOnly) distance constraints in sw.
func (sw *SoftWorld) Edges() [][2]Vec3 {
	if sw == nil {
		return nil
	}
	edges := make([][2]Vec3, 0, len(sw.Distances))
	for _, c := range sw.Distances {
		if c.MaxOnly || !sw.validParticle(c.A) || !sw.validParticle(c.B) {
			continue
		}
		edges = append(edges, [2]Vec3{sw.Particles[c.A].Pos, sw.Particles[c.B].Pos})
	}
	return edges
}

// RopeConfig configures a 3D rope constructed by SoftWorld.AddRope.
type RopeConfig struct {
	Start, End     Vec3
	Segments       int
	TotalMass      float32
	ParticleRadius float32
	Compliance     float32
	PinStart       bool
	PinEnd         bool
	Layer          LayerMask
	Mask           LayerMask
	Friction       float32
}

// AddRope constructs a 3D chain of Segments+1 particles from Start to End with distance
// constraints (plus unilateral tethers when Compliance == 0 so heavy attached bodies
// cannot over-stretch the rope).
func (sw *SoftWorld) AddRope(cfg RopeConfig) []int {
	segs := cfg.Segments
	if segs < 1 {
		segs = 1
	}
	totalMass := cfg.TotalMass
	if totalMass <= 0 {
		totalMass = 1
	}
	radius := cfg.ParticleRadius
	if radius <= 0 {
		radius = 0.03
	}
	friction := cfg.Friction
	if friction <= 0 {
		friction = 0.4
	}
	count := segs + 1
	invMass := float32(count) / totalMass

	indices := make([]int, count)
	for i := 0; i < count; i++ {
		t := float32(i) / float32(segs)
		pos := lerpVec3(cfg.Start, cfg.End, t)
		pinned := (i == 0 && cfg.PinStart) || (i == segs && cfg.PinEnd)
		indices[i] = sw.AddParticle(Particle{
			Pos:      pos,
			InvMass:  invMass,
			Radius:   radius,
			Friction: friction,
			Pinned:   pinned,
			Layer:    cfg.Layer,
			Mask:     cfg.Mask,
		})
	}

	segLen := cfg.Start.Dist(cfg.End) / float32(segs)
	for i := 0; i < segs; i++ {
		sw.AddDistance(indices[i], indices[i+1], segLen, cfg.Compliance)
	}
	if cfg.Compliance <= 0 && segs >= 2 {
		for i := 2; i <= segs; i++ {
			sw.AddTether(indices[0], indices[i], segLen*float32(i), 0)
		}
		if cfg.PinEnd {
			for i := 1; i <= segs-2; i++ {
				sw.AddTether(indices[segs], indices[i], segLen*float32(segs-i), 0)
			}
		}
	}
	return indices
}

// ClothConfig configures a rectangular 3D cloth grid constructed by SoftWorld.AddCloth.
type ClothConfig struct {
	Origin            Vec3
	AxisU             Vec3    // direction along columns; zero defaults to +X
	AxisV             Vec3    // direction along rows; zero defaults to +Z
	Width, Height     float32 // physical extent along AxisU and AxisV
	Cols, Rows        int     // number of quads (>= 1); creates (Cols+1)*(Rows+1) particles
	TotalMass         float32 // <= 0 defaults to 1.0
	ParticleRadius    float32 // <= 0 defaults to 0.04
	StretchCompliance float32 // structural and shear compliance
	BendCompliance    float32 // dihedral bending compliance (0 defaults to 0.01 unless DisableBending)
	DisableBending    bool
	PinTopLeft        bool // pin (col=0, row=0)
	PinTopRight       bool // pin (col=Cols, row=0)
	PinBottomLeft     bool // pin (col=0, row=Rows)
	PinBottomRight    bool // pin (col=Cols, row=Rows)
	Layer             LayerMask
	Mask              LayerMask
	Friction          float32
}

// ClothGrid holds the particle layout created by SoftWorld.AddCloth.
type ClothGrid struct {
	Particles []int
	Cols      int
	Rows      int
}

// Index returns the particle index in SoftWorld.Particles at grid vertex (col, row),
// or -1 when out of range.
func (g ClothGrid) Index(col, row int) int {
	if col < 0 || col > g.Cols || row < 0 || row > g.Rows {
		return -1
	}
	idx := row*(g.Cols+1) + col
	if idx < 0 || idx >= len(g.Particles) {
		return -1
	}
	return g.Particles[idx]
}

// AddCloth builds a triangulated (Cols+1) x (Rows+1) cloth mesh with structural, shear,
// and dihedral bending constraints.
func (sw *SoftWorld) AddCloth(cfg ClothConfig) ClothGrid {
	cols := cfg.Cols
	if cols < 1 {
		cols = 1
	}
	rows := cfg.Rows
	if rows < 1 {
		rows = 1
	}
	width := cfg.Width
	if width <= 0 {
		width = 1
	}
	height := cfg.Height
	if height <= 0 {
		height = 1
	}
	uAxis := cfg.AxisU.Norm()
	if uAxis.LenSq() == 0 {
		uAxis = Vec3{X: 1}
	}
	vAxis := cfg.AxisV.Norm()
	if vAxis.LenSq() == 0 {
		vAxis = Vec3{Z: 1}
	}
	totalMass := cfg.TotalMass
	if totalMass <= 0 {
		totalMass = 1
	}
	radius := cfg.ParticleRadius
	if radius <= 0 {
		radius = 0.04
	}
	friction := cfg.Friction
	if friction <= 0 {
		friction = 0.4
	}
	bendComp := cfg.BendCompliance
	if bendComp <= 0 {
		bendComp = 1.0
	}

	nCols := cols + 1
	nRows := rows + 1
	count := nCols * nRows
	invMass := float32(count) / totalMass

	grid := ClothGrid{
		Particles: make([]int, count),
		Cols:      cols,
		Rows:      rows,
	}

	du := width / float32(cols)
	dv := height / float32(rows)
	for r := 0; r < nRows; r++ {
		for c := 0; c < nCols; c++ {
			pos := cfg.Origin.Add(uAxis.Scale(float32(c) * du)).Add(vAxis.Scale(float32(r) * dv))
			pinned := (r == 0 && c == 0 && cfg.PinTopLeft) ||
				(r == 0 && c == cols && cfg.PinTopRight) ||
				(r == rows && c == 0 && cfg.PinBottomLeft) ||
				(r == rows && c == cols && cfg.PinBottomRight)
			grid.Particles[r*nCols+c] = sw.AddParticle(Particle{
				Pos:      pos,
				InvMass:  invMass,
				Radius:   radius,
				Friction: friction,
				Pinned:   pinned,
				Layer:    cfg.Layer,
				Mask:     cfg.Mask,
			})
		}
	}

	at := func(c, r int) int { return grid.Particles[r*nCols+c] }

	// Structural constraints (horizontal & vertical).
	for r := 0; r < nRows; r++ {
		for c := 0; c < cols; c++ {
			sw.AddDistance(at(c, r), at(c+1, r), -1, cfg.StretchCompliance)
		}
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < nCols; c++ {
			sw.AddDistance(at(c, r), at(c, r+1), -1, cfg.StretchCompliance)
		}
	}

	// Shear constraints (both diagonals per quad).
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			sw.AddDistance(at(c, r), at(c+1, r+1), -1, cfg.StretchCompliance)
			sw.AddDistance(at(c+1, r), at(c, r+1), -1, cfg.StretchCompliance)
		}
	}

	// Dihedral bending constraints across interior edges of the triangulated grid.
	// Each quad (c, r) is split along diagonal (p00, p11) into triangles (p00, p11, p10) and (p11, p00, p01).
	if !cfg.DisableBending {
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				p00 := at(c, r)
				p10 := at(c+1, r)
				p01 := at(c, r+1)
				p11 := at(c+1, r+1)
				// Across internal quad diagonal (p00, p11).
				sw.AddBending(p00, p11, p10, p01, bendComp)
				// Across vertical edge (p10, p11) shared with right neighbor quad.
				if c+1 < cols {
					p21 := at(c+2, r+1)
					sw.AddBending(p10, p11, p00, p21, bendComp)
				}
				// Across horizontal edge (p01, p11) shared with bottom neighbor quad.
				if r+1 < rows {
					p12 := at(c+1, r+2)
					sw.AddBending(p01, p11, p12, p00, bendComp)
				}
			}
		}
	}

	return grid
}

// SoftBoxConfig configures an 8-vertex, 5-tetrahedron 3D soft box constructed by SoftWorld.AddSoftBox.
type SoftBoxConfig struct {
	Center           Vec3
	HalfExtents      Vec3
	TotalMass        float32
	ParticleRadius   float32
	EdgeCompliance   float32
	VolumeCompliance float32
	Layer            LayerMask
	Mask             LayerMask
	Friction         float32
	Restitution      float32
}

// AddSoftBox constructs a deformable 3D box made of 8 corner particles, 18 distance
// constraints (12 outer edges + 6 face diagonals), and 5 tetrahedral volume constraints.
func (sw *SoftWorld) AddSoftBox(cfg SoftBoxConfig) []int {
	he := cfg.HalfExtents
	if he.X <= 0 {
		he.X = 0.5
	}
	if he.Y <= 0 {
		he.Y = 0.5
	}
	if he.Z <= 0 {
		he.Z = 0.5
	}
	totalMass := cfg.TotalMass
	if totalMass <= 0 {
		totalMass = 1
	}
	radius := cfg.ParticleRadius
	if radius <= 0 {
		radius = 0.04
	}
	friction := cfg.Friction
	if friction <= 0 {
		friction = 0.4
	}
	invMass := 8.0 / totalMass

	corners := [8]Vec3{
		{X: -he.X, Y: -he.Y, Z: -he.Z}, // 0
		{X: +he.X, Y: -he.Y, Z: -he.Z}, // 1
		{X: +he.X, Y: +he.Y, Z: -he.Z}, // 2
		{X: -he.X, Y: +he.Y, Z: -he.Z}, // 3
		{X: -he.X, Y: -he.Y, Z: +he.Z}, // 4
		{X: +he.X, Y: -he.Y, Z: +he.Z}, // 5
		{X: +he.X, Y: +he.Y, Z: +he.Z}, // 6
		{X: -he.X, Y: +he.Y, Z: +he.Z}, // 7
	}
	idx := make([]int, 8)
	for i, c := range corners {
		idx[i] = sw.AddParticle(Particle{
			Pos:         cfg.Center.Add(c),
			InvMass:     invMass,
			Radius:      radius,
			Friction:    friction,
			Restitution: cfg.Restitution,
			Layer:       cfg.Layer,
			Mask:        cfg.Mask,
		})
	}

	edges := [][2]int{
		// 12 outer edges
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{4, 5}, {5, 6}, {6, 7}, {7, 4},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
		// 6 face diagonals matching the 5-tet decomposition
		{0, 2}, {5, 7}, {0, 5}, {2, 7}, {0, 7}, {2, 5},
	}
	for _, e := range edges {
		sw.AddDistance(idx[e[0]], idx[e[1]], -1, cfg.EdgeCompliance)
	}

	// 5 non-overlapping tetrahedra tiling the cube.
	tets := [5][4]int{
		{0, 1, 2, 5},
		{0, 2, 3, 7},
		{0, 4, 5, 7},
		{2, 5, 6, 7},
		{0, 2, 5, 7},
	}
	for _, t := range tets {
		sw.AddVolume(idx[t[0]], idx[t[1]], idx[t[2]], idx[t[3]], cfg.VolumeCompliance)
	}
	return idx
}

func (sw *SoftWorld) validParticle(idx int) bool {
	return idx >= 0 && idx < len(sw.Particles)
}

func (sw *SoftWorld) findBodyHistory(b *Body) (rigidBodyPose3D, bool) {
	for _, h := range sw.bodyHistory {
		if h.body == b {
			return h, true
		}
	}
	return rigidBodyPose3D{}, false
}

func (sw *SoftWorld) saveBodyHistory(b *Body, pos vec3d, rot quatd) {
	for i := range sw.bodyHistory {
		if sw.bodyHistory[i].body == b {
			sw.bodyHistory[i].pos = pos
			sw.bodyHistory[i].rot = rot
			return
		}
	}
	sw.bodyHistory = append(sw.bodyHistory, rigidBodyPose3D{body: b, pos: pos, rot: rot})
}

type collisionHit3D struct {
	hit    bool
	normal vec3d
	rest   float64
}

// Step advances the 3D XPBD particle and constraint simulation by dt deterministically.
// Call this on the fixed game loop after World.Step(dt) so two-way rigid attachments stay
// synchronized without over-stretching.
func (sw *SoftWorld) Step(dt time.Duration) {
	if sw == nil || dt <= 0 || len(sw.Particles) == 0 {
		return
	}
	dtSec := dt.Seconds()
	if math.IsNaN(dtSec) || math.IsInf(dtSec, 0) || dtSec <= 0 {
		return
	}

	substeps := sw.Substeps
	if substeps <= 0 {
		substeps = DefaultSoftSubsteps
	}
	iters := sw.Iterations
	if iters <= 0 {
		iters = DefaultSoftIterations
	}
	h := dtSec / float64(substeps)
	invH := 1.0 / h
	invHSq := invH * invH

	// 1. Collect unique attached rigid bodies in deterministic attachment order.
	var tracked []trackedBody3D
	attachToTracked := make([]int, len(sw.Attachments))
	rigidAttachForParticle := make([]int, len(sw.Particles))
	for i := range rigidAttachForParticle {
		rigidAttachForParticle[i] = -1
	}
	for ai := range sw.Attachments {
		att := &sw.Attachments[ai]
		attachToTracked[ai] = -1
		if att.Body == nil || !sw.validParticle(att.Particle) {
			continue
		}
		ti := -1
		for k := range tracked {
			if tracked[k].body == att.Body {
				ti = k
				break
			}
		}
		if ti < 0 {
			b := att.Body
			tPos := toVec3d(b.Pos)
			tRot := toQuatd(b.Rot)
			sPos := tPos
			sRot := tRot
			if hist, ok := sw.findBodyHistory(b); ok {
				sPos = hist.pos
				sRot = hist.rot
			} else if !b.Static && !b.Kinematic {
				sPos = tPos.sub(toVec3d(b.Vel).scale(dtSec))
				sRot = integrateQuatd(tRot, toVec3d(b.AngVel).scale(-dtSec))
			}

			// Shortest-arc rotation vector from sRot to tRot, divided by substeps.
			dQ := tRot.mul(quatd{-sRot.x, -sRot.y, -sRot.z, sRot.w}).norm()
			if dQ.w < 0 {
				dQ = quatd{-dQ.x, -dQ.y, -dQ.z, -dQ.w}
			}
			var stepDRot vec3d
			sinHalf := math.Sqrt(dQ.x*dQ.x + dQ.y*dQ.y + dQ.z*dQ.z)
			if sinHalf > 1e-12 {
				ang := 2.0 * math.Atan2(sinHalf, dQ.w)
				scale := (ang / float64(substeps)) / sinHalf
				stepDRot = vec3d{dQ.x * scale, dQ.y * scale, dQ.z * scale}
			}

			invM := float64(b.InvMass)
			invI := float64(b.InvInertia)
			staticOrKin := b.Static || b.Kinematic || (invM <= 0 && invI <= 0)
			if staticOrKin {
				invM = 0
				invI = 0
			} else if b.LockRotation {
				invI = 0
			}

			ti = len(tracked)
			tracked = append(tracked, trackedBody3D{
				body:        b,
				staticOrKin: staticOrKin,
				targetPos:   tPos,
				targetRot:   tRot,
				stepDPos:    tPos.sub(sPos).scale(1.0 / float64(substeps)),
				stepDRot:    stepDRot,
				curPos:      sPos,
				curRot:      sRot,
				invMass:     invM,
				invInertia:  invI,
			})
		}
		attachToTracked[ai] = ti
		if att.Compliance <= 0 && rigidAttachForParticle[att.Particle] < 0 {
			rigidAttachForParticle[att.Particle] = ai
		}
	}

	attachedBodyOfParticle := make([]*Body, len(sw.Particles))
	for ai := range sw.Attachments {
		att := &sw.Attachments[ai]
		if att.Body != nil && sw.validParticle(att.Particle) && attachedBodyOfParticle[att.Particle] == nil {
			attachedBodyOfParticle[att.Particle] = att.Body
		}
	}

	extAccel := toVec3d(sw.Gravity).add(toVec3d(sw.Wind))
	dampFactor := 1.0
	if sw.Damping > 0 {
		dampFactor = 1.0 / (1.0 + float64(sw.Damping)*h)
	}

	pos := make([]vec3d, len(sw.Particles))
	prev := make([]vec3d, len(sw.Particles))
	vel := make([]vec3d, len(sw.Particles))
	for i := range sw.Particles {
		pos[i] = toVec3d(sw.Particles[i].Pos)
		vel[i] = toVec3d(sw.Particles[i].Vel)
	}

	hits := make([]collisionHit3D, len(sw.Particles))
	preHitVel := make([]vec3d, len(sw.Particles))

	// Precompute world body shapes and bounds once for the step if World is set.
	var worldBodies []*Body
	var bodyShapes []Shape
	var bodyBounds []Box
	if sw.World != nil {
		worldBodies = sw.World.bodies
		bodyShapes = make([]Shape, len(worldBodies))
		bodyBounds = make([]Box, len(worldBodies))
		for i, b := range worldBodies {
			bodyShapes[i] = b.WorldShape()
			bodyBounds[i] = bodyShapes[i].Bounds()
		}
	}

	syncRigidAttached := func() {
		for ai := range sw.Attachments {
			att := &sw.Attachments[ai]
			ti := attachToTracked[ai]
			if ti < 0 || att.Compliance > 0 || sw.Particles[att.Particle].IsPinned() {
				continue
			}
			anchor, _ := tracked[ti].worldAnchor(att.LocalAnchor)
			pos[att.Particle] = anchor
		}
	}

	for range substeps {
		// Advance attached rigid bodies smoothly through their per-substep motion.
		for ti := range tracked {
			tracked[ti].curPos = tracked[ti].curPos.add(tracked[ti].stepDPos)
			if tracked[ti].stepDRot.lenSq() > 0 {
				tracked[ti].curRot = integrateQuatd(tracked[ti].curRot, tracked[ti].stepDRot)
			}
		}

		// Predict particle positions.
		for i := range sw.Particles {
			p := &sw.Particles[i]
			prev[i] = pos[i]
			hits[i] = collisionHit3D{}

			if p.IsPinned() {
				vel[i] = vec3d{}
				continue
			}
			if ai := rigidAttachForParticle[i]; ai >= 0 {
				ti := attachToTracked[ai]
				anchor, _ := tracked[ti].worldAnchor(sw.Attachments[ai].LocalAnchor)
				pos[i] = anchor
				continue
			}
			w := p.effectiveInvMass()
			accel := extAccel.add(p.force.scale(w))
			vel[i] = vel[i].add(accel.scale(h)).scale(dampFactor)
			preHitVel[i] = vel[i]
			pos[i] = pos[i].add(vel[i].scale(h))
		}

		// Reset XPBD Lagrange multipliers for this substep.
		for i := range sw.Distances {
			sw.Distances[i].lambda = 0
		}
		for i := range sw.Bendings {
			sw.Bendings[i].lambda = 0
		}
		for i := range sw.Volumes {
			sw.Volumes[i].lambda = 0
		}
		for i := range sw.Attachments {
			sw.Attachments[i].lambda = 0
		}

		for iter := 0; iter < iters; iter++ {
			// Distance constraints (alternating sweep order for fast chain convergence).
			nDist := len(sw.Distances)
			for k := 0; k < nDist; k++ {
				di := k
				if iter&1 == 1 {
					di = nDist - 1 - k
				}
				sw.solveDistance3D(&sw.Distances[di], pos, invHSq, rigidAttachForParticle, attachToTracked, tracked)
			}

			// Dihedral bending constraints.
			for bi := range sw.Bendings {
				sw.solveBending3D(&sw.Bendings[bi], pos, invHSq)
			}

			// Tetrahedral volume constraints.
			for vi := range sw.Volumes {
				sw.solveVolume3D(&sw.Volumes[vi], pos, invHSq)
			}

			// Compliant rigid attachments.
			for ai := range sw.Attachments {
				att := &sw.Attachments[ai]
				ti := attachToTracked[ai]
				if ti < 0 || att.Compliance <= 0 {
					continue
				}
				sw.solveCompliantAttachment3D(att, &tracked[ti], pos, invHSq)
			}

			syncRigidAttached()

			// Resolve collisions against 3D world bodies, triangle meshes, and heightfields.
			if sw.World != nil {
				for i := range sw.Particles {
					if sw.Particles[i].IsPinned() || rigidAttachForParticle[i] >= 0 {
						continue
					}
					sw.resolveParticleCollisions3D(i, pos, prev, worldBodies, bodyShapes, bodyBounds, attachedBodyOfParticle[i], &hits[i])
				}
			}
		}

		// Derive XPBD velocities from constrained displacements.
		restThresh := max(0.5, 2.0*extAccel.len()*h)
		for i := range sw.Particles {
			p := &sw.Particles[i]
			if p.IsPinned() {
				vel[i] = vec3d{}
				continue
			}
			vel[i] = pos[i].sub(prev[i]).scale(invH)

			if hits[i].hit && hits[i].rest > 0 {
				preVN := preHitVel[i].dot(hits[i].normal)
				if -preVN > restThresh {
					curVN := vel[i].dot(hits[i].normal)
					targetVN := -hits[i].rest * preVN
					if targetVN > curVN {
						vel[i] = vel[i].add(hits[i].normal.scale(targetVN - curVN))
					}
				}
			}
		}
	}

	// Write back particle state and clear accumulated forces.
	for i := range sw.Particles {
		p := &sw.Particles[i]
		p.PrevPos = prev[i].toVec3()
		p.Pos = pos[i].toVec3()
		p.Vel = vel[i].toVec3()
		p.force = vec3d{}
	}

	// Write back two-way coupled rigid bodies and save pose history.
	invDt := 1.0 / dtSec
	for ti := range tracked {
		tb := &tracked[ti]
		b := tb.body
		if tb.staticOrKin {
			b.Pos = tb.targetPos.toVec3()
			b.Rot = tb.targetRot.toQuat()
		} else {
			b.Pos = tb.curPos.toVec3()
			b.Rot = tb.curRot.toQuat()
			if tb.corrPos.lenSq() > 0 || tb.corrAng.lenSq() > 0 {
				b.Wake()
				b.Vel = toVec3d(b.Vel).add(tb.corrPos.scale(invDt)).toVec3()
				if !b.LockRotation {
					b.AngVel = toVec3d(b.AngVel).add(tb.corrAng.scale(invDt)).toVec3()
				}
			}
		}
		sw.saveBodyHistory(b, toVec3d(b.Pos), toQuatd(b.Rot))
	}
}

func (sw *SoftWorld) solveDistance3D(
	c *DistanceConstraint,
	pos []vec3d,
	invHSq float64,
	rigidAttachForParticle []int,
	attachToTracked []int,
	tracked []trackedBody3D,
) {
	if !sw.validParticle(c.A) || !sw.validParticle(c.B) || c.A == c.B {
		return
	}
	pa := &sw.Particles[c.A]
	pb := &sw.Particles[c.B]

	attA := rigidAttachForParticle[c.A]
	attB := rigidAttachForParticle[c.B]
	if pa.IsPinned() {
		attA = -1
	}
	if pb.IsPinned() {
		attB = -1
	}

	var aPos, rA vec3d
	if attA >= 0 {
		aPos, rA = tracked[attachToTracked[attA]].worldAnchor(sw.Attachments[attA].LocalAnchor)
		pos[c.A] = aPos
	} else {
		aPos = pos[c.A]
	}

	var bPos, rB vec3d
	if attB >= 0 {
		bPos, rB = tracked[attachToTracked[attB]].worldAnchor(sw.Attachments[attB].LocalAnchor)
		pos[c.B] = bPos
	} else {
		bPos = pos[c.B]
	}

	d := bPos.sub(aPos)
	dist := d.len()
	if dist <= 1e-9 {
		return
	}
	err := dist - float64(c.RestLength)
	if c.MaxOnly && err <= 0 {
		return
	}

	n := d.scale(1.0 / dist)

	var wA, wB float64
	if attA >= 0 {
		wA = tracked[attachToTracked[attA]].generalizedInvMass(rA, n)
	} else {
		wA = pa.effectiveInvMass()
	}
	if attB >= 0 {
		wB = tracked[attachToTracked[attB]].generalizedInvMass(rB, n)
	} else {
		wB = pb.effectiveInvMass()
	}

	alphaTilde := float64(c.Compliance) * invHSq
	denom := wA + wB + alphaTilde
	if denom <= 1e-12 {
		return
	}
	dLambda := (-err - alphaTilde*c.lambda) / denom
	if c.MaxOnly {
		newLambda := c.lambda + dLambda
		if newLambda > 0 {
			dLambda = -c.lambda
			newLambda = 0
		}
		c.lambda = newLambda
	} else {
		c.lambda += dLambda
	}

	if attA >= 0 {
		tb := &tracked[attachToTracked[attA]]
		tb.applyPosImpulse(n.scale(-dLambda), rA)
		pos[c.A], _ = tb.worldAnchor(sw.Attachments[attA].LocalAnchor)
	} else if wA > 0 {
		pos[c.A] = pos[c.A].sub(n.scale(dLambda * wA))
	}

	if attB >= 0 {
		tb := &tracked[attachToTracked[attB]]
		tb.applyPosImpulse(n.scale(dLambda), rB)
		pos[c.B], _ = tb.worldAnchor(sw.Attachments[attB].LocalAnchor)
	} else if wB > 0 {
		pos[c.B] = pos[c.B].add(n.scale(dLambda * wB))
	}
}

// dihedralAngle3D returns the signed dihedral angle between triangles (a, b, c) and (b, a, d)
// around shared edge e = b - a. Coplanar triangles on opposite sides of (a, b) give 0.
func dihedralAngle3D(a, b, c, d vec3d) (float64, bool) {
	e := b.sub(a)
	eLen := e.len()
	if eLen <= 1e-9 {
		return 0, false
	}
	n1 := e.cross(c.sub(a))
	n2 := d.sub(a).cross(e)
	l1 := n1.len()
	l2 := n2.len()
	if l1 <= 1e-9 || l2 <= 1e-9 {
		return 0, false
	}
	u1 := n1.scale(1.0 / l1)
	u2 := n2.scale(1.0 / l2)
	eHat := e.scale(1.0 / eLen)
	sinA := u1.cross(u2).dot(eHat)
	cosA := u1.dot(u2)
	return math.Atan2(sinA, cosA), true
}

func wrapAngle3D(a float64) float64 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a <= -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

func (sw *SoftWorld) solveBending3D(c *BendingConstraint, pos []vec3d, invHSq float64) {
	if !sw.validParticle(c.A) || !sw.validParticle(c.B) || !sw.validParticle(c.C) || !sw.validParticle(c.D) {
		return
	}
	wA := sw.Particles[c.A].effectiveInvMass()
	wB := sw.Particles[c.B].effectiveInvMass()
	wC := sw.Particles[c.C].effectiveInvMass()
	wD := sw.Particles[c.D].effectiveInvMass()
	if wA+wB+wC+wD <= 0 {
		return
	}

	pA, pB, pC, pD := pos[c.A], pos[c.B], pos[c.C], pos[c.D]
	e := pB.sub(pA)
	eLenSq := e.lenSq()
	if eLenSq <= 1e-12 {
		return
	}
	eLen := math.Sqrt(eLenSq)

	e1 := pC.sub(pA)
	e2 := pD.sub(pA)
	n1 := e.cross(e1)
	n2 := e2.cross(e)
	n1LenSq := n1.lenSq()
	n2LenSq := n2.lenSq()
	if n1LenSq <= 1e-12 || n2LenSq <= 1e-12 {
		return
	}

	u1 := n1.scale(1.0 / math.Sqrt(n1LenSq))
	u2 := n2.scale(1.0 / math.Sqrt(n2LenSq))
	eHat := e.scale(1.0 / eLen)
	phi := math.Atan2(u1.cross(u2).dot(eHat), u1.dot(u2))
	err := wrapAngle3D(phi - float64(c.RestAngle))

	// Exact singularity-free gradients of the signed dihedral angle (Bridson 2003 / XPBD).
	gC := n1.scale(-eLen / n1LenSq)
	gD := n2.scale(-eLen / n2LenSq)
	t1 := e1.dot(e) / eLenSq
	t2 := e2.dot(e) / eLenSq
	gB := gC.scale(-t1).add(gD.scale(-t2))
	gA := gC.scale(t1 - 1.0).add(gD.scale(t2 - 1.0))

	alphaTilde := float64(c.Compliance) * invHSq
	denom := wA*gA.lenSq() + wB*gB.lenSq() + wC*gC.lenSq() + wD*gD.lenSq() + alphaTilde
	if denom <= 1e-12 {
		return
	}
	dLambda := (-err - alphaTilde*c.lambda) / denom
	c.lambda += dLambda

	if wA > 0 {
		pos[c.A] = pos[c.A].add(gA.scale(dLambda * wA))
	}
	if wB > 0 {
		pos[c.B] = pos[c.B].add(gB.scale(dLambda * wB))
	}
	if wC > 0 {
		pos[c.C] = pos[c.C].add(gC.scale(dLambda * wC))
	}
	if wD > 0 {
		pos[c.D] = pos[c.D].add(gD.scale(dLambda * wD))
	}
}

func tetVolume3D(a, b, c, d vec3d) float64 {
	return b.sub(a).cross(c.sub(a)).dot(d.sub(a)) / 6.0
}

func (sw *SoftWorld) solveVolume3D(c *VolumeConstraint, pos []vec3d, invHSq float64) {
	if !sw.validParticle(c.A) || !sw.validParticle(c.B) || !sw.validParticle(c.C) || !sw.validParticle(c.D) {
		return
	}
	wA := sw.Particles[c.A].effectiveInvMass()
	wB := sw.Particles[c.B].effectiveInvMass()
	wC := sw.Particles[c.C].effectiveInvMass()
	wD := sw.Particles[c.D].effectiveInvMass()
	if wA+wB+wC+wD <= 0 {
		return
	}

	pA, pB, pC, pD := pos[c.A], pos[c.B], pos[c.C], pos[c.D]
	ab := pB.sub(pA)
	ac := pC.sub(pA)
	ad := pD.sub(pA)

	const inv6 = 1.0 / 6.0
	gD := ab.cross(ac).scale(inv6)
	gC := ad.cross(ab).scale(inv6)
	gB := ac.cross(ad).scale(inv6)
	gA := gB.add(gC).add(gD).scale(-1)

	vol := gD.dot(ad)
	err := vol - float64(c.RestVolume)

	alphaTilde := float64(c.Compliance) * invHSq
	denom := wA*gA.lenSq() + wB*gB.lenSq() + wC*gC.lenSq() + wD*gD.lenSq() + alphaTilde
	if denom <= 1e-12 {
		return
	}
	dLambda := (-err - alphaTilde*c.lambda) / denom
	c.lambda += dLambda

	if wA > 0 {
		pos[c.A] = pos[c.A].add(gA.scale(dLambda * wA))
	}
	if wB > 0 {
		pos[c.B] = pos[c.B].add(gB.scale(dLambda * wB))
	}
	if wC > 0 {
		pos[c.C] = pos[c.C].add(gC.scale(dLambda * wC))
	}
	if wD > 0 {
		pos[c.D] = pos[c.D].add(gD.scale(dLambda * wD))
	}
}

func (sw *SoftWorld) solveCompliantAttachment3D(att *RigidAttachment, tb *trackedBody3D, pos []vec3d, invHSq float64) {
	if !sw.validParticle(att.Particle) {
		return
	}
	p := &sw.Particles[att.Particle]
	anchor, r := tb.worldAnchor(att.LocalAnchor)
	d := pos[att.Particle].sub(anchor)
	dist := d.len()
	if dist <= 1e-9 {
		return
	}
	n := d.scale(1.0 / dist)

	wP := p.effectiveInvMass()
	wB := tb.generalizedInvMass(r, n)
	alphaTilde := float64(att.Compliance) * invHSq
	denom := wP + wB + alphaTilde
	if denom <= 1e-12 {
		return
	}
	dLambda := (-dist - alphaTilde*att.lambda) / denom
	att.lambda += dLambda

	if wP > 0 {
		pos[att.Particle] = pos[att.Particle].add(n.scale(dLambda * wP))
	}
	if !tb.staticOrKin {
		tb.applyPosImpulse(n.scale(-dLambda), r)
	}
}

func (sw *SoftWorld) resolveParticleCollisions3D(
	i int,
	pos, prev []vec3d,
	worldBodies []*Body,
	bodyShapes []Shape,
	bodyBounds []Box,
	skipBody *Body,
	hitOut *collisionHit3D,
) {
	p := &sw.Particles[i]
	r := p.Radius
	if r < 1e-4 {
		r = 1e-4
	}

	applySurface := func(n vec3d, depth, surfaceFriction, surfaceRest float64) {
		if depth <= 0 {
			return
		}
		pos[i] = pos[i].add(n.scale(depth))

		mu := math.Sqrt(max(0, float64(p.Friction)) * max(0, surfaceFriction))
		if mu > 0 {
			disp := pos[i].sub(prev[i])
			vn := disp.dot(n)
			tang := disp.sub(n.scale(vn))
			tLen := tang.len()
			if tLen > 1e-12 {
				maxSlide := mu * depth
				if tLen <= maxSlide {
					pos[i] = pos[i].sub(tang)
				} else {
					pos[i] = pos[i].sub(tang.scale(maxSlide / tLen))
				}
			}
		}

		rest := max(float64(p.Restitution), surfaceRest)
		if !hitOut.hit || rest > hitOut.rest {
			hitOut.hit = true
			hitOut.normal = n
			hitOut.rest = rest
		}
	}

	makeParticleShape := func() (Shape, Box) {
		sp := Sphere{Center: pos[i].toVec3(), Radius: r}
		s := SphereShape(sp)
		return s, s.Bounds()
	}

	// 1. World rigid/static/kinematic bodies (skipping triggers and attached body).
	pShape, pBounds := makeParticleShape()
	for bi, b := range worldBodies {
		if b == nil || b == skipBody || b.IsTrigger {
			continue
		}
		if !canCollideMasks(p.Layer, p.Mask, b.Layer, b.Mask) {
			continue
		}
		if !aabbOverlap(pBounds, bodyBounds[bi]) {
			continue
		}
		if c := TestShapes(pShape, bodyShapes[bi]); c.Hit {
			applySurface(toVec3d(c.Normal), float64(c.Depth), float64(b.effectiveFriction()), float64(b.effectiveRestitution()))
			pShape, pBounds = makeParticleShape()
		}
	}

	// 2. Static triangle meshes in World.
	for _, tm := range sw.World.triMeshes {
		if !canCollideMasks(p.Layer, p.Mask, tm.Layer, tm.Mask) {
			continue
		}
		if !aabbOverlap(pBounds, tm.Bounds) {
			continue
		}
		tm.QueryTriangles(pBounds, func(tri Triangle) {
			if !aabbOverlap(pBounds, tri.Bounds()) {
				return
			}
			if c := TestSphereTriangle(pShape.Sphere, tri); c.Hit {
				applySurface(toVec3d(c.Normal), float64(c.Depth), float64(tm.Friction), float64(tm.Restitution))
				pShape, pBounds = makeParticleShape()
			}
		})
	}

	// 3. Static heightfields in World.
	for _, hf := range sw.World.heightfields {
		if !canCollideMasks(p.Layer, p.Mask, hf.Layer, hf.Mask) {
			continue
		}
		if !aabbOverlap(pBounds, hf.Bounds) {
			continue
		}
		hf.QueryTriangles(pBounds, func(tri Triangle) {
			if !aabbOverlap(pBounds, tri.Bounds()) {
				return
			}
			if c := testShapeHeightfieldTriangle(pShape, tri); c.Hit {
				applySurface(toVec3d(c.Normal), float64(c.Depth), float64(hf.Friction), float64(hf.Restitution))
				pShape, pBounds = makeParticleShape()
			}
		})
	}
}

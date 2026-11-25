// Ported from pixelfx (https://github.com/patch-gg/pixelfx) src/core/poses.js and the pose handling of src/core/act.js and src/core/rig.js at commit 5294518ce7c72e48b6100a744596e0acf57f2826, MIT License, Copyright (c) 2026 Patch and Hugo Duprez. See LICENSE and NOTICE.md.

package pixelfx

// PoseKey is one thing a pose can say. A pose only says what differs from standing easy (Stand), in screen terms, px from the feet, y negative up. Give the hands and feet; the skeleton works out the rest.
//
// How the body works. He faces right, three-quarters towards us, so his near shoulder is on the left of the chest on screen and his far shoulder on the right: for arms raised or open to us, the near hand goes left (FX < 0) and the far hand right (BX > 0). A hand driving forward closes the chest and brings its shoulder round; a hand drawn back opens him up. Bones have fixed lengths: an arm reaches about 15 px from its shoulder (shoulders are near y = -29), a leg 17 px. Arms don't stretch, so step the body in rather than ask too much.
//
// How to hold things. Keep a little bend in the elbow. The blade lags the hand on a swing, so the wrist breaks and it whips. Long weapons rest in one hand with the butt on the floor and take the second hand to swing. Everything is in depth, so a weapon can swing round the front of him: turn the body (Yaw) and the weapon goes with it.
type PoseKey uint8

// The pose keys. Numbers move smoothly between keys of an act; the switches (Two, ArmsFront, Pull, Nocked, ToeF, ToeB) are on when not 0 and change at the later key.
const (
	// Lean bends the spine forward (+) or back (-).
	Lean PoseKey = iota
	// Crouch lowers the hips.
	Crouch
	// Yaw turns the whole body about its feet (0 faces right, π faces left, in between towards us; a spin is really a spin).
	Yaw
	// FX and FY place the near (right) hand.
	FX
	FY
	// BX and BY place the far (left) hand.
	BX
	BY
	// FFX and FFY place the near foot.
	FFX
	FFY
	// FBX and FBY place the far foot.
	FBX
	FBY
	// X and Y are where the feet are, from the effect's origin.
	X
	Y
	// HX rolls the head and HY nods it.
	HX
	HY
	// WL foreshortens the weapon towards the viewer: its length on screen, 1 is full; WZ -1 turns it away instead.
	WL
	WZ
	// SA is the angle on screen of what the near hand holds (radians, 0 points forward), SB that of what the far hand holds.
	SA
	SB
	// FZ and BZ are the hands' depth, FZZ and BZZ the feet's.
	FZ
	BZ
	FZZ
	BZZ
	// Turn is how far the chest faces us, in radians (it follows the hands if you leave it out); Twist is extra chest turn.
	Turn
	Twist
	// Face -1 turns him round to face left.
	Face
	// Ghost leaves after-images.
	Ghost
	// Two puts both hands on the weapon.
	Two
	// ArmsFront brings the far hand round in front of the chest.
	ArmsFront
	// Pull draws a bow to the far hand, and Nocked puts an arrow on the string.
	Pull
	Nocked
	// ToeF and ToeB put the near and far foot up on the toes.
	ToeF
	ToeB
	numPoseKeys
)

// poseKeyNames are the keys' names in pixelfx.
var poseKeyNames = [numPoseKeys]string{"lean", "crouch", "yaw", "fx", "fy", "bx", "by", "ffx", "ffy", "fbx", "fby", "x", "y", "hx", "hy", "wl", "wz", "sa", "sb", "fz", "bz", "fzz", "bzz", "turn", "twist", "face", "ghost", "two", "armsFront", "pull", "nocked", "toeF", "toeB"}

// String returns the key's name in pixelfx, such as "fx".
func (k PoseKey) String() string {
	if k < numPoseKeys {
		return poseKeyNames[k]
	}
	return "?"
}

// isSwitch reports whether k is on or off rather than a number.
func (k PoseKey) isSwitch() bool { return k >= Two && k < numPoseKeys }

// PoseValue is a key with its value, for Pose.With.
type PoseValue struct {
	Key   PoseKey
	Value float64
}

// Is returns k set to v.
func (k PoseKey) Is(v float64) PoseValue { return PoseValue{k, v} }

// On returns the switch k turned on.
func (k PoseKey) On() PoseValue { return PoseValue{k, 1} }

// Pose is what a character is doing: the keys it sets, and what the near and far hands hold. The zero Pose sets nothing, which is standing easy.
type Pose struct {
	v         [numPoseKeys]float64
	set       uint64
	weapon    string
	offWeapon string
}

// With returns p with each of vs set.
func (p Pose) With(vs ...PoseValue) Pose {
	for _, kv := range vs {
		p.v[kv.Key] = kv.Value
		p.set |= 1 << kv.Key
	}
	return p
}

// Without returns p with each of ks left unsaid.
func (p Pose) Without(ks ...PoseKey) Pose {
	for _, k := range ks {
		p.v[k] = 0
		p.set &^= 1 << k
	}
	return p
}

// Has reports whether p says k.
func (p Pose) Has(k PoseKey) bool { return p.set&(1<<k) != 0 }

// Get returns k's value in p and whether p says it.
func (p Pose) Get(k PoseKey) (float64, bool) { return p.v[k], p.Has(k) }

// Or returns k's value in p, or def when p doesn't say it.
func (p Pose) Or(k PoseKey, def float64) float64 {
	if p.Has(k) {
		return p.v[k]
	}
	return def
}

// on reports whether p says k and it is not 0 (JavaScript's truthiness).
func (p Pose) on(k PoseKey) bool { return p.Has(k) && p.v[k] != 0 }

// Hold returns p with the near hand holding weapon (one of Weapons' names; "" holds nothing).
func (p Pose) Hold(weapon string) Pose {
	p.weapon = weapon
	return p
}

// HoldOff returns p with the far hand holding weapon ("" holds nothing).
func (p Pose) HoldOff(weapon string) Pose {
	p.offWeapon = weapon
	return p
}

// Weapon returns what the near hand holds, "" for nothing.
func (p Pose) Weapon() string { return p.weapon }

// OffWeapon returns what the far hand holds, "" for nothing.
func (p Pose) OffWeapon() string { return p.offWeapon }

// Merge returns p with everything q says laid over it, as pixelfx's { ...p, ...q }.
func (p Pose) Merge(q Pose) Pose {
	for k := range numPoseKeys {
		if q.Has(k) {
			p.v[k] = q.v[k]
			p.set |= 1 << k
		}
	}
	if q.weapon != "" {
		p.weapon = q.weapon
	}
	if q.offWeapon != "" {
		p.offWeapon = q.offWeapon
	}
	return p
}

// Stand is standing easy, in screen terms: hands and feet are where they should end up, y negative is up.
var Stand = Pose{}.With(Lean.Is(0), Crouch.Is(0), Yaw.Is(0), FX.Is(-3), FY.Is(-15), BX.Is(5), BY.Is(-15), FFX.Is(-2), FFY.Is(-4), FBX.Is(3), FBY.Is(-4))

// poseCacheKey is what the rig caches a rendered pose by: in the air or not, and every key but X, Y and Ghost rounded (angles and the weapon's length to a twentieth), as pixelfx keys its cache.
type poseCacheKey struct {
	air       bool
	set       uint64
	v         [numPoseKeys]int64
	weapon    string
	offWeapon string
}

func keyOf(p Pose) poseCacheKey {
	k := poseCacheKey{air: p.Or(Y, 0) < -0.5, weapon: p.weapon, offWeapon: p.offWeapon}
	for pk := range numPoseKeys {
		if !p.Has(pk) || pk == X || pk == Y || pk == Ghost {
			continue
		}
		k.set |= 1 << pk
		v := p.v[pk]
		switch {
		case pk.isSwitch():
			if v != 0 {
				k.v[pk] = 1
			}
		case pk == SA || pk == SB || pk == WL || pk == Yaw || pk == Turn:
			k.v[pk] = int64(Round(v * 20))
		default:
			k.v[pk] = int64(Round(v))
		}
	}
	return k
}

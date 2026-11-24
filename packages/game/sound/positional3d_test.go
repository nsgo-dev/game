package sound_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sound"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// listener3D sits at origin facing -Z with +Y up, full distance gain within 2 and silent from 22.
var listener3D = sound.Listener3D{
	Position: sound.V3(0, 0, 0),
	Forward:  sound.V3(0, 0, -1),
	Up:       sound.V3(0, 1, 0),
	Near:     2,
	Far:      22,
}

func TestEffectBehindCameraIsQuieterAndPannedBySide(t *testing.T) {
	m := startMixed(t)

	// Place a scene3d.Camera at (10, 2, 5) looking down -Z (towards (10, 2, -5)) with +Y up.
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 10, Y: 2, Z: 5}, math3d.DegToRad(60), 16.0/9.0, 0.1, 100)
	cam.LookAt(math3d.Vec3{X: 10, Y: 2, Z: -5}, math3d.Vec3Up())

	l := sound.Listener3D{
		Position: sound.Vec3(cam.Pos),
		Forward:  sound.Vec3(cam.Forward()),
		Up:       sound.Vec3(cam.Up()),
		Near:     2,
		Far:      22,
	}

	// Equidistant sources: front-left, behind-left, behind-right, directly front, directly behind.
	frontLeft := sound.V3(10-6, 2, 5-6)
	behindLeft := sound.V3(10-6, 2, 5+6)
	behindRight := sound.V3(10+6, 2, 5+6)
	frontCenter := sound.V3(10, 2, 5-10)
	behindCenter := sound.V3(10, 2, 5+10)

	m.click.Limit = 8
	for _, pos := range []sound.Vec3{frontLeft, behindLeft, behindRight, frontCenter, behindCenter} {
		if !m.click.PlayAt3D(l, pos, audio.Voice{}) {
			t.Fatalf("PlayAt3D(%v) did not play", pos)
		}
	}
	plays := m.plays()
	if len(plays) != 5 {
		t.Fatalf("plays = %d, want 5", len(plays))
	}

	pFrontLeft, pBehindLeft, pBehindRight, pFrontCenter, pBehindCenter := plays[0], plays[1], plays[2], plays[3], plays[4]

	// Behind-left is quieter than equidistant front-left and panned left identically.
	if !(pBehindLeft.Volume < pFrontLeft.Volume) {
		t.Errorf("behind-left volume %v not quieter than front-left %v", pBehindLeft.Volume, pFrontLeft.Volume)
	}
	if !(pBehindLeft.Pan < -0.5) || !near(pBehindLeft.Pan, pFrontLeft.Pan) {
		t.Errorf("behind-left pan %v, front-left pan %v; want matching negative pan near -0.707", pBehindLeft.Pan, pFrontLeft.Pan)
	}

	// Behind-right is quieter than front-left (same distance) and panned right symmetrically.
	if !near(pBehindRight.Volume, pBehindLeft.Volume) {
		t.Errorf("behind-right volume %v != behind-left %v", pBehindRight.Volume, pBehindLeft.Volume)
	}
	if !near(pBehindRight.Pan, -pBehindLeft.Pan) || !(pBehindRight.Pan > 0.5) {
		t.Errorf("behind-right pan %v, want symmetric positive pan near +0.707", pBehindRight.Pan)
	}

	// Directly behind is DefaultRearGain (0.7x) of directly front and centered.
	if !near(pBehindCenter.Volume, pFrontCenter.Volume*sound.DefaultRearGain) {
		t.Errorf("behind-center volume %v, want %v (%v * %v)", pBehindCenter.Volume, pFrontCenter.Volume*sound.DefaultRearGain, pFrontCenter.Volume, sound.DefaultRearGain)
	}
	if !near(pFrontCenter.Pan, 0) || !near(pBehindCenter.Pan, 0) {
		t.Errorf("center pans = %v, %v; want 0", pFrontCenter.Pan, pBehindCenter.Pan)
	}
}

func TestListener3DPanAndOrientation(t *testing.T) {
	// Rotated camera: facing +X (turned 90° clockwise around +Y), so right is +Z and left is -Z.
	turned := sound.Listener3D{
		Position: sound.V3(0, 0, 0),
		Forward:  sound.V3(1, 0, 0),
		Up:       sound.V3(0, 1, 0),
		Near:     1,
		Far:      50,
	}
	// Camera looking straight up along +Y with collinear Up=(0,1,0) exercises fallback basis.
	straightUp := sound.Listener3D{
		Forward: sound.V3(0, 1, 0),
		Up:      sound.V3(0, 1, 0),
	}
	panWidth := sound.Listener3D{
		Far:      100,
		PanWidth: 20,
	}

	for _, c := range []struct {
		name string
		l    sound.Listener3D
		pos  sound.Vec3
		want float32
	}{
		{"at listener", listener3D, sound.V3(0, 0, 0), 0},
		{"straight ahead", listener3D, sound.V3(0, 0, -10), 0},
		{"straight behind", listener3D, sound.V3(0, 0, 10), 0},
		{"directly overhead", listener3D, sound.V3(0, 10, 0), 0},
		{"directly underfoot", listener3D, sound.V3(0, -10, 0), 0},
		{"full left", listener3D, sound.V3(-10, 0, 0), -1},
		{"full right", listener3D, sound.V3(10, 0, 0), 1},
		{"45 deg front-right", listener3D, sound.V3(5, 0, -5), float32(math.Sqrt2 / 2)},
		{"45 deg behind-left", listener3D, sound.V3(-5, 0, 5), float32(-math.Sqrt2 / 2)},
		{"zero listener defaults to -Z forward", sound.Listener3D{}, sound.V3(-4, 0, 0), -1},
		{"turned facing +X, +Z is right", turned, sound.V3(0, 0, 10), 1},
		{"turned facing +X, -Z is left", turned, sound.V3(0, 0, -10), -1},
		{"collinear up fallback stays finite", straightUp, sound.V3(5, 0, 0), 1},
		{"PanWidth half right", panWidth, sound.V3(10, 0, -5), 0.5},
		{"PanWidth clamped left", panWidth, sound.V3(-40, 0, 0), -1},
		{"PanWidth overhead centered", panWidth, sound.V3(0, 50, 0), 0},
		{"NaN pos centered", listener3D, sound.V3(float32(math.NaN()), 0, 0), 0},
		{"+Inf X full right", listener3D, sound.V3(float32(math.Inf(1)), 0, 0), 1},
		{"-Inf X with PanWidth full left", panWidth, sound.V3(float32(math.Inf(-1)), 0, 0), -1},
		{"+Inf Y with PanWidth centered", panWidth, sound.V3(0, float32(math.Inf(1)), 0), 0},
		{"NaN forward falls back to -Z", sound.Listener3D{Forward: sound.V3(float32(math.NaN()), 0, 0)}, sound.V3(5, 0, 0), 1},
	} {
		if got := c.l.Pan(c.pos); !near(got, c.want) {
			t.Errorf("%s: Pan(%v) = %v, want %v", c.name, c.pos, got, c.want)
		}
	}
}

func TestListener3DGainAndRolloffModels(t *testing.T) {
	linear := sound.Listener3D{Near: 10, Far: 110, Rolloff: sound.RolloffLinear, RearGain: 1}
	inverse := sound.Listener3D{Near: 10, Far: 110, Rolloff: sound.RolloffInverse, RearGain: 1}
	expDefault := sound.Listener3D{Near: 10, Far: 110, Rolloff: sound.RolloffExponential, RearGain: 1}
	expFactor3 := sound.Listener3D{Near: 10, Far: 110, Rolloff: sound.RolloffExponential, RolloffFactor: 3, RearGain: 1}
	expNoNear := sound.Listener3D{Near: 0, Far: 100, Rolloff: sound.RolloffExponential, RearGain: 1}
	customRear := sound.Listener3D{Near: 10, Far: 110, RearGain: 0.5}

	for _, c := range []struct {
		name string
		l    sound.Listener3D
		pos  sound.Vec3
		want float32
	}{
		{"at listener", listener3D, sound.V3(0, 0, 0), 1},
		{"within Near", listener3D, sound.V3(0, 0, -2), 1},
		{"halfway linear front", listener3D, sound.V3(0, 0, -12), 0.5},
		{"halfway linear overhead", listener3D, sound.V3(0, 12, 0), 0.5},
		{"halfway linear behind (default rear 0.7)", listener3D, sound.V3(0, 0, 12), 0.35},
		{"at Far", listener3D, sound.V3(0, 0, -22), 0},
		{"beyond Far", listener3D, sound.V3(0, 0, -100), 0},
		{"linear at d=20", linear, sound.V3(0, 0, -20), 0.9},
		// (10/20 - 10/110) / (1 - 10/110) = 0.45
		{"inverse at d=20", inverse, sound.V3(0, 0, -20), 0.45},
		// ((10/20)^2 - (10/110)^2) / (1 - (10/110)^2) = 29.25 / 120 = 0.24375
		{"exponential default (k=2) at d=20", expDefault, sound.V3(0, 0, -20), 0.24375},
		{"exponential k=3 steeper than k=2", expFactor3, sound.V3(0, 0, -20), float32((math.Pow(0.5, 3) - math.Pow(10.0/110.0, 3)) / (1 - math.Pow(10.0/110.0, 3)))},
		// ((100-50)/100)^2 = 0.25
		{"exponential without Near at d=50", expNoNear, sound.V3(0, 0, -50), 0.25},
		{"attenuation off front", sound.Listener3D{}, sound.V3(0, 0, -1e5), 1},
		{"attenuation off behind still applies RearGain", sound.Listener3D{}, sound.V3(0, 0, 1e5), sound.DefaultRearGain},
		{"RearGain=1 disables rear attenuation", linear, sound.V3(0, 0, 10), 1},
		{"custom RearGain=0.5 directly behind", customRear, sound.V3(0, 0, 10), 0.5},
		{"NaN pos silent", listener3D, sound.V3(0, float32(math.NaN()), 0), 0},
		{"NaN pos silent even with attenuation off", sound.Listener3D{}, sound.V3(float32(math.NaN()), 0, 0), 0},
		{"infinite pos silent with attenuation on", listener3D, sound.V3(0, 0, float32(math.Inf(-1))), 0},
		{"infinite pos audible with attenuation off", sound.Listener3D{RearGain: 1}, sound.V3(0, 0, float32(math.Inf(-1))), 1},
	} {
		if got := c.l.Gain(c.pos); !near(got, c.want) {
			t.Errorf("%s: Gain(%v) = %v, want %v", c.name, c.pos, got, c.want)
		}
	}

	// Verify exponential < inverse < linear strictly at the same intermediate distance.
	dPos := sound.V3(0, 0, -20)
	if !(expDefault.Gain(dPos) < inverse.Gain(dPos) && inverse.Gain(dPos) < linear.Gain(dPos)) {
		t.Errorf("expected exp (%v) < inverse (%v) < linear (%v)", expDefault.Gain(dPos), inverse.Gain(dPos), linear.Gain(dPos))
	}
}

func TestSpatial3DSourceConeAndOverrides(t *testing.T) {
	// Omnidirectional listener at origin with distance attenuation off and RearGain=1 to isolate cone math.
	l := sound.Listener3D{RearGain: 1}
	cone := sound.Spatial3DOptions{
		SourceForward:  sound.V3(0, 0, 1), // source points toward +Z (toward listener at origin when source is at -Z)
		InnerConeAngle: math.Pi / 2,       // 90° full, 45° half
		OuterConeAngle: math.Pi,           // 180° full, 90° half
		OuterGain:      0.25,
	}

	// Source at (0, 0, -5) pointing +Z directly at listener at origin: angle = 0° -> gain 1.
	if sp := sound.Spatialize3D(l, sound.V3(0, 0, -5), cone); !near(sp.Gain, 1) || !near(sp.Distance, 5) {
		t.Errorf("inside inner cone = %+v, want gain 1 dist 5", sp)
	}
	// Source at (0, 0, 5) pointing +Z directly away from listener at origin: angle = 180° -> OuterGain 0.25.
	if sp := l.Spatialize(sound.V3(0, 0, 5), cone); !near(sp.Gain, 0.25) {
		t.Errorf("outside outer cone = %+v, want gain 0.25", sp)
	}
	// Source at (-5, 0, -5) pointing +Z: toListener = (+5, 0, +5), angle = 45° == innerHalf -> gain 1.
	if sp := l.Spatialize(sound.V3(-5, 0, -5), cone); !near(sp.Gain, 1) {
		t.Errorf("at inner edge = %+v, want gain 1", sp)
	}
	// Source at (-5, 0, 0) pointing +Z: toListener = (+5, 0, 0), angle = 90° == outerHalf -> OuterGain 0.25.
	if sp := l.Spatialize(sound.V3(-5, 0, 0), cone); !near(sp.Gain, 0.25) {
		t.Errorf("at outer edge = %+v, want gain 0.25", sp)
	}
	// Source at angle 67.5° (midway between 45° and 90°): gain = (1 + 0.25) / 2 = 0.625.
	rad := 67.5 * math.Pi / 180
	srcPos := sound.V3(float32(-5*math.Sin(rad)), 0, float32(-5*math.Cos(rad)))
	if sp := l.Spatialize(srcPos, cone); !near(sp.Gain, 0.625) {
		t.Errorf("mid-cone = %+v, want gain 0.625", sp)
	}

	// Per-source distance overrides take precedence over listener's Near/Far/Rolloff.
	override := sound.Spatial3DOptions{
		Near:       1,
		Far:        11,
		HasRolloff: true,
		Rolloff:    sound.RolloffLinear,
	}
	if sp := l.Spatialize(sound.V3(0, 0, -6), override); !near(sp.Gain, 0.5) {
		t.Errorf("override distance = %+v, want gain 0.5", sp)
	}
}

func TestListener3DSetPoseAndLookAt(t *testing.T) {
	l := sound.Listener3D{Near: 2, Far: 22, RearGain: 0.6}
	l.SetPose(sound.V3(5, 0, 0), sound.V3(0, 0, -1), sound.V3(0, 1, 0))
	if l.Near != 2 || l.Far != 22 || l.RearGain != 0.6 || l.Position != sound.V3(5, 0, 0) {
		t.Fatalf("SetPose clobbered fields: %+v", l)
	}
	// LookAt +X from (5,0,0): right becomes +Z.
	l.LookAt(sound.V3(15, 0, 0), sound.V3(0, 1, 0))
	if !near(l.Pan(sound.V3(5, 0, 10)), 1) {
		t.Errorf("after LookAt(+X), Pan at +Z = %v, want 1", l.Pan(sound.V3(5, 0, 10)))
	}
	// Degenerate LookAt (target == Position) preserves existing Forward.
	prevFwd := l.Forward
	l.LookAt(l.Position, sound.V3(0, 1, 0))
	if l.Forward != prevFwd {
		t.Errorf("degenerate LookAt changed Forward from %v to %v", prevFwd, l.Forward)
	}
	// Nil receiver is safe.
	var nilL *sound.Listener3D
	nilL.SetPose(sound.V3(1, 2, 3), sound.V3(0, 0, -1), sound.V3(0, 1, 0))
	nilL.LookAt(sound.V3(1, 2, 3), sound.V3(0, 1, 0))
}

func TestMixerLivePlayback3DTracksListenerAndSourceMovement(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetListener3D(listener3D)
	if m.mixer.Listener3D() != listener3D {
		t.Fatalf("Listener3D() = %+v, want %+v", m.mixer.Listener3D(), listener3D)
	}

	// Start a looping 3D playback in front at (0, 0, -12): linear gain = 0.5, pan = 0.
	pb := m.click.Play3D(sound.V3(0, 0, -12), audio.Voice{Volume: 0.8, Loop: true}, sound.Spatial3DOptions{})
	if !pb.Playing() || pb.Ended() {
		t.Fatal("expected live Playback3D to be playing")
	}
	if !near(pb.Gain(), 0.5) || !near(pb.Pan(), 0) || !near(pb.Spatial().Distance, 12) {
		t.Fatalf("initial spatial = %+v, want gain 0.5 pan 0 dist 12", pb.Spatial())
	}
	// Initial backend playback volume = 0.8 * 0.5 = 0.4.
	raw := m.playbacks()[0]
	m.expectPlayback(t, raw, 0.4)

	// Move source behind-right to (6, 0, 6): distance = sqrt(72) ≈ 8.485, rear < 1, pan ≈ +0.707.
	pb.SetPosition(sound.V3(6, 0, 6))
	expectedBehind := listener3D.Spatialize(sound.V3(6, 0, 6), sound.Spatial3DOptions{})
	if pb.Position() != sound.V3(6, 0, 6) || !near(pb.Gain(), expectedBehind.Gain) || !near(pb.Pan(), expectedBehind.Pan) {
		t.Fatalf("after SetPosition: pos=%v spatial=%+v, want %+v", pb.Position(), pb.Spatial(), expectedBehind)
	}
	if !(pb.Pan() > 0.5) {
		t.Errorf("expected positive right pan, got %v", pb.Pan())
	}
	m.expectPlayback(t, raw, 0.8*expectedBehind.Gain)

	// Rotate camera 180° around +Y to face +Z: (6, 0, 6) is now in FRONT and to the LEFT!
	turned := listener3D
	turned.Forward = sound.V3(0, 0, 1)
	m.mixer.SetListener3D(turned)
	if !(pb.Gain() > expectedBehind.Gain) {
		t.Errorf("after turning camera toward source, gain %v should exceed rear gain %v", pb.Gain(), expectedBehind.Gain)
	}
	if !(pb.Pan() < -0.5) {
		t.Errorf("after turning 180°, pan %v should flip to left", pb.Pan())
	}
	m.expectPlayback(t, raw, 0.8*pb.Gain())

	// Move listener beyond Far: live playback silences to 0 without stopping.
	farAway := turned
	farAway.Position = sound.V3(0, 0, -100)
	m.mixer.SetListener3D(farAway)
	if pb.Ended() || pb.Gain() != 0 {
		t.Fatalf("beyond Far: ended=%v gain=%v, want playing silently at gain 0", pb.Ended(), pb.Gain())
	}
	m.expectPlayback(t, raw, 0)

	// Move listener right next to source: volume returns to full baseVolume (0.8).
	closeUp := turned
	closeUp.Position = sound.V3(6, 0, 5)
	m.mixer.SetListener3D(closeUp)
	if !near(pb.Gain(), 1) {
		t.Fatalf("close up gain = %v, want 1", pb.Gain())
	}
	m.expectPlayback(t, raw, 0.8)

	// Apply a directional cone pointing away from listener via SetOptions.
	cone := sound.Spatial3DOptions{
		SourceForward:  sound.V3(0, 0, 1), // listener is at z=5, source at z=6, so pointing +Z points away
		InnerConeAngle: math.Pi / 2,
		OuterConeAngle: math.Pi,
		OuterGain:      0.25,
	}
	pb.SetOptions(cone)
	if pb.Options() != cone || !near(pb.Gain(), 0.25) {
		t.Fatalf("after SetOptions: opts=%+v gain=%v, want gain 0.25", pb.Options(), pb.Gain())
	}
	m.expectPlayback(t, raw, 0.8*0.25)

	// SetRate updates backend playback rate.
	pb.SetRate(1.5)
	m.app.Settle()
	if !near(raw.Rate, 1.5) {
		t.Errorf("raw.Rate = %v, want 1.5", raw.Rate)
	}

	// Stop ends the playback cleanly.
	pb.Stop()
	m.app.Settle()
	if !pb.Ended() || pb.Playing() || !raw.Ended {
		t.Fatalf("after Stop: pb.Ended=%v raw.Ended=%v, want true", pb.Ended(), raw.Ended)
	}
}

func TestPlayback3DRespectsMixerVolumePauseSuspendAndBackground(t *testing.T) {
	h := startPauseHarness(t, nil)
	h.mixer.SetPauseExempt("ui", true)
	h.mixer.SetListener3D(listener3D)

	// Start a gameplay 3D loop and a pause-exempt UI 3D loop at (0, 0, -12) (gain = 0.5).
	sfxPB := h.longSFX.Play3D(sound.V3(0, 0, -12), audio.Voice{Volume: 0.8, Loop: true}, sound.Spatial3DOptions{})
	uiPB := h.uiFX.Play3D(sound.V3(0, 0, -12), audio.Voice{Volume: 1.0, Loop: true}, sound.Spatial3DOptions{})
	if !sfxPB.Playing() || !uiPB.Playing() {
		t.Fatal("expected both 3D playbacks to start")
	}

	sfxRaw := h.soundByURI(t, longSFXURI).Playbacks[0]
	uiRaw := h.soundByURI(t, uiClickURI).Playbacks[0]

	checkVol := func(label string, p *nsgotest.AudioPlayback, want float32) {
		t.Helper()
		h.app.Settle()
		if p.Ended || !near(p.Volume, want) {
			t.Fatalf("%s: ended=%v volume=%v, want playing at %v", label, p.Ended, p.Volume, want)
		}
	}

	checkVol("initial sfx", sfxRaw, 0.4)
	checkVol("initial ui", uiRaw, 0.5)

	// Group & master volume scale live 3D playbacks.
	h.mixer.SetVolume("effects", 0.5)
	h.mixer.SetMaster(0.5)
	checkVol("scaled sfx", sfxRaw, 0.1) // 0.8 * 0.5 * 0.5 * 0.5
	checkVol("scaled ui", uiRaw, 0.25)  // 1.0 * 0.5 * 1.0 * 0.5

	// Pause silences gameplay 3D playback while keeping exempt UI 3D playback audible.
	h.mixer.SetPaused(true)
	checkVol("paused sfx", sfxRaw, 0)
	checkVol("paused ui", uiRaw, 0.25)

	// Moving the source or listener while paused keeps gameplay silenced and updates UI live.
	sfxPB.SetPosition(sound.V3(0, 0, -2)) // gain rises to 1.0
	uiPB.SetPosition(sound.V3(0, 0, -2))  // gain rises to 1.0
	checkVol("moved while paused sfx", sfxRaw, 0)
	checkVol("moved while paused ui", uiRaw, 0.5) // 1.0 * 1.0 * 1.0 * 0.5

	// New gameplay Play3D while paused does not start.
	blocked := h.sfx.Play3D(sound.V3(0, 0, -2), audio.Voice{}, sound.Spatial3DOptions{})
	if !blocked.Ended() {
		t.Fatal("gameplay Play3D started while paused")
	}

	// Resume restores gameplay 3D playback at its UPDATED 3D position gain (1.0).
	h.mixer.SetPaused(false)
	checkVol("resumed sfx at new position", sfxRaw, 0.2) // 0.8 * 1.0 * 0.5 * 0.5

	// Suspend silences all 3D playbacks including pause-exempt groups, and resume restores them.
	resume := h.mixer.Suspend()
	checkVol("suspended sfx", sfxRaw, 0)
	checkVol("suspended ui", uiRaw, 0)
	resume()
	checkVol("unsuspended sfx", sfxRaw, 0.2)
	checkVol("unsuspended ui", uiRaw, 0.5)

	// Backgrounding silences all 3D playbacks and foreground restores them.
	h.app.SetEnvironment(core.Environment{Audio: true, Lifecycle: core.LifecycleBackground})
	checkVol("background sfx", sfxRaw, 0)
	checkVol("background ui", uiRaw, 0)
	h.app.SetEnvironment(core.Environment{Audio: true, Lifecycle: core.LifecycleActive})
	checkVol("foreground sfx", sfxRaw, 0.2)
	checkVol("foreground ui", uiRaw, 0.5)

	// Cleanup loops.
	sfxPB.Stop()
	uiPB.Stop()
}

func TestPlay3DRespectsLimitAndSkipsSilentSources(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetListener3D(listener3D)

	// Beyond Far or muted starts nothing and consumes no voice limit.
	for _, pos := range []sound.Vec3{sound.V3(0, 0, -22), sound.V3(100, 0, 0), sound.V3(float32(math.NaN()), 0, 0)} {
		pb := m.click.Play3D(pos, audio.Voice{}, sound.Spatial3DOptions{})
		if !pb.Ended() || pb.Playing() {
			t.Errorf("Play3D(%v) unexpectedly started", pos)
		}
	}
	if pb := m.click.Play3D(sound.V3(0, 0, -2), audio.Voice{Muted: true}, sound.Spatial3DOptions{}); !pb.Ended() {
		t.Error("muted Play3D unexpectedly started")
	}
	if len(m.plays()) != 0 {
		t.Fatalf("silent Play3D calls produced %d plays, want 0", len(m.plays()))
	}

	// Click limit is 4; playing 5 live 3D effects stops the oldest one.
	var handles []*sound.Playback3D
	for range 5 {
		handles = append(handles, m.click.Play3D(sound.V3(0, 0, -4), audio.Voice{}, sound.Spatial3DOptions{}))
	}
	m.app.Settle()
	if !handles[0].Ended() {
		t.Error("oldest Playback3D was not evicted when Limit was exceeded")
	}
	for i := 1; i < 5; i++ {
		if handles[i].Ended() {
			t.Errorf("Playback3D %d unexpectedly ended", i)
		}
	}

	// Natural completion ends handles cleanly.
	m.app.Advance(200 * time.Millisecond)
	for i := 1; i < 5; i++ {
		if !handles[i].Ended() {
			t.Errorf("Playback3D %d did not end after duration elapsed", i)
		}
	}

	// Nil Playback3D and nil Effect safety.
	var nilPB *sound.Playback3D
	if !nilPB.Ended() || nilPB.Playing() || nilPB.Gain() != 0 || nilPB.Pan() != 0 || nilPB.Position() != (sound.Vec3{}) {
		t.Error("nil Playback3D returned unexpected state")
	}
	nilPB.SetPosition(sound.V3(1, 2, 3))
	nilPB.SetOptions(sound.Spatial3DOptions{})
	nilPB.SetRate(1.2)
	nilPB.Stop()

	var nilFX *sound.Effect
	if nilFX.PlayAt3D(listener3D, sound.V3(0, 0, -2), audio.Voice{}) {
		t.Error("nil Effect.PlayAt3D returned true")
	}
	if !nilFX.Play3D(sound.V3(0, 0, -2), audio.Voice{}, sound.Spatial3DOptions{}).Ended() {
		t.Error("nil Effect.Play3D returned non-ended Playback3D")
	}
}

func TestCuePlayAt3DAndPlay3D(t *testing.T) {
	m := startMixed(t)
	m.mixer.SetListener3D(listener3D)
	cue := sound.NewCue(7, sound.CueOptions{
		Clips:     []sound.Clip{sound.EffectClip(m.click)},
		PitchMin:  1.25,
		PitchMax:  1.25,
		VolumeMin: 0.5,
		VolumeMax: 0.5,
	})

	// Cue.PlayAt3D behind-left: quieter than front-left and panned left.
	if pick, ok := cue.PlayAt3D(listener3D, sound.V3(-6, 0, 6)); !ok || pick.Index != 0 {
		t.Fatalf("Cue.PlayAt3D = %+v, %v, want played", pick, ok)
	}
	if _, ok := cue.PlayAt3D(listener3D, sound.V3(0, 0, -100)); ok {
		t.Error("Cue.PlayAt3D beyond Far unexpectedly played")
	}

	// Cue.Play3D returns live Playback3D.
	pick, pb := cue.Play3D(sound.V3(0, 0, -12), sound.Spatial3DOptions{})
	if !pick.Valid() || !pb.Playing() || !near(pb.Gain(), 0.5) {
		t.Fatalf("Cue.Play3D = %+v, playing=%v gain=%v", pick, pb.Playing(), pb.Gain())
	}
	plays := m.plays()
	if len(plays) != 2 || !near(plays[1].Volume, 0.25) || !near(plays[1].Rate, 1.25) {
		t.Fatalf("plays = %+v, want second play at volume 0.25 rate 1.25", plays)
	}

	var nilCue *sound.Cue
	if p, ok := nilCue.PlayAt3D(listener3D, sound.V3(0, 0, -2)); ok || p.Index != -1 {
		t.Errorf("nil Cue.PlayAt3D = %+v, %v", p, ok)
	}
	if p, h := nilCue.Play3D(sound.V3(0, 0, -2), sound.Spatial3DOptions{}); p.Index != -1 || !h.Ended() {
		t.Errorf("nil Cue.Play3D = %+v, ended=%v", p, h.Ended())
	}
}

func TestListener3DVoiceMutedWhenSilent(t *testing.T) {
	for _, c := range []struct {
		name string
		pos  sound.Vec3
		v    audio.Voice
	}{
		{"beyond Far", sound.V3(0, 0, -100), audio.Voice{}},
		{"NaN", sound.V3(float32(math.NaN()), 0, 0), audio.Voice{}},
		{"negative voice volume", sound.V3(0, 0, -2), audio.Voice{Volume: -1}},
	} {
		if v := listener3D.Voice(c.pos, c.v); v.Volume != 0 || !v.Muted {
			t.Errorf("%s: Voice = %+v, want volume 0 and muted", c.name, v)
		}
	}
	if v := listener3D.Voice(sound.V3(0, 0, -2), audio.Voice{}); v.Muted || v.Volume != 1 {
		t.Errorf("Voice within Near = %+v, want unmuted at volume 1", v)
	}
}

func FuzzListener3D(f *testing.F) {
	f.Add(float32(0), float32(0), float32(0), float32(0), float32(0), float32(-1), float32(0), float32(1), float32(0), float32(2), float32(22), float32(0), float32(0), float32(0), uint8(0), float32(-6), float32(0), float32(6))
	f.Add(float32(1e30), float32(-1e30), float32(0), float32(0), float32(1), float32(0), float32(0), float32(1), float32(0), float32(0), float32(10), float32(2), float32(0.5), float32(20), uint8(2), float32(1e38), float32(-1e38), float32(0))
	f.Fuzz(func(t *testing.T, lx, ly, lz, fx, fy, fz, ux, uy, uz, nearD, farD, factor, rear, pw float32, rolloff uint8, px, py, pz float32) {
		l := sound.Listener3D{
			Position:      sound.V3(lx, ly, lz),
			Forward:       sound.V3(fx, fy, fz),
			Up:            sound.V3(ux, uy, uz),
			Near:          nearD,
			Far:           farD,
			Rolloff:       sound.Rolloff(rolloff % 3),
			RolloffFactor: factor,
			RearGain:      rear,
			PanWidth:      pw,
		}
		pos := sound.V3(px, py, pz)
		p := l.Pan(pos)
		if !(p >= -1 && p <= 1) {
			t.Fatalf("%+v: Pan(%v) = %v, want within [-1, 1]", l, pos, p)
		}
		g := l.Gain(pos)
		if !(g >= 0 && g <= 1) {
			t.Fatalf("%+v: Gain(%v) = %v, want within [0, 1]", l, pos, g)
		}
		v := l.Voice(pos, audio.Voice{})
		if v.Volume != g || v.Pan != p || v.Muted != (g == 0) {
			t.Fatalf("%+v: Voice(%v) = %+v, want volume %v pan %v muted %v", l, pos, v, g, p, g == 0)
		}
	})
}

func BenchmarkListener3DVoice(b *testing.B) {
	l := sound.Listener3D{
		Position: sound.V3(10, 2, 5),
		Forward:  sound.V3(0, 0, -1),
		Up:       sound.V3(0, 1, 0),
		Near:     2,
		Far:      100,
		Rolloff:  sound.RolloffInverse,
	}
	pos := sound.V3(-12, 4, 25)
	b.ReportAllocs()
	var v audio.Voice
	for b.Loop() {
		v = l.Voice(pos, audio.Voice{})
	}
	if !(v.Volume > 0) {
		b.Fatalf("Voice = %+v, want audible", v)
	}
}

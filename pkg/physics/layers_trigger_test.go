package physics_test

import (
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	layerPlayer physics.LayerMask = 1 << 0
	layerEnemy  physics.LayerMask = 1 << 1
	layerBullet physics.LayerMask = 1 << 2
)

func TestCollisionLayersAndMasks(t *testing.T) {
	// Zero Layer/Mask defaults to LayerAll for backwards compatibility.
	defaultA := physics.NewBoxBody(0, 0, 10, 10)
	defaultB := physics.NewBoxBody(5, 5, 10, 10)
	if !physics.CanCollide(defaultA, defaultB) {
		t.Fatalf("bodies with zero Layer/Mask should collide by default")
	}
	if physics.CanCollide(nil, defaultB) || physics.CanCollide(defaultA, nil) {
		t.Fatalf("nil body should never collide")
	}

	if physics.LayerBit(0) != layerPlayer || physics.LayerBit(2) != layerBullet || physics.LayerBit(32) != 0 {
		t.Fatalf("unexpected LayerBit values")
	}

	player := physics.NewBoxBody(0, 0, 16, 16)
	player.Layer = layerPlayer
	player.Mask = layerEnemy

	enemy := physics.NewBoxBody(0, 0, 16, 16)
	enemy.Layer = layerEnemy
	enemy.Mask = layerPlayer | layerBullet

	bullet := physics.NewBoxBody(0, 0, 4, 4)
	bullet.Layer = layerBullet
	bullet.Mask = layerEnemy

	// Player <-> Enemy: both accept each other.
	if !physics.CanCollide(player, enemy) {
		t.Fatalf("expected player and enemy to collide")
	}
	// Enemy <-> Bullet: both accept each other.
	if !physics.CanCollide(enemy, bullet) {
		t.Fatalf("expected enemy and bullet to collide")
	}
	// Player <-> Bullet: neither accepts the other.
	if physics.CanCollide(player, bullet) {
		t.Fatalf("expected player and bullet not to collide")
	}
	// Asymmetric mask: if bullet adds player to its mask, player still excludes bullet.
	bullet.Mask |= layerPlayer
	if physics.CanCollide(player, bullet) {
		t.Fatalf("asymmetric masks should not collide")
	}
	// Default-layer body collides with any body whose mask includes all (or any bit).
	if !physics.CanCollide(defaultA, player) {
		t.Fatalf("default body (LayerAll) should collide with player")
	}
}

func TestWorldLayerFilteredQueriesAndRaycasts(t *testing.T) {
	grid := physics.NewGrid(8, 2, 16, 16)
	grid.Set(6, 0, physics.TileSolid) // x=96..112

	player := physics.NewBoxBody(16, 0, 16, 16)
	player.Tag = "player"
	player.Layer = layerPlayer

	enemy := physics.NewBoxBody(48, 0, 16, 16)
	enemy.Tag = "enemy"
	enemy.Layer = layerEnemy

	untagged := physics.NewBoxBody(80, 0, 16, 16)
	untagged.Tag = "default"
	// Layer == 0 => LayerAll

	w := physics.World{Tiles: grid}
	w.Add(player, enemy, untagged)

	probe := physics.BoxShape(physics.Box{X: 0, Y: 0, W: 120, H: 16})

	// Zero layerMask behaves like LayerAll.
	allHits := w.QueryOverlapLayer(probe, physics.QueryBodies, 0)
	if len(allHits) != 3 {
		t.Fatalf("QueryOverlapLayer(0) = %d hits, want 3", len(allHits))
	}

	// Only enemies (plus untagged default-layer body which matches LayerAll).
	enemyHits := w.QueryOverlapLayer(probe, physics.QueryBodies, layerEnemy)
	if len(enemyHits) != 2 || enemyHits[0].Body != enemy || enemyHits[1].Body != untagged {
		t.Fatalf("QueryOverlapLayer(layerEnemy) = %+v, want [enemy, untagged]", enemyHits)
	}

	// Point query with layer filter.
	if got := w.QueryPointLayer(20, 8, layerPlayer); len(got) != 1 || got[0] != player {
		t.Fatalf("QueryPointLayer(layerPlayer) = %+v, want [player]", got)
	}
	if got := w.QueryPointLayer(20, 8, layerEnemy); len(got) != 0 {
		t.Fatalf("QueryPointLayer(layerEnemy) = %+v, want empty", got)
	}

	// RaycastLayer filters bodies while still hitting tiles when QueryTiles is set.
	from := core.Point{X: 0, Y: 8}
	to := core.Point{X: 120, Y: 8}
	rayEnemy := w.RaycastLayer(from, to, physics.QueryAll, layerEnemy)
	if len(rayEnemy) != 3 {
		t.Fatalf("RaycastLayer(layerEnemy) = %d hits, want 3 (enemy, untagged, tile): %+v", len(rayEnemy), rayEnemy)
	}
	if rayEnemy[0].Body != enemy || rayEnemy[1].Body != untagged || !rayEnemy[2].Tile || rayEnemy[2].Col != 6 {
		t.Fatalf("unexpected RaycastLayer hits: %+v", rayEnemy)
	}

	// RaycastFirstLayer skips player and returns enemy first.
	first, ok := w.RaycastFirstLayer(from, to, physics.QueryBodies, layerEnemy)
	if !ok || first.Body != enemy {
		t.Fatalf("RaycastFirstLayer(layerEnemy) = %+v, %v, want enemy", first, ok)
	}
}

func TestTriggerEnterStayExitAndRemoval(t *testing.T) {
	var log []string

	b1 := physics.NewBoxBody(0, 0, 10, 10)
	b1.Tag = "hero"
	b1.Layer = layerPlayer

	b2 := physics.NewBoxBody(100, 0, 10, 10)
	b2.Tag = "goblin"
	b2.Layer = layerEnemy

	zone := &physics.Trigger{
		ID:    1,
		Tag:   "checkpoint",
		Shape: physics.BoxShape(physics.Box{X: 50, Y: 0, W: 30, H: 30}),
		Mask:  layerPlayer,
		OnEnter: func(b *physics.Body) {
			log = append(log, "enter:"+b.Tag)
		},
		OnStay: func(b *physics.Body) {
			log = append(log, "stay:"+b.Tag)
		},
		OnExit: func(b *physics.Body) {
			log = append(log, "exit:"+b.Tag)
		},
	}

	var w physics.World
	w.Add(b1, b2)
	w.AddTrigger(zone)

	// Step 1: neither overlaps.
	w.StepTriggers()
	if len(log) != 0 {
		t.Fatalf("step 1 log = %v, want empty", log)
	}

	// Step 2: b1 enters, b2 also moves inside but is filtered out by Mask=layerPlayer.
	b1.Translate(55, 5)
	b2.Shape = physics.BoxShape(physics.Box{X: 55, Y: 5, W: 10, H: 10})
	w.StepTriggers()
	if want := []string{"enter:hero"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("step 2 log = %v, want %v", log, want)
	}

	// Step 3 & 4: b1 stays inside.
	w.StepTriggers()
	w.StepTriggers()
	if want := []string{"enter:hero", "stay:hero", "stay:hero"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("step 4 log = %v, want %v", log, want)
	}

	// Step 5: b1 moves out -> OnExit fires once.
	b1.Translate(100, 0)
	w.StepTriggers()
	if want := []string{"enter:hero", "stay:hero", "stay:hero", "exit:hero"}; !reflect.DeepEqual(log, want) {
		t.Fatalf("step 5 log = %v, want %v", log, want)
	}

	// Step 6: still outside -> no additional events.
	w.StepTriggers()
	if len(log) != 4 {
		t.Fatalf("step 6 should not fire extra events, got %v", log)
	}

	// Step 7: re-enter, then remove body from world -> OnExit fires on next step.
	b1.Shape = physics.BoxShape(physics.Box{X: 55, Y: 5, W: 10, H: 10})
	w.StepTriggers()
	w.Remove(b1)
	w.StepTriggers()
	w.StepTriggers() // should not double-fire exit

	want := []string{"enter:hero", "stay:hero", "stay:hero", "exit:hero", "enter:hero", "exit:hero"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("final log = %v, want %v", log, want)
	}

	// RemoveTrigger succeeds once and stops tracking.
	if !w.RemoveTrigger(zone) || w.RemoveTrigger(zone) {
		t.Fatalf("RemoveTrigger returned unexpected result")
	}
}

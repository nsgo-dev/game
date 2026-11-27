package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/ai/navbake"
	"github.com/nsgo-dev/game/pkg/flow"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/text"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	SaveSlot          = "slot1"
	SaveSchemaVersion = 1

	playerHitboxSize = 10
	enemyHitboxSize  = 10
	enemyPathRadius  = float32(enemyHitboxSize) * 0.5 * math.Sqrt2
	playerSpeed      = 68
	enemyPatrolSpeed = 34
	enemyChaseSpeed  = 48

	senseRadius     = 5.0 * TileSize  // 80 pts: hears footsteps around corners
	sightRadius     = 8.0 * TileSize  // 128 pts: unobstructed line-of-sight
	loseAggroRadius = 10.5 * TileSize // 168 pts: breaks chase when player escapes
	interactRange   = 28.0
	attackRange     = 28.0
	repathInterval  = 200 * time.Millisecond

	typewriterCharsPerSec = 48.0
)

// EnemyState identifies an enemy's current flow.Machine state.
type EnemyState string

const (
	EnemyPatrol   EnemyState = "patrol"
	EnemyChase    EnemyState = "chase"
	EnemyDefeated EnemyState = "defeated"
)

// DialoguePage is one page of spoken text in the dialogue box.
type DialoguePage struct {
	Speaker string
	Body    string
}

// DialogueState tracks the multi-page typewriter dialogue box.
type DialogueState struct {
	Open          bool
	Pages         []DialoguePage
	PageIndex     int
	RevealedChars int
	charAccum     float64
}

// CurrentPage returns the active dialogue page, or zero if closed.
func (d *DialogueState) CurrentPage() DialoguePage {
	if !d.Open || d.PageIndex < 0 || d.PageIndex >= len(d.Pages) {
		return DialoguePage{}
	}
	return d.Pages[d.PageIndex]
}

// VisibleText returns the typewriter-revealed prefix of the current page.
func (d *DialogueState) VisibleText() string {
	page := d.CurrentPage()
	if page.Body == "" {
		return ""
	}
	runes := []rune(page.Body)
	n := d.RevealedChars
	if n < 0 {
		n = 0
	}
	if n > len(runes) {
		n = len(runes)
	}
	return string(runes[:n])
}

// Enemy represents a skeleton guardian driven by flow.Machine and ai.AStar.
type Enemy struct {
	ID            string
	Name          string
	Body          *physics.Body
	PrevX, PrevY  float32
	Waypoints     []ai.Cell
	WaypointIndex int
	State         *flow.Machine[EnemyState]
	Alerted       bool
	Defeated      bool
	Path          []ai.Cell
	SmoothPath    []core.Point
	PathStep      int
	RepathTimer   time.Duration
	LastGoal      ai.Cell
	Anim          sprite.Animation
	FlipX         bool
}

// Center returns the center point of the enemy's collision box.
func (e *Enemy) Center() core.Point {
	b := e.Body.Bounds()
	return core.Point{
		X: core.Points(b.X + b.W*0.5),
		Y: core.Points(b.Y + b.H*0.5),
	}
}

// EnemySave is the JSON-persisted state of a single enemy.
type EnemySave struct {
	ID            string     `json:"id"`
	X             float32    `json:"x"`
	Y             float32    `json:"y"`
	State         EnemyState `json:"state"`
	Alerted       bool       `json:"alerted"`
	Defeated      bool       `json:"defeated"`
	WaypointIndex int        `json:"waypointIndex"`
}

// SaveData is the versioned v1 save payload stored via save.Store.
type SaveData struct {
	PlayerX        float32     `json:"playerX"`
	PlayerY        float32     `json:"playerY"`
	PlayerHP       int         `json:"playerHP"`
	PlayerMaxHP    int         `json:"playerMaxHP"`
	HasRelic       bool        `json:"hasRelic"`
	DoorUnlocked   bool        `json:"doorUnlocked"`
	QuestCompleted bool        `json:"questCompleted"`
	TalkedToElder  bool        `json:"talkedToElder"`
	VisitedRooms   []string    `json:"visitedRooms"`
	Enemies        []EnemySave `json:"enemies"`
}

// HUDState is a comparable snapshot pushed to native NSGo HUD signals.
type HUDState struct {
	HP              int
	MaxHP           int
	HasRelic        bool
	DoorUnlocked    bool
	QuestCompleted  bool
	RoomName        string
	VisitedCount    int
	Status          string
	HasSave         bool
	DialogueOpen    bool
	DialogueSpeaker string
	DialoguePage    int
	DialogueTotal   int
	AlertedCount    int
	DefeatedCount   int
}

// Game holds all CryptQuest simulation state, pathfinder, viewport, camera,
// dialogue box, input actions, and save store.
type Game struct {
	World    *World
	Viewport screen.Viewport
	Camera   tilemap.Camera
	Actions  *input.Actions
	// Touch is the view-space on-screen touch controls (floating stick plus Attack and Use buttons).
	Touch    *input.TouchControls
	ViewSize core.Size

	env        *core.Env
	stageFrame core.Bounds

	Player        *physics.Body
	PlayerPrevX   float32
	PlayerPrevY   float32
	PlayerFacingX float32
	PlayerFacingY float32
	PlayerHP      int
	PlayerMaxHP   int
	HurtCooldown  time.Duration
	AttackTimer   time.Duration
	PlayerAnim    sprite.Animation
	ElderAnim     sprite.Animation

	HasRelic       bool
	DoorUnlocked   bool
	QuestCompleted bool
	TalkedToElder  bool
	VisitedRooms   map[string]bool
	CurrentRoom    string
	Status         string

	Enemies   []*Enemy
	Dialogue  DialogueState
	DrawAlpha float64
	Elapsed   time.Duration

	// ShowNavMesh draws World.NavMesh over the crypt; N toggles it.
	ShowNavMesh bool

	astar        *ai.AStar
	store        *save.Store
	keyMoveX     float32
	keyMoveY     float32
	keyMoveTimer time.Duration
	batch        sprite.Batch
}

// NewGame builds a fresh playable CryptQuest instance backed by store.
func NewGame(store *save.Store) *Game {
	w := NewWorld()
	worldBounds := w.Map.WorldBounds()
	g := &Game{
		World:    w,
		Viewport: gameViewport,
		Camera: tilemap.Camera{
			Width:     VirtualWidth,
			Height:    VirtualHeight,
			Bounds:    &worldBounds,
			ShakeSeed: 42,
		},
		Actions:      input.NewActions(),
		VisitedRooms: make(map[string]bool),
		astar:        ai.NewAStar(),
		store:        store,
		batch:        sprite.Batch{Atlas: w.Atlas},
	}

	g.Actions.MapPoint = func(pt core.Point) (core.Point, bool) {
		if g.ViewSize.Width <= 0 || g.ViewSize.Height <= 0 {
			return pt, true
		}
		return g.Viewport.ViewToGame(g.ViewSize, pt)
	}
	gameProject.BindInput(g.Actions)

	g.Reset()
	return g
}

// Reset returns the dungeon, player, enemies, and dialogue to their initial state.
func (g *Game) Reset() {
	g.World.LockDoor()
	g.Player = physics.NewBoxBody(
		float32(g.World.PlayerSpawn.X)-playerHitboxSize*0.5,
		float32(g.World.PlayerSpawn.Y)-playerHitboxSize*0.5,
		playerHitboxSize,
		playerHitboxSize,
	)
	g.PlayerPrevX = g.Player.Bounds().X
	g.PlayerPrevY = g.Player.Bounds().Y
	g.PlayerFacingX = 1
	g.PlayerFacingY = 0
	g.PlayerHP = 5
	g.PlayerMaxHP = 5
	g.HurtCooldown = 0
	g.AttackTimer = 0
	g.HasRelic = false
	g.DoorUnlocked = false
	g.QuestCompleted = false
	g.TalkedToElder = false
	g.Dialogue = DialogueState{}
	g.keyMoveX = 0
	g.keyMoveY = 0
	g.keyMoveTimer = 0
	g.Elapsed = 0
	g.Actions.Reset()
	if g.Touch != nil {
		g.Touch.Wake()
	}

	g.PlayerAnim = sprite.Animation{
		Loop: true,
		Frames: []sprite.AnimationFrame{
			{Frame: g.World.Atlas.Frames["hero_0"], Duration: 220 * time.Millisecond},
			{Frame: g.World.Atlas.Frames["hero_1"], Duration: 220 * time.Millisecond},
		},
	}
	g.ElderAnim = sprite.Animation{
		Loop: true,
		Frames: []sprite.AnimationFrame{
			{Frame: g.World.Atlas.Frames["elder_0"], Duration: 480 * time.Millisecond},
			{Frame: g.World.Atlas.Frames["elder_1"], Duration: 480 * time.Millisecond},
		},
	}

	clear(g.VisitedRooms)
	g.recordCurrentRoom()

	g.Enemies = make([]*Enemy, 0, len(g.World.EnemySpawns))
	for _, sp := range g.World.EnemySpawns {
		g.Enemies = append(g.Enemies, g.newEnemy(sp))
	}

	p := g.PlayerCenter()
	g.Camera.Follow(p, 0, 0)
	g.Status = "Speak with Elder Aldric or explore the crypt for the Sun Relic."
}

func (g *Game) newEnemy(sp EnemySpawn) *Enemy {
	start := g.World.CellCenter(sp.Waypoints[0])
	e := &Enemy{
		ID:            sp.ID,
		Name:          sp.Name,
		Body:          physics.NewBoxBody(float32(start.X)-enemyHitboxSize*0.5, float32(start.Y)-enemyHitboxSize*0.5, enemyHitboxSize, enemyHitboxSize),
		Waypoints:     slices.Clone(sp.Waypoints),
		WaypointIndex: 1 % len(sp.Waypoints),
		Path:          make([]ai.Cell, 0, 64),
		SmoothPath:    make([]core.Point, 0, 64),
		Anim: sprite.Animation{
			Loop: true,
			Frames: []sprite.AnimationFrame{
				{Frame: g.World.Atlas.Frames["skel_0"], Duration: 260 * time.Millisecond},
				{Frame: g.World.Atlas.Frames["skel_1"], Duration: 260 * time.Millisecond},
			},
		},
	}
	e.PrevX = e.Body.Bounds().X
	e.PrevY = e.Body.Bounds().Y

	sm := flow.NewMachine(EnemyPatrol)
	sm.AddState(EnemyPatrol, flow.State{
		Enter: func() {
			e.RepathTimer = 0
			g.planEnemyPath(e, e.Waypoints[e.WaypointIndex])
		},
		Update: func(dt time.Duration) {
			g.stepEnemyPatrol(e, dt)
		},
	})
	sm.AddState(EnemyChase, flow.State{
		Enter: func() {
			e.Alerted = true
			e.RepathTimer = 0
			playerCell := g.PlayerCell()
			g.planEnemyPath(e, playerCell)
		},
		Update: func(dt time.Duration) {
			g.stepEnemyChase(e, dt)
		},
	})
	sm.AddState(EnemyDefeated, flow.State{
		Enter: func() {
			e.Defeated = true
			e.Path = e.Path[:0]
			e.SmoothPath = e.SmoothPath[:0]
			e.Body.VX, e.Body.VY = 0, 0
		},
	})
	sm.AddAnyTransition(EnemyDefeated, func() bool { return e.Defeated })
	sm.AddTransition(EnemyPatrol, EnemyChase, func() bool { return !e.Defeated && g.enemyDetectsPlayer(e) })
	sm.AddTransition(EnemyChase, EnemyPatrol, func() bool { return !e.Defeated && !g.enemyShouldKeepChasing(e) })
	sm.Start()
	e.State = sm
	return e
}

// PlayerCenter returns the world center of the player's collision box.
func (g *Game) PlayerCenter() core.Point {
	b := g.Player.Bounds()
	return core.Point{
		X: core.Points(b.X + b.W*0.5),
		Y: core.Points(b.Y + b.H*0.5),
	}
}

// PlayerCell returns the tile cell containing the player's center.
func (g *Game) PlayerCell() ai.Cell {
	p := g.PlayerCenter()
	return g.World.CellAt(float32(p.X), float32(p.Y))
}

// SetPlayerPos teleports the player's center to (x, y) and updates room tracking and camera.
func (g *Game) SetPlayerPos(x, y float32) {
	g.Player.Shape = physics.BoxShape(physics.Box{
		X: x - playerHitboxSize*0.5,
		Y: y - playerHitboxSize*0.5,
		W: playerHitboxSize,
		H: playerHitboxSize,
	})
	g.PlayerPrevX = g.Player.Bounds().X
	g.PlayerPrevY = g.Player.Bounds().Y
	g.recordCurrentRoom()
	g.Camera.Follow(g.PlayerCenter(), 0, 0)
}

// HandleKey processes hardware keyboard input from View.OnKeyDown or Scope.OnKey.
func (g *Game) HandleKey(key string) bool {
	handled := false
	switch key {
	case "w", "up":
		if !g.Dialogue.Open {
			g.nudgeMove(0, -1)
		}
		handled = true
	case "s", "down":
		if !g.Dialogue.Open {
			g.nudgeMove(0, 1)
		}
		handled = true
	case "a", "left":
		if !g.Dialogue.Open {
			g.nudgeMove(-1, 0)
		}
		handled = true
	case "d", "right":
		if !g.Dialogue.Open {
			g.nudgeMove(1, 0)
		}
		handled = true
	case "e", "space", "enter":
		if g.Dialogue.Open {
			g.AdvanceDialogue()
		} else {
			g.Interact()
		}
		handled = true
	case "f", "j":
		if !g.Dialogue.Open {
			g.Attack()
		}
		handled = true
	case "n":
		g.ShowNavMesh = !g.ShowNavMesh
		handled = true
	case "escape":
		if g.Dialogue.Open {
			g.CloseDialogue()
			handled = true
		}
	}
	if handled && g.Touch != nil {
		g.Touch.Tracker().OnKey(core.KeyEvent{Key: key})
	}
	return handled
}

func (g *Game) nudgeMove(dx, dy float32) {
	g.keyMoveX = dx
	g.keyMoveY = dy
	g.keyMoveTimer = 180 * time.Millisecond
}

// Update advances the simulation by one fixed step dt (60 Hz).
func (g *Game) Update(dt time.Duration) {
	if dt <= 0 {
		return
	}
	g.Elapsed += dt
	g.Actions.Step()
	if g.Touch != nil {
		g.Touch.Update(dt)
	}
	g.World.Map.Update(dt)
	g.ElderAnim.Update(dt)
	g.Camera.Update(dt)

	if g.HurtCooldown > 0 {
		g.HurtCooldown = max(0, g.HurtCooldown-dt)
	}
	if g.AttackTimer > 0 {
		g.AttackTimer = max(0, g.AttackTimer-dt)
	}

	if g.Dialogue.Open {
		g.updateDialogue(dt)
		return
	}

	if g.Actions.JustPressed("interact") {
		g.Interact()
	}
	if !g.Dialogue.Open && g.Actions.JustPressed("attack") {
		g.Attack()
	}
	if g.Dialogue.Open {
		return
	}

	g.updatePlayer(dt)
	for _, e := range g.Enemies {
		e.PrevX = e.Body.Bounds().X
		e.PrevY = e.Body.Bounds().Y
		e.State.Update(dt)
		if !e.Defeated {
			e.Anim.Update(dt)
			g.checkEnemyContact(e)
		}
	}

	g.Camera.Follow(g.PlayerCenter(), 8, dt)
}

func (g *Game) updateDialogue(dt time.Duration) {
	page := g.Dialogue.CurrentPage()
	totalRunes := utf8.RuneCountInString(page.Body)
	if g.Dialogue.RevealedChars >= totalRunes {
		return
	}
	g.Dialogue.charAccum += dt.Seconds() * typewriterCharsPerSec
	add := int(g.Dialogue.charAccum)
	if add > 0 {
		g.Dialogue.charAccum -= float64(add)
		g.Dialogue.RevealedChars = min(totalRunes, g.Dialogue.RevealedChars+add)
	}
}

func (g *Game) updatePlayer(dt time.Duration) {
	g.PlayerPrevX = g.Player.Bounds().X
	g.PlayerPrevY = g.Player.Bounds().Y

	// move_y is up-positive (matching TouchControls sticks), while world Y increases downward.
	mx := float32(g.Actions.Axis("move_x"))
	my := -float32(g.Actions.Axis("move_y"))
	if mx == 0 && my == 0 && g.keyMoveTimer > 0 {
		mx = g.keyMoveX
		my = g.keyMoveY
		g.keyMoveTimer = max(0, g.keyMoveTimer-dt)
		if g.keyMoveTimer == 0 {
			g.keyMoveX, g.keyMoveY = 0, 0
		}
	}

	if mx != 0 || my != 0 {
		l := float32(math.Hypot(float64(mx), float64(my)))
		dirX, dirY := mx/l, my/l
		speed := min(l, 1) * playerSpeed
		g.PlayerFacingX = dirX
		g.PlayerFacingY = dirY
		g.Player.VX = dirX * speed
		g.Player.VY = dirY * speed
		physics.MoveAndSlide(g.Player, g.World.Physics, dt)
		g.PlayerAnim.Update(dt)
	} else {
		g.Player.VX = 0
		g.Player.VY = 0
	}

	g.recordCurrentRoom()
	g.checkWorldTriggers()
}

func (g *Game) recordCurrentRoom() {
	cell := g.PlayerCell()
	room := RoomNameAt(cell.Col, cell.Row)
	g.CurrentRoom = room
	g.VisitedRooms[room] = true
}

func (g *Game) checkWorldTriggers() {
	p := g.PlayerCenter()
	if !g.HasRelic && distPoints(p, g.World.ChestPos) <= 16 {
		g.collectRelic()
	}
	if g.DoorUnlocked && !g.QuestCompleted && distPoints(p, g.World.ExitPos) <= 12 {
		g.QuestCompleted = true
		g.Status = "Victory! You escaped the crypt with the Sun Relic!"
	}
}

func (g *Game) collectRelic() {
	if g.HasRelic {
		return
	}
	g.HasRelic = true
	g.DoorUnlocked = true
	g.World.UnlockDoor()
	g.Status = "Sun Relic acquired! The Sanctum gate to the south is now unlocked."
}

// Interact triggers the nearest context-sensitive interaction:
// Elder NPC dialogue, Sun Relic chest, locked door inspection, or exit portal.
func (g *Game) Interact() {
	if g.Dialogue.Open {
		g.AdvanceDialogue()
		return
	}
	p := g.PlayerCenter()

	if distPoints(p, g.World.ElderPos) <= interactRange {
		g.TalkedToElder = true
		g.OpenDialogue(g.elderDialoguePages())
		g.Status = "Speaking with Elder Aldric..."
		return
	}

	if !g.HasRelic && distPoints(p, g.World.ChestPos) <= interactRange {
		g.collectRelic()
		return
	}

	doorPos := g.World.CellCenter(g.World.DoorCell)
	if !g.DoorUnlocked && distPoints(p, doorPos) <= interactRange {
		g.Status = "The Sanctum gate is sealed. Retrieve the Sun Relic in the Relic Chamber."
		return
	}

	if g.DoorUnlocked && distPoints(p, g.World.ExitPos) <= interactRange {
		g.QuestCompleted = true
		g.Status = "Victory! You escaped the crypt with the Sun Relic!"
		return
	}

	g.Status = "Nothing nearby to interact with. Approach Elder Aldric or the Relic Chest."
}

func (g *Game) elderDialoguePages() []DialoguePage {
	if g.HasRelic {
		return []DialoguePage{
			{
				Speaker: "Elder Aldric",
				Body:    "By the dawn! You recovered the Sun Relic from the northern chamber.",
			},
			{
				Speaker: "Elder Aldric",
				Body:    "The Sanctum gate in the southeast now stands open. Step into the emerald portal to claim victory!",
			},
		}
	}
	return []DialoguePage{
		{
			Speaker: "Elder Aldric",
			Body:    "Welcome, traveler. The ancient Sun Relic lies locked away in the northeastern Relic Chamber.",
		},
		{
			Speaker: "Elder Aldric",
			Body:    "Beware the restless skeletons in Bone Corridor—they sense footsteps and hunt around every stone pillar.",
		},
		{
			Speaker: "Elder Aldric",
			Body:    "Press F or Banish to shatter nearby skeletons, seize the Relic, and unlock the southern Sanctum Exit!",
		},
	}
}

// OpenDialogue opens the multi-page typewriter dialogue box on page 0.
func (g *Game) OpenDialogue(pages []DialoguePage) {
	if len(pages) == 0 {
		g.CloseDialogue()
		return
	}
	g.Actions.Reset()
	g.Dialogue = DialogueState{
		Open:          true,
		Pages:         slices.Clone(pages),
		PageIndex:     0,
		RevealedChars: min(1, utf8.RuneCountInString(pages[0].Body)),
	}
}

// AdvanceDialogue moves to the next dialogue page, or closes cleanly after the last page.
func (g *Game) AdvanceDialogue() {
	if !g.Dialogue.Open {
		return
	}
	if g.Dialogue.PageIndex+1 < len(g.Dialogue.Pages) {
		g.Dialogue.PageIndex++
		g.Dialogue.charAccum = 0
		g.Dialogue.RevealedChars = min(1, utf8.RuneCountInString(g.Dialogue.CurrentPage().Body))
		return
	}
	g.CloseDialogue()
}

// CloseDialogue closes the dialogue box cleanly.
func (g *Game) CloseDialogue() {
	wasOpen := g.Dialogue.Open
	g.Dialogue.Open = false
	g.Dialogue.Pages = nil
	g.Dialogue.PageIndex = 0
	g.Dialogue.RevealedChars = 0
	g.Dialogue.charAccum = 0
	if wasOpen {
		g.Actions.Reset()
		if g.Touch != nil {
			g.Touch.Wake()
		}
	}
	if !g.QuestCompleted {
		g.Status = "Explore the crypt and retrieve the Sun Relic."
	}
}

// Attack unleashes a short-range banish arc that defeats skeletons within attackRange.
func (g *Game) Attack() {
	g.AttackTimer = 220 * time.Millisecond
	p := g.PlayerCenter()
	hitCount := 0
	for _, e := range g.Enemies {
		if e.Defeated {
			continue
		}
		if distPoints(p, e.Center()) <= attackRange {
			e.Defeated = true
			e.State.SetState(EnemyDefeated)
			hitCount++
		}
	}
	if hitCount > 0 {
		g.Camera.Shake(2.5, 160*time.Millisecond)
		g.Status = fmt.Sprintf("Banished %d crypt skeleton(s)!", hitCount)
	} else {
		g.Status = "Swung banish ward—no skeleton in range."
	}
}

func (g *Game) enemyDetectsPlayer(e *Enemy) bool {
	ep := e.Center()
	pp := g.PlayerCenter()
	dist := distPoints(ep, pp)
	if dist <= senseRadius {
		return true
	}
	if dist <= sightRadius && g.World.HasLineOfSight(ep, pp) {
		return true
	}
	return false
}

func (g *Game) enemyShouldKeepChasing(e *Enemy) bool {
	return distPoints(e.Center(), g.PlayerCenter()) <= loseAggroRadius
}

var enemyPathOptions = ai.GridPathOptions{
	CellSize: TileSize,
	Radius:   enemyPathRadius,
}

func (g *Game) planEnemyPath(e *Enemy, goal ai.Cell) bool {
	ep := e.Center()
	start := g.World.CellAt(float32(ep.X), float32(ep.Y))
	// Reuse AStar internal search buffers, e.Path cell slice, and e.SmoothPath world-space waypoints.
	path, _, ok := g.astar.FindInto(e.Path, g.World.Nav, start, goal)
	if !ok {
		e.Path = e.Path[:0]
		e.SmoothPath = e.SmoothPath[:0]
		e.PathStep = 0
		return false
	}
	e.Path = path
	e.SmoothPath = ai.SmoothGridPathInto(e.SmoothPath, g.World.Nav, e.Path, enemyPathOptions)
	e.LastGoal = goal
	if len(e.SmoothPath) > 1 {
		e.PathStep = 1
	} else {
		e.PathStep = 0
	}
	return true
}

func (g *Game) stepEnemyPatrol(e *Enemy, dt time.Duration) {
	if len(e.Waypoints) == 0 {
		return
	}
	targetCell := e.Waypoints[e.WaypointIndex]
	targetPos := g.World.CellCenter(targetCell)
	if distPoints(e.Center(), targetPos) <= 3.5 {
		e.WaypointIndex = (e.WaypointIndex + 1) % len(e.Waypoints)
		targetCell = e.Waypoints[e.WaypointIndex]
		g.planEnemyPath(e, targetCell)
	} else if len(e.SmoothPath) == 0 || e.LastGoal != targetCell {
		g.planEnemyPath(e, targetCell)
	}
	g.followEnemyPath(e, enemyPatrolSpeed, dt)
}

func (g *Game) stepEnemyChase(e *Enemy, dt time.Duration) {
	e.RepathTimer -= dt
	playerCell := g.PlayerCell()
	if e.RepathTimer <= 0 || e.LastGoal != playerCell || len(e.SmoothPath) == 0 {
		g.planEnemyPath(e, playerCell)
		e.RepathTimer = repathInterval
	}
	g.followEnemyPath(e, enemyChaseSpeed, dt)
}

func (g *Game) followEnemyPath(e *Enemy, speed float32, dt time.Duration) {
	if len(e.SmoothPath) == 0 {
		e.Body.VX, e.Body.VY = 0, 0
		return
	}
	if e.PathStep >= len(e.SmoothPath) {
		e.PathStep = len(e.SmoothPath) - 1
	}
	ep := e.Center()
	wp := e.SmoothPath[e.PathStep]
	dx := float32(wp.X - ep.X)
	dy := float32(wp.Y - ep.Y)
	dist := float32(math.Hypot(float64(dx), float64(dy)))
	if dist <= 2.5 && e.PathStep+1 < len(e.SmoothPath) {
		e.PathStep++
		wp = e.SmoothPath[e.PathStep]
		dx = float32(wp.X - ep.X)
		dy = float32(wp.Y - ep.Y)
		dist = float32(math.Hypot(float64(dx), float64(dy)))
	}
	if dist <= 0.5 {
		e.Body.VX, e.Body.VY = 0, 0
		return
	}
	vx := (dx / dist) * speed
	vy := (dy / dist) * speed
	if vx < -1 {
		e.FlipX = true
	} else if vx > 1 {
		e.FlipX = false
	}
	e.Body.VX = vx
	e.Body.VY = vy
	physics.MoveAndSlide(e.Body, g.World.Physics, dt)
}

func (g *Game) checkEnemyContact(e *Enemy) {
	if g.HurtCooldown > 0 || g.QuestCompleted {
		return
	}
	if distPoints(e.Center(), g.PlayerCenter()) <= 11 {
		g.PlayerHP--
		g.HurtCooldown = 900 * time.Millisecond
		g.Camera.Shake(3.5, 220*time.Millisecond)
		if g.PlayerHP <= 0 {
			g.PlayerHP = g.PlayerMaxHP
			g.SetPlayerPos(float32(g.World.PlayerSpawn.X), float32(g.World.PlayerSpawn.Y))
			g.Status = "Struck down! Returned to Entry Hall with restored vitality."
		} else {
			g.Status = fmt.Sprintf("Hit by %s! (%d/%d HP)", e.Name, g.PlayerHP, g.PlayerMaxHP)
		}
	}
}

// Save writes the current game state as a v1 save into save.Store.
func (g *Game) Save() error {
	if g.store == nil {
		return errors.New("cryptquest: no save store configured")
	}
	p := g.PlayerCenter()
	visited := make([]string, 0, len(g.VisitedRooms))
	for room := range g.VisitedRooms {
		visited = append(visited, room)
	}
	slices.Sort(visited)

	enemies := make([]EnemySave, len(g.Enemies))
	for i, e := range g.Enemies {
		c := e.Center()
		enemies[i] = EnemySave{
			ID:            e.ID,
			X:             float32(c.X),
			Y:             float32(c.Y),
			State:         e.State.Current(),
			Alerted:       e.Alerted,
			Defeated:      e.Defeated,
			WaypointIndex: e.WaypointIndex,
		}
	}

	snap := SaveData{
		PlayerX:        float32(p.X),
		PlayerY:        float32(p.Y),
		PlayerHP:       g.PlayerHP,
		PlayerMaxHP:    g.PlayerMaxHP,
		HasRelic:       g.HasRelic,
		DoorUnlocked:   g.DoorUnlocked,
		QuestCompleted: g.QuestCompleted,
		TalkedToElder:  g.TalkedToElder,
		VisitedRooms:   visited,
		Enemies:        enemies,
	}
	if err := g.store.Save(SaveSlot, snap); err != nil {
		g.Status = fmt.Sprintf("Save failed: %v", err)
		return err
	}
	g.Status = fmt.Sprintf("Progress saved (%s, %d/%d rooms).", g.CurrentRoom, len(visited), TotalRooms)
	return nil
}

// Load restores game state from the v1 save in save.Store.
func (g *Game) Load() error {
	if g.store == nil {
		return errors.New("cryptquest: no save store configured")
	}
	var snap SaveData
	if err := g.store.Load(SaveSlot, &snap); err != nil {
		g.Status = "No saved game found."
		return err
	}

	g.CloseDialogue()
	g.HasRelic = snap.HasRelic
	g.DoorUnlocked = snap.DoorUnlocked
	if g.DoorUnlocked {
		g.World.UnlockDoor()
	} else {
		g.World.LockDoor()
	}
	g.QuestCompleted = snap.QuestCompleted
	g.TalkedToElder = snap.TalkedToElder
	g.PlayerHP = snap.PlayerHP
	g.PlayerMaxHP = snap.PlayerMaxHP
	if g.PlayerMaxHP <= 0 {
		g.PlayerMaxHP = 5
	}
	if g.PlayerHP <= 0 {
		g.PlayerHP = g.PlayerMaxHP
	}
	g.HurtCooldown = 0
	g.AttackTimer = 0
	g.keyMoveX, g.keyMoveY, g.keyMoveTimer = 0, 0, 0
	g.Actions.Reset()
	if g.Touch != nil {
		g.Touch.Wake()
	}

	clear(g.VisitedRooms)
	for _, r := range snap.VisitedRooms {
		g.VisitedRooms[r] = true
	}
	g.SetPlayerPos(snap.PlayerX, snap.PlayerY)

	byID := make(map[string]EnemySave, len(snap.Enemies))
	for _, es := range snap.Enemies {
		byID[es.ID] = es
	}
	for _, e := range g.Enemies {
		es, ok := byID[e.ID]
		if !ok {
			continue
		}
		e.Body.Shape = physics.BoxShape(physics.Box{
			X: es.X - enemyHitboxSize*0.5,
			Y: es.Y - enemyHitboxSize*0.5,
			W: enemyHitboxSize,
			H: enemyHitboxSize,
		})
		e.PrevX = e.Body.Bounds().X
		e.PrevY = e.Body.Bounds().Y
		e.Alerted = es.Alerted
		e.Defeated = es.Defeated
		if len(e.Waypoints) > 0 {
			e.WaypointIndex = es.WaypointIndex % len(e.Waypoints)
		}
		e.Path = e.Path[:0]
		e.SmoothPath = e.SmoothPath[:0]
		e.PathStep = 0
		switch es.State {
		case EnemyDefeated:
			e.Defeated = true
			e.State.SetState(EnemyDefeated)
		case EnemyChase:
			e.State.SetState(EnemyChase)
		default:
			e.State.SetState(EnemyPatrol)
		}
	}

	g.Status = fmt.Sprintf("Save loaded (%s, %d/%d rooms).", g.CurrentRoom, len(g.VisitedRooms), TotalRooms)
	return nil
}

// HasSave reports whether a save slot exists in storage.
func (g *Game) HasSave() bool {
	return g.store != nil && g.store.Exists(SaveSlot)
}

// SnapshotHUD returns a comparable summary for the native NSGo HUD.
func (g *Game) SnapshotHUD() HUDState {
	alerted := 0
	defeated := 0
	for _, e := range g.Enemies {
		if e.Defeated {
			defeated++
		} else if e.State.Current() == EnemyChase {
			alerted++
		}
	}
	speaker := ""
	page := 0
	total := 0
	if g.Dialogue.Open {
		speaker = g.Dialogue.CurrentPage().Speaker
		page = g.Dialogue.PageIndex + 1
		total = len(g.Dialogue.Pages)
	}
	return HUDState{
		HP:              g.PlayerHP,
		MaxHP:           g.PlayerMaxHP,
		HasRelic:        g.HasRelic,
		DoorUnlocked:    g.DoorUnlocked,
		QuestCompleted:  g.QuestCompleted,
		RoomName:        g.CurrentRoom,
		VisitedCount:    len(g.VisitedRooms),
		Status:          g.Status,
		HasSave:         g.HasSave(),
		DialogueOpen:    g.Dialogue.Open,
		DialogueSpeaker: speaker,
		DialoguePage:    page,
		DialogueTotal:   total,
		AlertedCount:    alerted,
		DefeatedCount:   defeated,
	}
}

// stageSafeArea returns the part of the window's safe-area insets that still
// overlaps the game stage, since the native HUD and status bar absorb the top
// and bottom insets on their own edges.
func (g *Game) stageSafeArea() core.Edges {
	if g.env == nil {
		return core.Edges{}
	}
	safe := g.env.SafeArea()
	win := g.env.Window()
	if g.stageFrame.Size.Width <= 0 || g.stageFrame.Size.Height <= 0 || win.Width <= 0 || win.Height <= 0 {
		return safe
	}
	rightEdge := g.stageFrame.Origin.X + g.stageFrame.Size.Width
	bottomEdge := g.stageFrame.Origin.Y + g.stageFrame.Size.Height
	return core.Edges{
		Left:   max(0, safe.Left-g.stageFrame.Origin.X),
		Top:    max(0, safe.Top-g.stageFrame.Origin.Y),
		Right:  max(0, rightEdge-(win.Width-safe.Right)),
		Bottom: max(0, bottomEdge-(win.Height-safe.Bottom)),
	}
}

// Draw renders the dungeon, sprites, A* path breadcrumbs, dialogue text, and
// view-space touch controls into d.
func (g *Game) Draw(d *core.Drawing, window core.Size) {
	if window.Width <= 0 || window.Height <= 0 {
		window = core.Size{Width: core.Points(VirtualWidth * 2), Height: core.Points(VirtualHeight * 2)}
	}
	g.ViewSize = window

	g.Viewport.Draw(d, window, func(d *core.Drawing, size core.Size) {
		// 1. Background fill
		d.SetFillColor(core.RGB(0x11121c))
		d.FillRect(0, 0, float32(size.Width), float32(size.Height))

		// 2. World-space pass (tilemap, A* chase path indicators, entities)
		d.Save()
		g.Camera.Apply(d)
		g.World.Map.Draw(d, &g.Camera)
		if g.ShowNavMesh {
			navbake.Draw(d, g.World.NavMesh, core.RGBA(0x74f4e0, 0.18), core.RGBA(0x74f4e0, 0.7), core.RGB(0xfada70), 0.75)
		}
		g.drawEnemyPaths(d)
		g.drawEntities(d)
		d.Restore()

		// 3. Virtual screen-space overlays
		g.drawRoomBadge(d)
		if g.Dialogue.Open {
			g.drawDialogueBox(d)
		} else {
			g.drawContextHint(d)
		}
		if g.QuestCompleted {
			g.drawVictoryBanner(d)
		}
	})

	// 4. View-space touch controls, anchored to the safe area outside the virtual canvas.
	if g.Touch != nil {
		g.Touch.Resize(window, g.stageSafeArea())
		if !g.Dialogue.Open {
			g.Touch.Draw(d)
		}
	}
}

func (g *Game) drawEnemyPaths(d *core.Drawing) {
	for _, e := range g.Enemies {
		if e.Defeated || e.State.Current() != EnemyChase || len(e.SmoothPath) < 2 {
			continue
		}
		d.Save()
		d.SetFillColor(core.RGBA(0xea3e44, 0.55))
		for i := e.PathStep; i < len(e.SmoothPath); i++ {
			c := e.SmoothPath[i]
			d.BeginPath().Arc(float32(c.X), float32(c.Y), 1.8, 0, 2*math.Pi, false).Fill()
		}
		d.Restore()
	}
}

func (g *Game) drawEntities(d *core.Drawing) {
	alpha := float32(g.DrawAlpha)
	sprites := g.batch.Sprites[:0]

	// Treasure chest and Sun Relic sparkle
	chestFrame := g.World.Atlas.Frames["chest_closed"]
	if g.HasRelic {
		chestFrame = g.World.Atlas.Frames["chest_open"]
	}
	chest := sprite.NewSprite(chestFrame)
	chest.X = float32(g.World.ChestPos.X) - TileSize*0.5
	chest.Y = float32(g.World.ChestPos.Y) - TileSize*0.5
	chest.Z = 10
	sprites = append(sprites, &chest)

	var relicIcon sprite.Sprite
	if !g.HasRelic {
		bob := float32(math.Sin(g.Elapsed.Seconds()*4.0)) * 2.0
		relicIcon = sprite.NewSprite(g.World.Atlas.Frames["relic"])
		relicIcon.X = float32(g.World.ChestPos.X) - TileSize*0.5
		relicIcon.Y = float32(g.World.ChestPos.Y) - TileSize - 2 + bob
		relicIcon.ScaleX = 0.75
		relicIcon.ScaleY = 0.75
		relicIcon.OriginX = TileSize * 0.5
		relicIcon.OriginY = TileSize * 0.5
		relicIcon.X += TileSize * 0.5
		relicIcon.Y += TileSize * 0.5
		relicIcon.Z = 25
		sprites = append(sprites, &relicIcon)
	}

	// Elder Aldric NPC
	elder := sprite.NewSprite(g.ElderAnim.Frame())
	elder.X = float32(g.World.ElderPos.X) - TileSize*0.5
	elder.Y = float32(g.World.ElderPos.Y) - TileSize*0.5
	elder.Z = 20
	sprites = append(sprites, &elder)

	var talkMark sprite.Sprite
	if !g.Dialogue.Open && (!g.TalkedToElder || distPoints(g.PlayerCenter(), g.World.ElderPos) <= interactRange) {
		bob := float32(math.Sin(g.Elapsed.Seconds()*5.0)) * 1.5
		talkMark = sprite.NewSprite(g.World.Atlas.Frames["talk_mark"])
		talkMark.X = float32(g.World.ElderPos.X) - TileSize*0.5
		talkMark.Y = float32(g.World.ElderPos.Y) - TileSize - 3 + bob
		talkMark.Z = 40
		sprites = append(sprites, &talkMark)
	}

	// Skeleton enemies
	enemySprites := make([]sprite.Sprite, len(g.Enemies))
	alertMarks := make([]sprite.Sprite, 0, len(g.Enemies))
	for i, e := range g.Enemies {
		b := e.Body.Bounds()
		ex := lerp(e.PrevX, b.X, alpha) + b.W*0.5 - TileSize*0.5
		ey := lerp(e.PrevY, b.Y, alpha) + b.H*0.5 - TileSize*0.5
		var frame sprite.Frame
		z := 22
		switch e.State.Current() {
		case EnemyDefeated:
			frame = g.World.Atlas.Frames["skel_defeated"]
			z = 5
		case EnemyChase:
			frame = g.World.Atlas.Frames["skel_alert"]
		default:
			frame = e.Anim.Frame()
		}
		sp := sprite.NewSprite(frame)
		sp.X, sp.Y = g.Viewport.Snap(ex, ey)
		sp.FlipX = e.FlipX
		sp.Z = z
		enemySprites[i] = sp
		sprites = append(sprites, &enemySprites[i])

		if e.State.Current() == EnemyChase {
			am := sprite.NewSprite(g.World.Atlas.Frames["alert_mark"])
			am.X = sp.X
			am.Y = sp.Y - 11
			am.Z = 45
			alertMarks = append(alertMarks, am)
			sprites = append(sprites, &alertMarks[len(alertMarks)-1])
		}
	}

	// Player hero
	pb := g.Player.Bounds()
	px := lerp(g.PlayerPrevX, pb.X, alpha) + pb.W*0.5 - TileSize*0.5
	py := lerp(g.PlayerPrevY, pb.Y, alpha) + pb.H*0.5 - TileSize*0.5
	hero := sprite.NewSprite(g.PlayerAnim.Frame())
	hero.X, hero.Y = g.Viewport.Snap(px, py)
	hero.FlipX = g.PlayerFacingX < 0
	if g.HurtCooldown > 0 && (g.HurtCooldown/(70*time.Millisecond))%2 == 0 {
		hero.Alpha = 0.45
	}
	hero.Z = 30
	sprites = append(sprites, &hero)

	var slash sprite.Sprite
	if g.AttackTimer > 0 {
		slash = sprite.NewSprite(g.World.Atlas.Frames["slash"])
		slash.X = hero.X + g.PlayerFacingX*10
		slash.Y = hero.Y + g.PlayerFacingY*10
		slash.FlipX = g.PlayerFacingX < 0
		slash.Z = 35
		sprites = append(sprites, &slash)
	}

	g.batch.Sprites = sprites
	g.batch.Draw(d)
}

func (g *Game) drawRoomBadge(d *core.Drawing) {
	d.Save()
	d.SetFillColor(core.RGBA(0x0b0c12, 0.78))
	d.BeginPath().RoundRect(6, 6, 108, 16, 4).Fill()
	d.SetStrokeColor(core.RGBA(0xd4a640, 0.55)).SetLineWidth(1)
	d.BeginPath().RoundRect(6, 6, 108, 16, 4).Stroke()
	text.DrawPlatform(d, g.CurrentRoom, 11, 17, text.PlatformStyle{
		Font:  core.Font{Size: 8.5, Weight: core.FontWeightBold},
		Color: core.RGB(0xf4e8c8),
	})
	d.Restore()
}

func (g *Game) drawContextHint(d *core.Drawing) {
	hint := g.nearbyHint()
	if hint == "" {
		return
	}
	d.Save()
	d.SetFillColor(core.RGBA(0x0d0f1a, 0.84))
	d.BeginPath().RoundRect(74, 174, 172, 18, 4).Fill()
	d.SetStrokeColor(core.RGBA(0xd4a640, 0.7)).SetLineWidth(1)
	d.BeginPath().RoundRect(74, 174, 172, 18, 4).Stroke()
	text.DrawPlatform(d, hint, 74, 186, text.PlatformStyle{
		Font:     core.Font{Size: 8, Weight: core.FontWeightMedium},
		Color:    core.RGB(0xfada70),
		Align:    text.AlignCenter,
		MaxWidth: 172,
	})
	d.Restore()
}

func (g *Game) nearbyHint() string {
	p := g.PlayerCenter()
	if distPoints(p, g.World.ElderPos) <= interactRange {
		return "[E] Speak with Elder Aldric"
	}
	if !g.HasRelic && distPoints(p, g.World.ChestPos) <= interactRange {
		return "[E] Claim the Sun Relic"
	}
	if !g.DoorUnlocked && distPoints(p, g.World.CellCenter(g.World.DoorCell)) <= interactRange {
		return "Locked: Find the Sun Relic"
	}
	if g.DoorUnlocked && !g.QuestCompleted && distPoints(p, g.World.ExitPos) <= interactRange {
		return "[E] Enter Sanctum Portal"
	}
	return ""
}

func (g *Game) drawDialogueBox(d *core.Drawing) {
	page := g.Dialogue.CurrentPage()
	if page.Body == "" {
		return
	}
	const (
		boxX = 14
		boxY = 116
		boxW = 292
		boxH = 74
	)

	d.Save()
	d.SetFillColor(core.RGBA(0x0c0e18, 0.94))
	d.BeginPath().RoundRect(boxX, boxY, boxW, boxH, 6).Fill()
	d.SetStrokeColor(core.RGB(0xd4a640)).SetLineWidth(1.5)
	d.BeginPath().RoundRect(boxX, boxY, boxW, boxH, 6).Stroke()

	// Speaker name
	text.DrawPlatform(d, page.Speaker, boxX+10, boxY+15, text.PlatformStyle{
		Font:  core.Font{Size: 9, Weight: core.FontWeightBold},
		Color: core.RGB(0xfada70),
	})

	// Page counter
	counter := fmt.Sprintf("Page %d/%d", g.Dialogue.PageIndex+1, len(g.Dialogue.Pages))
	text.DrawPlatform(d, counter, boxX+10, boxY+15, text.PlatformStyle{
		Font:     core.Font{Size: 8, Weight: core.FontWeightMedium},
		Color:    core.RGB(0x9ca4c0),
		Align:    text.AlignRight,
		MaxWidth: boxW - 20,
	})

	// Typewriter-revealed, word-wrapped dialogue text rendered via text.DrawPlatform.
	visible := g.Dialogue.VisibleText()
	text.DrawPlatform(d, visible, boxX+10, boxY+31, text.PlatformStyle{
		Font:        core.Font{Size: 8.5},
		Color:       core.RGB(0xf0f2fa),
		MaxWidth:    boxW - 20,
		LineHeight:  11.5,
		LineSpacing: 1.5,
	})

	// Footer prompt
	footer := "[E / Next] Continue   [Esc / Close] Close"
	if g.Dialogue.PageIndex+1 == len(g.Dialogue.Pages) {
		footer = "[E / Next] Finish   [Esc / Close] Close"
	}
	text.DrawPlatform(d, footer, boxX+10, boxY+boxH-7, text.PlatformStyle{
		Font:     core.Font{Size: 7},
		Color:    core.RGB(0x8892b4),
		Align:    text.AlignRight,
		MaxWidth: boxW - 20,
	})
	d.Restore()
}

func (g *Game) drawVictoryBanner(d *core.Drawing) {
	d.Save()
	d.SetFillColor(core.RGBA(0x0c1c1b, 0.92))
	d.BeginPath().RoundRect(42, 46, 236, 52, 6).Fill()
	d.SetStrokeColor(core.RGB(0x74f4e0)).SetLineWidth(1.5)
	d.BeginPath().RoundRect(42, 46, 236, 52, 6).Stroke()

	text.DrawPlatform(d, "CRYPTQUEST COMPLETE", 42, 66, text.PlatformStyle{
		Font:     core.Font{Size: 12, Weight: core.FontWeightBold},
		Color:    core.RGB(0x74f4e0),
		Align:    text.AlignCenter,
		MaxWidth: 236,
	})
	text.DrawPlatform(d, "The Sun Relic is yours. Press Reset to play again.", 50, 82, text.PlatformStyle{
		Font:     core.Font{Size: 8},
		Color:    core.RGB(0xe6f8f5),
		Align:    text.AlignCenter,
		MaxWidth: 220,
	})
	d.Restore()
}

func distPoints(a, b core.Point) float32 {
	return float32(math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y)))
}

func lerp(a, b, t float32) float32 {
	return a + (b-a)*t
}

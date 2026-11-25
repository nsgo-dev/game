package playtest

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/random"
)

// ProfileTopDownExplorer is the built-in top-down exploration bot profile for RPG and adventure templates (rpg2d).
const ProfileTopDownExplorer = "topdown-explorer"

const (
	maxExplorerVisionRadius = 1024
	maxExplorerGridCells    = 1_000_000
)

func init() {
	RegisterBot(ProfileTopDownExplorer, NewTopDownExplorer)
}

// GridCell is a tile coordinate (column, row) in a top-down tile map.
type GridCell struct {
	X, Y int
}

// ExplorerNPC describes one NPC on a top-down map that the explorer can talk to once.
type ExplorerNPC struct {
	// ID uniquely identifies the NPC across the world.
	ID string
	// Cell is the tile where the NPC stands.
	Cell GridCell
	// Talked reports whether the player has already spoken to this NPC.
	Talked bool
}

// ExplorerChest describes one chest or collectible interaction on a top-down map.
type ExplorerChest struct {
	// ID uniquely identifies the chest or item across the world.
	ID string
	// Cell is the tile where the chest sits.
	Cell GridCell
	// Opened reports whether the chest has already been opened or picked up.
	Opened bool
	// Solid reports whether the chest blocks movement and must be opened by facing it and pressing confirm.
	// When false, the explorer walks directly onto Cell (and presses confirm if needed).
	Solid bool
}

// ExplorerObservation is the per-step world view a top-down simulation supplies to the "topdown-explorer" bot.
type ExplorerObservation struct {
	// MapID identifies the current map so seen tiles are tracked per map.
	MapID string
	// Width and Height are the current map's dimensions in tiles.
	Width, Height int
	// Walkable reports whether tile (x, y) can be walked onto. When nil, every in-bounds tile is treated as walkable.
	Walkable func(x, y int) bool
	// Player is the player's current tile coordinate.
	Player GridCell
	// Facing is the player's current facing direction ("up", "down", "left", "right", or "").
	Facing string
	// Moving reports whether the player is currently mid-step between tiles.
	Moving bool
	// InDialogue reports whether a dialogue or prompt box is open and expects confirm to advance.
	InDialogue bool
	// InBattle reports whether a turn-based battle is active and expects confirm to advance.
	InBattle bool
	// VisionRadius is the radius in tiles around Player marked as seen on each step (defaults to bot visionRadius when <= 0).
	VisionRadius int
	// NPCs lists NPCs on the current map.
	NPCs []ExplorerNPC
	// Chests lists chests or pickups on the current map.
	Chests []ExplorerChest
	// Exits lists walkable exit tiles leading to other maps, used once all reachable targets on the current map have been visited.
	Exits []GridCell
}

type explorerDir struct {
	name   string
	dx, dy int
	axisX  float64
	axisY  float64
}

var explorerDirs = []explorerDir{
	{name: "up", dx: 0, dy: -1, axisX: 0, axisY: -1},
	{name: "right", dx: 1, dy: 0, axisX: 1, axisY: 0},
	{name: "down", dx: 0, dy: 1, axisX: 0, axisY: 1},
	{name: "left", dx: -1, dy: 0, axisX: -1, axisY: 0},
}

// TopDownExplorer is the built-in "topdown-explorer" bot: it walks to the nearest unseen tile, talks to every NPC once, opens every chest once, and stops when the run finishes or reaches runs.maxDuration (configured at 8x speed).
type TopDownExplorer struct {
	visionRadius int
	talkToNPCs   bool
	openChests   bool

	upAction      string
	downAction    string
	leftAction    string
	rightAction   string
	confirmAction string
	moveXAxis     string
	moveYAxis     string

	// Per-run deterministic exploration memory.
	seenByMap      map[string]map[GridCell]bool
	talkedNPCs     map[string]bool
	openedChests   map[string]bool
	interactTries  map[string]int
	exitUseCount   map[string]map[GridCell]int
	tileTime       map[string]map[GridCell]time.Duration
	confirmToggle  bool
	lastDirName    string
	lastMapID      string
	lastPlayerCell GridCell
	hasLastCell    bool
	stuckSteps     int
}

// NewTopDownExplorer constructs a TopDownExplorer from a playtest BotConfig.
//
// Supported optional Bot.Params:
//   - "visionRadius" (number, default 3): tile radius marked seen around the player.
//   - "talkToNPCs" (bool, default true): whether to seek and talk to each NPC once.
//   - "openChests" (bool, default true): whether to seek and open each chest once.
//   - "upAction" (string, default "up")
//   - "downAction" (string, default "down")
//   - "leftAction" (string, default "left")
//   - "rightAction" (string, default "right")
//   - "confirmAction" (string, default "confirm")
//   - "moveXAxis" (string, default "moveX")
//   - "moveYAxis" (string, default "moveY")
func NewTopDownExplorer(cfg BotConfig) (Bot, error) {
	b := &TopDownExplorer{
		visionRadius:  3,
		talkToNPCs:    true,
		openChests:    true,
		upAction:      "up",
		downAction:    "down",
		leftAction:    "left",
		rightAction:   "right",
		confirmAction: "confirm",
		moveXAxis:     "moveX",
		moveYAxis:     "moveY",
		seenByMap:     make(map[string]map[GridCell]bool),
		talkedNPCs:    make(map[string]bool),
		openedChests:  make(map[string]bool),
		interactTries: make(map[string]int),
		exitUseCount:  make(map[string]map[GridCell]int),
		tileTime:      make(map[string]map[GridCell]time.Duration),
	}
	if cfg.Params != nil {
		if v, ok := cfg.Params["visionRadius"]; ok {
			n, ok := asFloat64(v)
			if !ok || n < 1 || n > maxExplorerVisionRadius {
				return nil, fmt.Errorf("playtest: topdown-explorer param visionRadius must be in [1, %d]", maxExplorerVisionRadius)
			}
			b.visionRadius = int(math.Round(n))
		}
		if v, ok := cfg.Params["talkToNPCs"]; ok {
			flag, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("playtest: topdown-explorer param talkToNPCs must be a boolean")
			}
			b.talkToNPCs = flag
		}
		if v, ok := cfg.Params["openChests"]; ok {
			flag, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("playtest: topdown-explorer param openChests must be a boolean")
			}
			b.openChests = flag
		}
		strParams := []struct {
			key string
			dst *string
		}{
			{"upAction", &b.upAction},
			{"downAction", &b.downAction},
			{"leftAction", &b.leftAction},
			{"rightAction", &b.rightAction},
			{"confirmAction", &b.confirmAction},
			{"moveXAxis", &b.moveXAxis},
			{"moveYAxis", &b.moveYAxis},
		}
		for _, sp := range strParams {
			if v, ok := cfg.Params[sp.key]; ok {
				s, ok := v.(string)
				if !ok || s == "" {
					return nil, fmt.Errorf("playtest: topdown-explorer param %s must be a non-empty string", sp.key)
				}
				*sp.dst = s
			}
		}
	}
	return b, nil
}

// Channels declares the default action and axis names that topdown-explorer drives.
func (b *TopDownExplorer) Channels() (actions, axes []string) {
	return []string{b.upAction, b.downAction, b.leftAction, b.rightAction, b.confirmAction}, []string{b.moveXAxis, b.moveYAxis}
}

// SeenCount returns how many unique walkable tiles the explorer has seen on mapID.
func (b *TopDownExplorer) SeenCount(mapID string) int {
	return len(b.seenByMap[mapID])
}

// TileDuration returns the total simulated time spent on cell in mapID.
func (b *TopDownExplorer) TileDuration(mapID string, cell GridCell) time.Duration {
	if m := b.tileTime[mapID]; m != nil {
		return m[cell]
	}
	return 0
}

// Decide picks the next directional movement or confirm interaction for one top-down exploration step.
func (b *TopDownExplorer) Decide(_ int, dt time.Duration, rawObs any, rng *random.Stream) InputState {
	var obs ExplorerObservation
	switch v := rawObs.(type) {
	case ExplorerObservation:
		obs = v
	case *ExplorerObservation:
		if v != nil {
			obs = *v
		}
	default:
		return InputState{}
	}

	mapID := obs.MapID
	if mapID == "" {
		mapID = "default"
	}
	if b.tileTime[mapID] == nil {
		b.tileTime[mapID] = make(map[GridCell]time.Duration)
	}
	b.tileTime[mapID][obs.Player] += dt

	// Advance dialogue or turn-based battles by pulsing confirm so JustPressed fires reliably.
	if obs.InDialogue || obs.InBattle {
		b.confirmToggle = !b.confirmToggle
		if b.confirmToggle {
			return InputState{Actions: map[string]bool{b.confirmAction: true}}
		}
		return InputState{}
	}

	// Sync external NPC / chest state into memory.
	for _, npc := range obs.NPCs {
		if npc.Talked && npc.ID != "" {
			b.talkedNPCs[npc.ID] = true
		}
	}
	for _, ch := range obs.Chests {
		if ch.Opened && ch.ID != "" {
			b.openedChests[ch.ID] = true
		}
	}

	// Mark tiles within vision radius as seen.
	b.markSeen(mapID, obs)

	// Track whether we changed cell or map so we detect blocked paths cleanly.
	if !b.hasLastCell || b.lastMapID != mapID || b.lastPlayerCell != obs.Player {
		b.lastMapID = mapID
		b.lastPlayerCell = obs.Player
		b.hasLastCell = true
		b.stuckSteps = 0
	} else if !obs.Moving {
		b.stuckSteps++
	}

	// While mid-step between tiles, hold the previous direction so tile transitions stay smooth.
	if obs.Moving && b.lastDirName != "" {
		return b.dirInput(b.lastDirName)
	}

	// 1. If standing on an unopened non-solid chest, open it now.
	if b.openChests {
		for _, ch := range obs.Chests {
			if ch.Opened || b.openedChests[ch.ID] || ch.Solid {
				continue
			}
			if ch.Cell == obs.Player {
				b.markInteracted("chest:"+ch.ID, ch.ID, false)
				return b.pulseConfirm()
			}
		}
	}

	// 2. If adjacent to an untalked NPC or unopened solid chest, turn toward it and confirm once.
	if b.talkToNPCs {
		for _, npc := range obs.NPCs {
			if npc.Talked || b.talkedNPCs[npc.ID] {
				continue
			}
			if dirName, ok := adjacentDir(obs.Player, npc.Cell); ok {
				if obs.Facing != "" && obs.Facing != dirName {
					b.lastDirName = dirName
					return b.dirInput(dirName)
				}
				b.markInteracted("npc:"+npc.ID, npc.ID, true)
				return b.pulseConfirm()
			}
		}
	}
	if b.openChests {
		for _, ch := range obs.Chests {
			if ch.Opened || b.openedChests[ch.ID] || !ch.Solid {
				continue
			}
			if dirName, ok := adjacentDir(obs.Player, ch.Cell); ok {
				if obs.Facing != "" && obs.Facing != dirName {
					b.lastDirName = dirName
					return b.dirInput(dirName)
				}
				b.markInteracted("chest:"+ch.ID, ch.ID, false)
				return b.pulseConfirm()
			}
		}
	}

	// 3. Plan shortest path on the walkable grid via BFS.
	stepDir := b.chooseNextDir(mapID, obs, rng)
	b.lastDirName = stepDir
	if stepDir == "" {
		return InputState{}
	}
	return b.dirInput(stepDir)
}

func (b *TopDownExplorer) markInteracted(key, id string, isNPC bool) {
	b.interactTries[key]++
	// Mark interacted after two confirm attempts so a simulation that does not flip Talked/Opened immediately still gets confirmed and never deadlocks.
	if b.interactTries[key] >= 2 {
		if isNPC {
			b.talkedNPCs[id] = true
		} else {
			b.openedChests[id] = true
		}
	}
}

func (b *TopDownExplorer) pulseConfirm() InputState {
	b.confirmToggle = !b.confirmToggle
	b.lastDirName = ""
	if b.confirmToggle {
		return InputState{Actions: map[string]bool{b.confirmAction: true}}
	}
	return InputState{}
}

func (b *TopDownExplorer) markSeen(mapID string, obs ExplorerObservation) {
	if obs.Width <= 0 || obs.Height <= 0 {
		return
	}
	seen := b.seenByMap[mapID]
	if seen == nil {
		seen = make(map[GridCell]bool)
		b.seenByMap[mapID] = seen
	}
	r := obs.VisionRadius
	if r <= 0 {
		r = b.visionRadius
	}
	if r > maxExplorerVisionRadius {
		r = maxExplorerVisionRadius
	}
	minX := max(0, obs.Player.X-r)
	maxX := min(obs.Width-1, obs.Player.X+r)
	minY := max(0, obs.Player.Y-r)
	maxY := min(obs.Height-1, obs.Player.Y+r)
	r2 := r * r
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := x - obs.Player.X
			dy := y - obs.Player.Y
			if dx*dx+dy*dy > r2 {
				continue
			}
			if obs.Walkable == nil || obs.Walkable(x, y) {
				seen[GridCell{X: x, Y: y}] = true
			}
		}
	}
}

func (b *TopDownExplorer) chooseNextDir(mapID string, obs ExplorerObservation, rng *random.Stream) string {
	if obs.Width <= 0 || obs.Height <= 0 || obs.Width > maxExplorerGridCells/obs.Height {
		return ""
	}
	blocked := make(map[GridCell]bool, len(obs.NPCs)+len(obs.Chests))
	npcTargets := make(map[GridCell]bool)
	chestTargets := make(map[GridCell]bool)

	for _, npc := range obs.NPCs {
		blocked[npc.Cell] = true
		if b.talkToNPCs && !npc.Talked && !b.talkedNPCs[npc.ID] {
			for _, d := range explorerDirs {
				adj := GridCell{X: npc.Cell.X + d.dx, Y: npc.Cell.Y + d.dy}
				if inGrid(adj, obs.Width, obs.Height) && (obs.Walkable == nil || obs.Walkable(adj.X, adj.Y)) {
					npcTargets[adj] = true
				}
			}
		}
	}
	for _, ch := range obs.Chests {
		if ch.Solid {
			blocked[ch.Cell] = true
		}
		if b.openChests && !ch.Opened && !b.openedChests[ch.ID] {
			if ch.Solid {
				for _, d := range explorerDirs {
					adj := GridCell{X: ch.Cell.X + d.dx, Y: ch.Cell.Y + d.dy}
					if inGrid(adj, obs.Width, obs.Height) && (obs.Walkable == nil || obs.Walkable(adj.X, adj.Y)) {
						chestTargets[adj] = true
					}
				}
			} else {
				chestTargets[ch.Cell] = true
			}
		}
	}

	seen := b.seenByMap[mapID]

	// Seeded neighbor iteration order lets different seeds explore symmetric branches differently while remaining strictly deterministic for each seed.
	dirOrder := [4]int{0, 1, 2, 3}
	if rng != nil {
		shift := rng.Intn(4)
		for i := range 4 {
			dirOrder[i] = (i + shift) % 4
		}
	}

	type node struct {
		cell     GridCell
		firstDir string
		dist     int
	}
	totalCells := obs.Width * obs.Height
	visited := make([]bool, totalCells)
	idxOf := func(c GridCell) int { return c.Y*obs.Width + c.X }

	if !inGrid(obs.Player, obs.Width, obs.Height) {
		return ""
	}
	visited[idxOf(obs.Player)] = true
	queue := make([]node, 0, min(totalCells, 256))
	queue = append(queue, node{cell: obs.Player, dist: 0})

	var (
		bestInteractDir  string
		bestInteractDist = math.MaxInt
		bestUnseenDir    string
		bestUnseenDist   = math.MaxInt
	)

	exitSet := make(map[GridCell]bool, len(obs.Exits))
	for _, ex := range obs.Exits {
		exitSet[ex] = true
	}
	var (
		bestExitDir   string
		bestExitScore = math.MaxInt
		bestExitCell  GridCell
	)

	for head := 0; head < len(queue); head++ {
		cur := queue[head]
		if cur.dist > 0 {
			if (npcTargets[cur.cell] || chestTargets[cur.cell]) && cur.dist < bestInteractDist {
				bestInteractDist = cur.dist
				bestInteractDir = cur.firstDir
			}
			if !seen[cur.cell] && cur.dist < bestUnseenDist {
				bestUnseenDist = cur.dist
				bestUnseenDir = cur.firstDir
			}
			if exitSet[cur.cell] {
				uses := 0
				if m := b.exitUseCount[mapID]; m != nil {
					uses = m[cur.cell]
				}
				score := uses*1000 + cur.dist
				if score < bestExitScore {
					bestExitScore = score
					bestExitDir = cur.firstDir
					bestExitCell = cur.cell
				}
			}
		}
		// Stop expanding once we have both an interaction target and an unseen tile closer than current distance.
		if bestInteractDir != "" && bestUnseenDir != "" && cur.dist > bestInteractDist && cur.dist > bestUnseenDist {
			break
		}
		for _, di := range dirOrder {
			d := explorerDirs[di]
			next := GridCell{X: cur.cell.X + d.dx, Y: cur.cell.Y + d.dy}
			if !inGrid(next, obs.Width, obs.Height) {
				continue
			}
			ni := idxOf(next)
			if visited[ni] || blocked[next] {
				continue
			}
			if obs.Walkable != nil && !obs.Walkable(next.X, next.Y) {
				continue
			}
			visited[ni] = true
			fd := cur.firstDir
			if fd == "" {
				fd = d.name
			}
			queue = append(queue, node{cell: next, firstDir: fd, dist: cur.dist + 1})
		}
	}

	// Prefer nearby untalked NPCs and unopened chests, otherwise walk to the nearest unseen tile.
	switch {
	case bestInteractDir != "" && (bestUnseenDir == "" || bestInteractDist <= bestUnseenDist+4):
		return bestInteractDir
	case bestUnseenDir != "":
		return bestUnseenDir
	case bestInteractDir != "":
		return bestInteractDir
	case bestExitDir != "":
		if bestExitScore < math.MaxInt {
			if b.exitUseCount[mapID] == nil {
				b.exitUseCount[mapID] = make(map[GridCell]int)
			}
			if manhattan(obs.Player, bestExitCell) <= 1 {
				b.exitUseCount[mapID][bestExitCell]++
			}
		}
		return bestExitDir
	default:
		return ""
	}
}

func (b *TopDownExplorer) dirInput(name string) InputState {
	for _, d := range explorerDirs {
		if d.name != name {
			continue
		}
		var actionName string
		switch name {
		case "up":
			actionName = b.upAction
		case "down":
			actionName = b.downAction
		case "left":
			actionName = b.leftAction
		case "right":
			actionName = b.rightAction
		}
		actions := map[string]bool{actionName: true}
		axes := map[string]float64{}
		if d.axisX != 0 {
			axes[b.moveXAxis] = d.axisX
		}
		if d.axisY != 0 {
			axes[b.moveYAxis] = d.axisY
		}
		return InputState{Actions: actions, Axes: axes}
	}
	return InputState{}
}

func adjacentDir(from, to GridCell) (string, bool) {
	dx, dy := to.X-from.X, to.Y-from.Y
	for _, d := range explorerDirs {
		if d.dx == dx && d.dy == dy {
			return d.name, true
		}
	}
	return "", false
}

func inGrid(c GridCell, w, h int) bool {
	return c.X >= 0 && c.Y >= 0 && c.X < w && c.Y < h
}

func manhattan(a, b GridCell) int {
	dx := a.X - b.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - b.Y
	if dy < 0 {
		dy = -dy
	}
	return dx + dy
}

package main

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/pathfind/navbake"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	TileSize = 16
	MapCols  = 24
	MapRows  = 16
)

// Global tile IDs matching the tileset frame order in buildWorldMap.
const (
	GIDFloor uint32 = iota + 1
	GIDMoss
	GIDCarpet
	GIDWall
	GIDPillar
	GIDDoorLocked
	GIDDoorOpen
	GIDExit
	GIDExitAlt
)

// TotalRooms is the number of named rooms in the crypt.
const TotalRooms = 7

// EnemySpawn describes an enemy's starting identity and patrol loop.
type EnemySpawn struct {
	ID        string
	Name      string
	Waypoints []pathfind.Cell
}

// World holds the tilemap, collision grid, navigation grid, spritesheet atlas,
// and key dungeon landmarks.
type World struct {
	Atlas       *sprite.Atlas
	Map         *tilemap.Tilemap
	Physics     *physics.Grid
	Nav         *pathfind.BoolGrid
	PlayerSpawn core.Point
	ElderCell   pathfind.Cell
	ElderPos    core.Point
	ChestCell   pathfind.Cell
	ChestPos    core.Point
	DoorCell    pathfind.Cell
	ExitCell    pathfind.Cell
	ExitPos     core.Point
	EnemySpawns []EnemySpawn

	// NavMesh is the crypt baked for a skeleton's radius from the tilemap's solid cells, shown by the debug overlay; it is rebaked when the sanctum door opens or closes.
	NavMesh *navbake.Mesh
}

// cryptLayout defines the 24x16 dungeon:
//
//	# = stone wall (solid)
//	. = slate floor
//	, = mossy stone floor
//	: = sanctum carpet
//	P = stone pillar (solid)
//	D = locked sanctum door (solid until unlocked)
//	X = sanctum exit portal
//	E = Elder Aldric NPC
//	C = Sun Relic chest
var cryptLayout = []string{
	"########################",
	"#......#,,,,,,#::::::::#",
	"#..E...#,,P,,,#:::C::::#",
	"#......#,,,,,,#::::::::#",
	"#......,,,P,,,#::::::::#",
	"#......#,,,,,,####..####",
	"###..###,,,,,,#........#",
	"#......#,,P,,,#..P..P..#",
	"#..PP..#,,,,,,....P....#",
	"#......#,,,,,,#..P..P..#",
	"#......###..###........#",
	"#......#......####D#####",
	"#..P...#..PP..#::::::::#",
	"#.............#:::X::::#",
	"#......#......#::::::::#",
	"########################",
}

// NewWorld constructs the procedural pixel-art atlas, dungeon tilemap,
// physics collision grid, and A* navigation grid.
func NewWorld() *World {
	atlas := buildAtlas()
	ts := tilemap.NewAtlasTileset(
		1,
		atlas,
		"floor",
		"moss",
		"carpet",
		"wall",
		"pillar",
		"door_locked",
		"door_open",
		"exit_0",
		"exit_1",
	)
	ts.Animations = map[uint32]*tilemap.TileAnimation{
		// Local ID 7 ("exit_0") pulses gently with local ID 8 ("exit_1").
		7: {
			Frames: []tilemap.TileAnimFrame{
				{TileID: 7, Duration: 380 * time.Millisecond},
				{TileID: 8, Duration: 380 * time.Millisecond},
			},
		},
	}

	tm := tilemap.New(MapCols, MapRows, TileSize, TileSize, ts)
	tm.SetFlags(GIDWall, tilemap.TileSolid)
	tm.SetFlags(GIDPillar, tilemap.TileSolid)
	tm.SetFlags(GIDDoorLocked, tilemap.TileSolid)

	layer := tilemap.NewTileLayer("crypt", MapCols, MapRows, nil)
	phys := physics.NewGrid(MapCols, MapRows, TileSize, TileSize)
	nav := pathfind.NewBoolGrid(MapCols, MapRows)

	w := &World{
		Atlas:       atlas,
		Map:         tm,
		Physics:     phys,
		Nav:         nav,
		PlayerSpawn: cellCenter(pathfind.Cell{Col: 2, Row: 4}),
		EnemySpawns: []EnemySpawn{
			{
				ID:   "bone-warden",
				Name: "Bone Warden",
				Waypoints: []pathfind.Cell{
					{Col: 9, Row: 2},
					{Col: 12, Row: 2},
					{Col: 12, Row: 8},
					{Col: 9, Row: 8},
				},
			},
			{
				ID:   "crypt-stalker",
				Name: "Crypt Stalker",
				Waypoints: []pathfind.Cell{
					{Col: 16, Row: 6},
					{Col: 21, Row: 6},
					{Col: 21, Row: 10},
					{Col: 16, Row: 10},
				},
			},
			{
				ID:   "vault-guard",
				Name: "Vault Guard",
				Waypoints: []pathfind.Cell{
					{Col: 9, Row: 11},
					{Col: 12, Row: 11},
					{Col: 12, Row: 14},
					{Col: 9, Row: 14},
				},
			},
		},
	}

	for row := 0; row < MapRows; row++ {
		line := cryptLayout[row]
		for col := 0; col < MapCols; col++ {
			ch := line[col]
			var gid uint32
			solid := false
			switch ch {
			case '#':
				gid = GIDWall
				solid = true
			case 'P':
				gid = GIDPillar
				solid = true
			case ',':
				gid = GIDMoss
			case ':':
				gid = GIDCarpet
			case 'D':
				gid = GIDDoorLocked
				solid = true
				w.DoorCell = pathfind.Cell{Col: col, Row: row}
			case 'X':
				gid = GIDExit
				w.ExitCell = pathfind.Cell{Col: col, Row: row}
				w.ExitPos = cellCenter(w.ExitCell)
			case 'E':
				gid = GIDFloor
				w.ElderCell = pathfind.Cell{Col: col, Row: row}
				w.ElderPos = cellCenter(w.ElderCell)
			case 'C':
				gid = GIDCarpet
				w.ChestCell = pathfind.Cell{Col: col, Row: row}
				w.ChestPos = cellCenter(w.ChestCell)
			default:
				gid = GIDFloor
			}
			layer.Data[row*MapCols+col] = gid
			if solid {
				phys.Set(col, row, physics.TileSolid)
				nav.SetWalkable(col, row, false)
			}
		}
	}
	tm.Layers = []tilemap.TileLayer{layer}
	w.bakeNavMesh()
	return w
}

// navAgent is the agent the crypt's navigation mesh is baked for: a skeleton, whose hitbox is enemyHitboxSize wide.
var navAgent = navbake.Agent{Name: "skeleton", Radius: enemyHitboxSize / 2}

// bakeNavMesh bakes NavMesh from the tilemap's current solid cells. The crypt's layout and its door states are fixed in code, so a failed bake is a programming error and panics.
func (w *World) bakeNavMesh() {
	src, err := navbake.FromTilemap(w.Map, tilemap.TileSolid)
	if err != nil {
		panic(fmt.Sprintf("cryptquest: navmesh source: %v", err))
	}
	mesh, err := navbake.Bake(src, navAgent, navbake.Options{})
	if err != nil {
		panic(fmt.Sprintf("cryptquest: bake navmesh: %v", err))
	}
	w.NavMesh = mesh
}

// UnlockDoor opens the sealed sanctum gate in tilemap, physics, and A* navgrid.
func (w *World) UnlockDoor() {
	c := w.DoorCell
	w.Map.SetTile(0, c.Col, c.Row, GIDDoorOpen)
	w.Physics.Set(c.Col, c.Row, 0)
	w.Nav.SetWalkable(c.Col, c.Row, true)
	w.bakeNavMesh()
}

// LockDoor seals the sanctum gate again (used on reset or loading a pre-relic save).
func (w *World) LockDoor() {
	c := w.DoorCell
	w.Map.SetTile(0, c.Col, c.Row, GIDDoorLocked)
	w.Physics.Set(c.Col, c.Row, physics.TileSolid)
	w.Nav.SetWalkable(c.Col, c.Row, false)
	w.bakeNavMesh()
}

// CellAt converts a world coordinate to a clamped grid cell.
func (w *World) CellAt(x, y float32) pathfind.Cell {
	col := int(x / TileSize)
	row := int(y / TileSize)
	if col < 0 {
		col = 0
	} else if col >= MapCols {
		col = MapCols - 1
	}
	if row < 0 {
		row = 0
	} else if row >= MapRows {
		row = MapRows - 1
	}
	return pathfind.Cell{Col: col, Row: row}
}

// CellCenter returns the center point of cell c in world points.
func (w *World) CellCenter(c pathfind.Cell) core.Point {
	return cellCenter(c)
}

func cellCenter(c pathfind.Cell) core.Point {
	return core.Point{
		X: core.Points(float32(c.Col)*TileSize + TileSize*0.5),
		Y: core.Points(float32(c.Row)*TileSize + TileSize*0.5),
	}
}

// HasLineOfSight reports whether no solid tile blocks the straight segment from -> to.
func (w *World) HasLineOfSight(from, to core.Point) bool {
	_, hit := w.Map.Raycast(from, to, tilemap.TileSolid)
	return !hit
}

// RoomNameAt returns the display name of the crypt room containing (col, row).
func RoomNameAt(col, row int) string {
	switch {
	case col <= 6 && row <= 5:
		return "Entry Hall"
	case col <= 6 && row >= 6:
		return "Sunken Crypt"
	case col >= 7 && col <= 13 && row <= 10:
		return "Bone Corridor"
	case col >= 7 && col <= 13 && row >= 11:
		return "Lower Vault"
	case col >= 14 && row <= 5:
		return "Relic Chamber"
	case col >= 14 && row >= 6 && row <= 10:
		return "Catacomb Hall"
	default:
		return "Exit Sanctum"
	}
}

// buildAtlas procedurally renders all 16x16 dungeon tiles, characters, and
// icons into an in-memory *image.RGBA spritesheet with zero external files.
func buildAtlas() *sprite.Atlas {
	const (
		cols = 8
		rows = 3
	)
	img := image.NewRGBA(image.Rect(0, 0, cols*TileSize, rows*TileSize))

	palette := map[byte]color.RGBA{
		'.': {0, 0, 0, 0},         // transparent
		'k': {16, 17, 26, 255},    // deep outline
		's': {46, 51, 69, 255},    // stone base
		'S': {67, 74, 98, 255},    // stone highlight
		'd': {31, 34, 48, 255},    // stone shadow / mortar
		'w': {84, 92, 120, 255},   // wall light brick
		'W': {112, 122, 152, 255}, // wall bevel top
		'm': {58, 96, 72, 255},    // moss dark
		'M': {86, 140, 96, 255},   // moss bright
		'r': {112, 34, 48, 255},   // carpet crimson
		'R': {148, 46, 62, 255},   // carpet highlight
		'g': {212, 166, 64, 255},  // gold trim / lock
		'G': {250, 218, 112, 255}, // bright gold / sun relic
		'b': {52, 128, 214, 255},  // hero cloak azure
		'B': {96, 176, 246, 255},  // hero cloak highlight
		'f': {236, 198, 162, 255}, // skin tone
		'p': {114, 72, 164, 255},  // elder robe violet
		'P': {154, 108, 212, 255}, // elder robe highlight
		'h': {232, 236, 244, 255}, // white beard / bone
		'H': {188, 196, 212, 255}, // bone shadow
		'e': {234, 62, 68, 255},   // glowing red eyes / alert
		'E': {255, 134, 118, 255}, // bright red highlight
		't': {36, 176, 164, 255},  // teal portal
		'T': {116, 244, 224, 255}, // bright cyan portal glow
		'o': {126, 78, 44, 255},   // chest wood / staff
		'O': {168, 112, 64, 255},  // light wood
		'c': {160, 212, 255, 255}, // blade / crystal cyan
	}

	type spriteDef struct {
		name string
		col  int
		row  int
		art  [16]string
	}

	defs := []spriteDef{
		// Row 0: Tiles
		{
			name: "floor", col: 0, row: 0,
			art: [16]string{
				"dddddddddddddddd",
				"dssssssddssssssd",
				"dsSssssddssssssd",
				"dssssssddssssssd",
				"dssssssddssssssd",
				"dssssssddssSsssd",
				"dssssssddssssssd",
				"dddddddddddddddd",
				"dddddddddddddddd",
				"dsssddssssssddsd",
				"dsssddssssssddsd",
				"dsssddsSssssddsd",
				"dsssddssssssddsd",
				"dsssddssssssddsd",
				"dsssddssssssddsd",
				"dddddddddddddddd",
			},
		},
		{
			name: "moss", col: 1, row: 0,
			art: [16]string{
				"dddddddddddddddd",
				"dmmssssddssssssd",
				"dmMssssddssmmssd",
				"dssssssddssMmssd",
				"dssssssddssssssd",
				"dssssssddssssssd",
				"dsssssmddssssssd",
				"dddddddddddddddd",
				"dddddddddddddddd",
				"dsssddssssssddsd",
				"dsmsddssssssddsd",
				"dsMsddsssmmsddsd",
				"dsssddsssmMsddsd",
				"dsssddssssssddsd",
				"dsssddssssmmddsd",
				"dddddddddddddddd",
			},
		},
		{
			name: "carpet", col: 2, row: 0,
			art: [16]string{
				"rrrrrrrrrrrrrrrr",
				"rggggggggggggggr",
				"rgRRRRRRRRRRRRgr",
				"rgRrrrrrrrrrrRgr",
				"rgRrRRrrrrRRrRgr",
				"rgRrrRrrrrRrrRgr",
				"rgRrrrrrrrrrrRgr",
				"rgRrrrrggrrrrRgr",
				"rgRrrrrggrrrrRgr",
				"rgRrrrrrrrrrrRgr",
				"rgRrrRrrrrRrrRgr",
				"rgRrRRrrrrRRrRgr",
				"rgRrrrrrrrrrrRgr",
				"rgRRRRRRRRRRRRgr",
				"rggggggggggggggr",
				"rrrrrrrrrrrrrrrr",
			},
		},
		{
			name: "wall", col: 3, row: 0,
			art: [16]string{
				"kkkkkkkkkkkkkkkk",
				"kWWWWWWWWWWWWWWk",
				"kWwwwwwwkwwwwwWk",
				"kWwwwwwwkwwwwwWk",
				"kWwwwwwwkwwwwwWk",
				"kWssssssksssssWk",
				"kkkkkkkkkkkkkkkk",
				"kWwwwkwwwwwwwkWk",
				"kWwwwkwwwwwwwkWk",
				"kWwwwkwwwwwwwkWk",
				"kWssskssssssskWk",
				"kkkkkkkkkkkkkkkk",
				"kSssssssksssssSk",
				"kddddddddddddddk",
				"kddddddddddddddk",
				"kkkkkkkkkkkkkkkk",
			},
		},
		{
			name: "pillar", col: 4, row: 0,
			art: [16]string{
				"dddddddddddddddd",
				"ddkkkkkkkkkkkkdd",
				"ddkWWWWWWWWWWkdd",
				"ddkWwwwwwwwwWkdd",
				"ddkWssssssssWkdd",
				"dddkkkkkkkkkkddd",
				"dddkWwwsswwWkddd",
				"dddkWwwsswwWkddd",
				"dddkWwwsswwWkddd",
				"dddkWwwsswwWkddd",
				"dddkWwwsswwWkddd",
				"dddkSssssssSkddd",
				"ddkkkkkkkkkkkkdd",
				"ddkddddddddddkdd",
				"ddkkkkkkkkkkkkdd",
				"dddddddddddddddd",
			},
		},
		{
			name: "door_locked", col: 5, row: 0,
			art: [16]string{
				"kkkkkkkkkkkkkkkk",
				"kWWWWWWWWWWWWWWk",
				"kWkkkkkkkkkkkkWk",
				"kWkooooooooooKWk",
				"kWkoOooooooOokWk",
				"kWkkkkkkkkkkkkWk",
				"kWkoooGGGGooOkWk",
				"kWkoooGkkGooOkWk",
				"kWkoooGkkGooOkWk",
				"kWkoooGGGGooOkWk",
				"kWkkkkkkkkkkkkWk",
				"kWkoOooooooOokWk",
				"kWkooooooooooKWk",
				"kWkkkkkkkkkkkkWk",
				"kddddddddddddddk",
				"kkkkkkkkkkkkkkkk",
			},
		},
		{
			name: "door_open", col: 6, row: 0,
			art: [16]string{
				"kkkkkkkkkkkkkkkk",
				"kWWWWWWWWWWWWWWk",
				"kWkksssssssskkWk",
				"kWkksssssssskkWk",
				"kWkkssSssssskkWk",
				"kWkksssssssskkWk",
				"kWkksssssssskkWk",
				"kWkksssssssskkWk",
				"kWkksssssssskkWk",
				"kWkksssssssskkWk",
				"kWkkssssSssskkWk",
				"kWkksssssssskkWk",
				"kWkksssssssskkWk",
				"kWkksssssssskkWk",
				"kdkkddddddddkkdk",
				"kkkkkkkkkkkkkkkk",
			},
		},
		{
			name: "exit_0", col: 7, row: 0,
			art: [16]string{
				"rrrrrkkkkkkrrrrr",
				"rrrkkttttttkkrrr",
				"rrktTTTTTTTTtkrr",
				"rktTttTTTTttTtkr",
				"rktTtkTTTTktTtkr",
				"ktTttTGGGGttTTtk",
				"ktTtTGTTTTGTtTtk",
				"ktTtTGTKkTGTtTtk",
				"ktTtTGTKkTGTtTtk",
				"ktTtTGTTTTGTtTtk",
				"ktTttTGGGGttTTtk",
				"rktTtkTTTTktTtkr",
				"rktTttTTTTttTtkr",
				"rrktTTTTTTTTtkrr",
				"rrrkkttttttkkrrr",
				"rrrrrkkkkkkrrrrr",
			},
		},
		// Row 1: Characters
		{
			name: "hero_0", col: 0, row: 1,
			art: [16]string{
				".....kkkkkk.....",
				"....kBBBBBBk....",
				"...kBBbbbbBBk...",
				"...kBffffffBk...",
				"...kBfkkffkBk...",
				"...kBffffffBk...",
				"....kbbbbbbk....",
				"...kBbbbbbbBk...",
				"..kBbbggggbbBk..",
				"..kBbbbbbbbbBk..",
				"..kfkbBBBBbkfk..",
				"...k.kbbbbk.k...",
				".....kbbkbbk....",
				".....kddkddk....",
				".....kkkkkkk....",
				"................",
			},
		},
		{
			name: "hero_1", col: 1, row: 1,
			art: [16]string{
				"................",
				".....kkkkkk.....",
				"....kBBBBBBk....",
				"...kBBbbbbBBk...",
				"...kBffffffBk...",
				"...kBfkkffkBk...",
				"...kBffffffBk...",
				"....kbbbbbbk....",
				"...kBbbbbbbBk...",
				"..kBbbggggbbBk..",
				"..kBbbbbbbbbBk..",
				"...k.kBBBBk.k...",
				"....kbbkkbbk....",
				"...kddk..kddk...",
				"...kkk....kkk...",
				"................",
			},
		},
		{
			name: "elder_0", col: 2, row: 1,
			art: [16]string{
				".....kkkkkk..kk.",
				"....kPPPPPPkkck.",
				"...kPPppppPPkok.",
				"...kPffffffPkok.",
				"...kPfkkffkPkok.",
				"...kPPhhhhPPkok.",
				"....khhhhhhkkok.",
				"...kPphhhhPPkok.",
				"..kPppgppgppkok.",
				"..kPppppppppkok.",
				"..kPppppppppkok.",
				"...kppppppppkok.",
				"...kppppppppkok.",
				"...kddkkkkddkkk.",
				"...kkk....kkk...",
				"................",
			},
		},
		{
			name: "elder_1", col: 3, row: 1,
			art: [16]string{
				".....kkkkkk.kkk.",
				"....kPPPPPPkGGk.",
				"...kPPppppPPkGk.",
				"...kPffffffPkok.",
				"...kPfkkffkPkok.",
				"...kPPhhhhPPkok.",
				"....khhhhhhkkok.",
				"...kPphhhhPPkok.",
				"..kPppgppgppkok.",
				"..kPppppppppkok.",
				"..kPppppppppkok.",
				"...kppppppppkok.",
				"...kppppppppkok.",
				"...kddkkkkddkkk.",
				"...kkk....kkk...",
				"................",
			},
		},
		{
			name: "skel_0", col: 4, row: 1,
			art: [16]string{
				".....kkkkkk.....",
				"....khhhhhhk....",
				"...khhhhhhhhk...",
				"...khhehhhehk...",
				"...khhhhhhhhk...",
				"....khhkkhhk....",
				"....kHkHkHk.....",
				"...kHHhhhhHHk...",
				"..kH.kHhhHk.Hk..",
				"..k..khhhhk..k..",
				".....kHkkHk.....",
				".....khkkhk.....",
				".....khkkhk.....",
				"....khhkkhhk....",
				"....kkk..kkk....",
				"................",
			},
		},
		{
			name: "skel_1", col: 5, row: 1,
			art: [16]string{
				"................",
				".....kkkkkk.....",
				"....khhhhhhk....",
				"...khhhhhhhhk...",
				"...khhehhhehk...",
				"...khhhhhhhhk...",
				"....khhkkhhk....",
				"...kHHhhhhHHk...",
				"..kH.kHhhHk.Hk..",
				".....khhhhk.....",
				"....kHkkkkHk....",
				"...khk....khk...",
				"...khk....khk...",
				"..khhk....khhk..",
				"..kkk......kkk..",
				"................",
			},
		},
		{
			name: "skel_alert", col: 6, row: 1,
			art: [16]string{
				"....kkkkkkkk....",
				"...kEhhhhhhEk...",
				"..kEhhhhhhhhEk..",
				"..kEhEehheEhEk..",
				"...khhhhhhhhk...",
				"....khhkkhhk.kk.",
				"...kEHHHHHHEkck.",
				"..kE.kHhhHk.kck.",
				"..k..khhhhk.kkk.",
				".....kHkkHk.....",
				"....kHkkkkHk....",
				"...khk....khk...",
				"...khk....khk...",
				"..khhk....khhk..",
				"..kkk......kkk..",
				"................",
			},
		},
		{
			name: "skel_defeated", col: 7, row: 1,
			art: [16]string{
				"................",
				"................",
				"................",
				"................",
				"................",
				"................",
				"................",
				"......kkkk......",
				".....khhhhk.....",
				"...kkhhkhhkkk...",
				"..kHHhhhhhhHHk..",
				".kHhhHHkkHHhhHk.",
				"..kkkkkkkkkkkk..",
				"................",
				"................",
				"................",
			},
		},
		// Row 2: Objects & FX
		{
			name: "chest_closed", col: 0, row: 2,
			art: [16]string{
				"................",
				"................",
				"..kkkkkkkkkkkk..",
				".kggggggggggggk.",
				".kgoOOOOOOOOogk.",
				".kgoooooooooogk.",
				".kggggGGGGggggk.",
				".kgoooGkkGooogk.",
				".kgoooGGGGooogk.",
				".kgoooooooooogk.",
				".kgoOOOOOOOOogk.",
				".kggggggggggggk.",
				"..kkkkkkkkkkkk..",
				"...dddddddddd...",
				"................",
				"................",
			},
		},
		{
			name: "chest_open", col: 1, row: 2,
			art: [16]string{
				"..kkkkkkkkkkkk..",
				".kggggggggggggk.",
				".kgoooooooooogk.",
				".kddddddddddddk.",
				".kddddddddddddk.",
				".kggggggggggggk.",
				".kgoooooooooogk.",
				".kgoooGkkGooogk.",
				".kgoooGGGGooogk.",
				".kgoooooooooogk.",
				".kgoOOOOOOOOogk.",
				".kggggggggggggk.",
				"..kkkkkkkkkkkk..",
				"...dddddddddd...",
				"................",
				"................",
			},
		},
		{
			name: "relic", col: 2, row: 2,
			art: [16]string{
				".......kk.......",
				"......kGGk......",
				".....kGggGk.....",
				"....kGgGGgGk....",
				"...kGgGGGGgGk...",
				"..kGgGGhhGGgGk..",
				"..kGgGGhhGGgGk..",
				"...kGgGGGGgGk...",
				"....kGgGGgGk....",
				".....kGggGk.....",
				"......kggk......",
				".....kggggk.....",
				"....kGGGGGGk....",
				"....kkkkkkkk....",
				"................",
				"................",
			},
		},
		{
			name: "exit_1", col: 3, row: 2,
			art: [16]string{
				"rrrrrkkkkkkrrrrr",
				"rrrkkTTTTTTkkrrr",
				"rrkTttttttttTkrr",
				"rkTttTTTTTTttTkr",
				"rkTtkTTGGTTktTkr",
				"kTttTGGhhGGTttTk",
				"kTtTGThTTThGTtTk",
				"kTtTGhTkkThGTtTk",
				"kTtTGhTkkThGTtTk",
				"kTtTGThTTThGTtTk",
				"kTttTGGhhGGTttTk",
				"rkTtkTTGGTTktTkr",
				"rkTttTTTTTTttTkr",
				"rrkTttttttttTkrr",
				"rrrkkTTTTTTkkrrr",
				"rrrrrkkkkkkrrrrr",
			},
		},
		{
			name: "alert_mark", col: 4, row: 2,
			art: [16]string{
				"......kkkk......",
				".....keeeeK.....",
				".....keEEek.....",
				".....keEEek.....",
				".....keEEek.....",
				".....keeeeK.....",
				"......kekk......",
				"......kkkk......",
				"................",
				"......kkkk......",
				".....keEEek.....",
				".....keeeeK.....",
				"......kkkk......",
				"................",
				"................",
				"................",
			},
		},
		{
			name: "talk_mark", col: 5, row: 2,
			art: [16]string{
				"....kkkkkkkk....",
				"...kGGGGGGGGk...",
				"..kGggggggggGk..",
				"..kGgGGggGGgGk..",
				"..kGggggggggGk..",
				"..kGgGGGGGGgGk..",
				"..kGggggggggGk..",
				"...kGGGGGGGGk...",
				"....kkkGGkkk....",
				"......kGk.......",
				".......k........",
				"................",
				"................",
				"................",
				"................",
				"................",
			},
		},
		{
			name: "slash", col: 6, row: 2,
			art: [16]string{
				"...........kkk..",
				".........kkcck..",
				".......kkcGGck..",
				"......kcGGcck...",
				".....kcGcckk....",
				"....kcGckk......",
				"...kcGck........",
				"..kcGck.........",
				"..kcck..........",
				".kcck...........",
				".kck............",
				".kk.............",
				"................",
				"................",
				"................",
				"................",
			},
		},
		{
			name: "sparkle", col: 7, row: 2,
			art: [16]string{
				"................",
				".......GG.......",
				".......GG.......",
				"......GhhG......",
				".....GhhhhG.....",
				"..GGGhhhhhhGGG..",
				".GhhhhhhhhhhhhG.",
				"..GGGhhhhhhGGG..",
				".....GhhhhG.....",
				"......GhhG......",
				".......GG.......",
				".......GG.......",
				"................",
				"................",
				"................",
				"................",
			},
		},
	}

	frames := make(map[string]sprite.Frame, len(defs))
	for _, def := range defs {
		ox := def.col * TileSize
		oy := def.row * TileSize
		for y := 0; y < TileSize; y++ {
			rowStr := def.art[y]
			for x := 0; x < TileSize; x++ {
				ch := byte('.')
				if x < len(rowStr) {
					ch = rowStr[x]
				}
				c, ok := palette[ch]
				if !ok {
					// Case-insensitive fallback for accidental uppercase K.
					if ch == 'K' {
						c = palette['k']
					}
				}
				img.SetRGBA(ox+x, oy+y, c)
			}
		}
		frames[def.name] = sprite.Frame{
			Name:   def.name,
			X:      float32(ox),
			Y:      float32(oy),
			W:      TileSize,
			H:      TileSize,
			Width:  TileSize,
			Height: TileSize,
		}
	}

	return &sprite.Atlas{
		Image: core.ImageSource{
			Pixels:  img,
			Version: 1,
			Size: core.Size{
				Width:  core.Points(cols * TileSize),
				Height: core.Points(rows * TileSize),
			},
		},
		Frames: frames,
	}
}

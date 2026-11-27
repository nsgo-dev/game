package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// The component types a level scene's objects carry. A scene is a sceneformat scene file whose objects all sit at the top of their layer, each placed in meters by its position (x, y, posZ, with y up) and scale (scaleX, scaleY, scaleZ). Only the spawn is rotated, by rotY.
const (
	// compBlock is a solid box the size of the object's scale, centered on its position.
	compBlock = "block"
	// compSpawn is where the hero starts, at its feet, facing the object's rotY.
	compSpawn = "spawn"
	// compEnemy is where an enemy starts, at its feet.
	compEnemy = "enemy"
	// compKey is the key, which the hero picks up by walking into it.
	compKey = "key"
	// compDoor is a solid box that opens when its unlock handler runs.
	compDoor = "door"
	// compGoal is where the hero wins once it stands there.
	compGoal = "goal"
	// compLamp is a point light.
	compLamp = "lamp"
)

// The events and handlers the scene's connections can name.
const (
	eventCollected = "collected"
	eventOpened    = "opened"
	handlerUnlock  = "unlock"
)

// box is an axis-aligned box: its center, half its size and its color.
type box struct {
	center, half math3d.Vec3
	color        core.Color
}

// lamp is a point light placed in the scene.
type lamp struct {
	pos       math3d.Vec3
	color     core.Color
	intensity float32
	reach     float32
}

// level is a level scene built into what the game simulates and draws: the sky color behind it, its solid blocks, where the hero and the enemies start, the key, the door, the goal and the lamps.
type level struct {
	doc       *sceneformat.SceneDoc
	sky       core.Color
	blocks    []box
	spawn     math3d.Vec3
	spawnYaw  float32
	enemies   []math3d.Vec3
	keyID     string
	key       math3d.Vec3
	keyColor  core.Color
	doorID    string
	door      box
	goal      math3d.Vec3
	goalColor core.Color
	lamps     []lamp
}

// levelComponents registers the level's component types. The door's unlock handler calls unlock on the *game the scene's signals are connected to, so the scene file, not Go code, says which pickup opens which door.
func levelComponents() *sceneformat.ComponentRegistry {
	reg := sceneformat.NewComponentRegistry()
	color := func(def, doc string) sceneformat.Field {
		return sceneformat.Field{Name: "color", Kind: sceneformat.FieldString, Default: def, Doc: doc}
	}
	reg.MustRegister(sceneformat.ComponentType{Name: compBlock, Doc: "A solid box the size of the object's scale.", Fields: []sceneformat.Field{color("#8b8f98", "The box's CSS color.")}})
	reg.MustRegister(sceneformat.ComponentType{Name: compSpawn, Doc: "Where the hero starts, facing rotY."})
	reg.MustRegister(sceneformat.ComponentType{Name: compEnemy, Doc: "Where an enemy starts."})
	reg.MustRegister(sceneformat.ComponentType{
		Name:   compKey,
		Doc:    "A key the hero picks up by walking into it.",
		Fields: []sceneformat.Field{color("#facc15", "The key's CSS color.")},
		Events: []sceneformat.Event{{Name: eventCollected, Doc: "The hero picked up the key."}},
	})
	reg.MustRegister(sceneformat.ComponentType{
		Name:   compDoor,
		Doc:    "A solid door the size of the object's scale.",
		Fields: []sceneformat.Field{color("#92400e", "The door's CSS color.")},
		Events: []sceneformat.Event{{Name: eventOpened, Doc: "The door opened."}},
		Handlers: []sceneformat.Handler{{
			Name: handlerUnlock,
			Doc:  "Opens the door.",
			Func: func(c sceneformat.Call) { c.Context.(*game).unlock(c.Target.ID) },
		}},
	})
	reg.MustRegister(sceneformat.ComponentType{Name: compGoal, Doc: "Where the hero wins.", Fields: []sceneformat.Field{color("#22d3ee", "The goal's CSS color.")}})
	reg.MustRegister(sceneformat.ComponentType{
		Name: compLamp,
		Doc:  "A point light.",
		Fields: []sceneformat.Field{
			color("#ffd8a8", "The light's CSS color."),
			{Name: "intensity", Kind: sceneformat.FieldFloat, Default: 6.0, Min: sceneformat.Float64(0), Doc: "The light's brightness."},
			{Name: "range", Kind: sceneformat.FieldFloat, Default: 10.0, Min: sceneformat.Float64(0.1), Doc: "How far the light reaches, in meters."},
		},
	})
	return reg
}

// buildLevel checks a resolved level scene against reg and reads its objects: exactly one spawn, key, door and goal, any number of blocks, enemies and lamps.
func buildLevel(doc *sceneformat.SceneDoc, reg *sceneformat.ComponentRegistry) (*level, error) {
	if err := sceneformat.ValidateComponents(doc, reg); err != nil {
		return nil, err
	}
	l := &level{doc: doc, sky: skyColor}
	if doc.BackgroundColor != "" {
		// sceneformat.Validate has parsed it already.
		l.sky, _ = core.ParseColor(doc.BackgroundColor)
	}
	counts := map[string]int{}
	var err error
	for li := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, parent *sceneformat.ObjectDoc) bool {
			// The level reads each object's position and scale as world values and its blocks as boxes aligned with the axes, so it refuses what would place them elsewhere: an object inside another, whose transform is local to its parent, and a rotation other than the spawn's heading.
			if parent != nil {
				err = fmt.Errorf("object %q: a level's objects sit at the top of their layer, not inside %q", o.ID, parent.ID)
				return false
			}
			for _, name := range slices.Sorted(maps.Keys(o.Components)) {
				if o.Rotation != 0 || o.RotX != 0 || o.RotZ != 0 || (o.RotY != 0 && name != compSpawn) {
					err = fmt.Errorf("object %q: a %s is not rotated; only the spawn turns, by rotY", o.ID, name)
					return false
				}
				fields, ferr := reg.Resolve(name, asMap(o.Components[name]))
				if ferr != nil {
					err = fmt.Errorf("object %q: %w", o.ID, ferr)
					return false
				}
				counts[name]++
				pos := math3d.Vec3{X: o.X, Y: o.Y, Z: o.PosZ}
				half := math3d.Vec3{X: o.EffectiveScaleX(), Y: o.EffectiveScaleY(), Z: o.EffectiveScaleZ()}.Scale(0.5)
				col := func() core.Color {
					c, cerr := core.ParseColor(fields["color"].(string))
					if cerr != nil && err == nil {
						err = fmt.Errorf("object %q: %s color: %w", o.ID, name, cerr)
					}
					return c
				}
				switch name {
				case compBlock:
					l.blocks = append(l.blocks, box{center: pos, half: half, color: col()})
				case compSpawn:
					l.spawn, l.spawnYaw = pos, o.RotY
				case compEnemy:
					l.enemies = append(l.enemies, pos)
				case compKey:
					l.keyID, l.key, l.keyColor = o.ID, pos, col()
				case compDoor:
					l.doorID, l.door = o.ID, box{center: pos, half: half, color: col()}
				case compGoal:
					l.goal, l.goalColor = pos, col()
				case compLamp:
					l.lamps = append(l.lamps, lamp{pos: pos, color: col(), intensity: float32(fields["intensity"].(float64)), reach: float32(fields["range"].(float64))})
				}
			}
			return err == nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, name := range []string{compSpawn, compKey, compDoor, compGoal} {
		if counts[name] != 1 {
			return nil, fmt.Errorf("level: %d objects with a %s component, want 1", counts[name], name)
		}
	}
	if len(l.blocks) == 0 {
		return nil, errors.New("level: no blocks to stand on")
	}
	return l, nil
}

// asMap returns a component's YAML mapping, or nil for an empty one.
func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

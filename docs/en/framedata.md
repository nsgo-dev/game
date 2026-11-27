# framedata

`github.com/nsgo-dev/game/pkg/framedata` gives fighting games per-frame hitboxes, hurtboxes and hit properties (damage, hitstun, blockstun, knockback, cancel windows) for their moves. A character's frame data is a YAML file, and a `World` resolves it into hits once per fixed step, the same way every time.

```go
import "github.com/nsgo-dev/game/pkg/framedata"
```

## Frame data files

One file lists one character's moves. A move lasts `length` frames, one per fixed step of the [loop](loop.md), numbered from 1 as published frame data tables count them. Each box is active from frame `start` to frame `end` inclusive.

```yaml
version: 1
moves:
  - name: idle
    animation: idle
    length: 8
    loop: true
    boxes:
      - kind: hurt
        start: 1
        end: 8
        x: -12
        "y": -80
        width: 24
        height: 80
  - name: jab
    animation: jab
    length: 14
    boxes:
      - kind: hurt
        start: 1
        end: 14
        x: -12
        "y": -80
        width: 24
        height: 80
      - kind: hit
        start: 4
        end: 6
        x: 12
        "y": -66
        width: 28
        height: 12
        damage: 30
        hitstun: 14
        blockstun: 9
        knockbackX: 2.5
    cancels:
      - start: 4
        end: 10
        when: contact
        into: [fireball]
```

| Field | Meaning |
| --- | --- |
| `version` | Schema version, `1` |
| `moves[].name` | Unique name, used by `Start`, `Force`, `Neutral` and `into` |
| `moves[].animation` | Optional sprite animation the game plays alongside, such as an entry of a [sprite](sprite.md) atlas's `Animations` |
| `moves[].length` | Frames the move lasts, 1 to 3600 |
| `moves[].loop` | Starts over after the last frame, for stances such as idle, walk and crouch |
| `boxes[].kind` | `hurt` (can be hit) or `hit` (strikes) |
| `boxes[].start`, `end` | First and last active frame |
| `boxes[].x`, `"y"`, `width`, `height` | Rectangle in points from the fighter's origin when it faces right; Y grows downward |
| `boxes[].group` | Hit group, 0 to 63: hitboxes of one hit share a group, each hit of a multi-hit move has its own |
| `boxes[].damage`, `hitstun`, `blockstun` | Damage dealt and frames of stun on a hit or a block |
| `boxes[].knockbackX`, `knockbackY` | Push on hit, positive X away from the attacker: toward whichever side of the attacker the victim stands on, so a cross-up pushes the right way, or the attacker's facing when they share an X |
| `cancels[].start`, `end` | Frames the window is open on |
| `cancels[].when` | Omitted (always), `hit`, `block` or `contact` (hit or block) |
| `cancels[].into` | Moves the window may cancel into |

`Parse` refuses unknown fields, a version other than 1, duplicate move names, boxes and windows outside their move, sizes that are not positive and finite, hit properties on hurtboxes and cancels into moves that do not exist. `Encode` writes the fields in the order above with 2-space indentation, so loading and saving a file changes nothing. It quotes the key `"y"` because YAML 1.1 reads a bare `y` as a boolean; `Parse` accepts it either way.

`Move.FrameData` summarizes a move as a frame data table does: startup, active and recovery frames, and the advantage on hit and on block when its earliest hitbox connects on its first frame. For the jab above that is startup 4, 3 active, 8 recovery, +4 on hit and -1 on block.

## Fighters and hits

```go
doc, err := framedata.Parse(data)
if err != nil { ... }
p1 := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 100}
p2 := &framedata.Fighter{Moves: doc, Neutral: "idle", X: 160, FacingLeft: true}
world := &framedata.World{Fighters: []*framedata.Fighter{p1, p2}}

loop := &game.Loop{Rate: 60, Update: func(time.Duration) {
	if actions.JustPressed("punch") {
		p1.Start("jab")
	}
	p2.Blocking = actions.Pressed("back")
	for _, h := range world.Step() {
		health[h.Victim] -= h.Damage
		h.Victim.X += h.KnockbackX
		spawnSpark(h.X, h.Y)
	}
}}
```

- **`Start(name)`** begins a move when the fighter can act (no stun, and its move loops or is on its last frame) or when a cancel window of its current move is open on this frame and lists the move. `CanStart` asks without starting. **`Force(name)`** begins a move regardless, for hit reactions, throws and scripted moves.
- **`World.Step`** advances every fighter one frame, then tests each active hitbox against every other fighter's active hurtboxes. A fighter returns to its `Neutral` move when a move that does not loop ends.
- **Hits** are all found from the state before any of them applies, so fighters striking each other on the same frame trade. Fighters are visited in slice order and boxes in document order, so the same inputs give the same hits in the same order, which [replay](replay.md) and rollback [netplay](netplay.md) code rely on, together with saving and restoring a match (below).
- **Once per group**: a move instance hits each opponent once per hit group and at most once per step; starting a move or looping one begins a new instance.
- **Stun**: the victim takes the hitbox's `hitstun`, or its `blockstun` when `Blocking`, and returns to its `Neutral` move. A new hit replaces the stun left; several hits in one step take the longest. `Stun()` counts the steps left and `Actionable()` reports when it can act again.
- **Teams**: fighters with the same non-zero `Team` never hit each other.
- **Contact**: `Contact()` reports whether the current move hit or was blocked, which opens `when: hit`, `when: block` and `when: contact` windows.

`Step` reuses its result slice and allocates nothing once warm: two fighters take about 0.1 µs a step. A hitbox whose `group` is outside 0 to 63, which only a `Doc` built in Go without `Validate` can have, never hits.

## Save and restore

`World.Save` copies everything `Step` reads and changes into a `WorldState`: each fighter's position, facing, `Blocking`, move by name, frame, stun, contact and the hit groups its move has landed, with opponents named by their index in `Fighters`. `World.Restore` puts it back. A rollback session saves each tick and restores the last confirmed one when a late input arrives, then steps again to the same hits:

```go
var state framedata.WorldState

func (s *sim) Save() []byte {
	s.world.Save(&state)
	data, _ := json.Marshal(state)
	return data
}

func (s *sim) Load(data []byte) error {
	var st framedata.WorldState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	return s.world.Restore(&st)
}
```

`WorldState` holds no pointers, so it encodes as JSON or YAML and hashes for a checksum. `Save` reuses the slices of the state it is given and allocates nothing once warm, and the copy shares no memory with the `World`. `Restore` copies the state, and refuses one with a different number of fighters, a move the fighter's `Moves` does not have, a frame outside its move or a landed opponent that is not another fighter, changing nothing. The fighters' `Moves`, `Neutral` and `Team` are configuration and not part of the state.

## Debug overlay

The [gamedebug](gamedebug.md) overlay draws a fighter's active boxes as wireframes, hurtboxes first and hitboxes on top, mirrored as the fighter faces:

```go
overlay.DrawFighterBoxes(d, p1, core.RGB(0x38bdf8), core.RGB(0xef4444))
```

`Fighter.Boxes(dst)` returns the same boxes in world points for a game's own drawing or tests.

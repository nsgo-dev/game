# Coming from RPG Maker

A map from RPG Maker's database, maps, events, messages and plugins to this layer, with the page that covers each one, and the parts that are not here yet.

The biggest change is that there is no built-in RPG. RPG Maker gives you a database, a map editor, an event interpreter and a battle system before you write a line; here a game is Go code inside an NSGo component, the data it reads is YAML you write by hand or save from the [editor](editor.md), and the packages below are pieces you put together. The [rpg2d template](template-rpg2d.md) puts them together into a small working RPG, so it is the place to start.

## At a glance

| RPG Maker | Here | Guide |
| --- | --- | --- |
| Map, tilesets, autotiles | Tiled maps through `tilemap.LoadTiled`, terrains painted with `SetTerrain` | [tilemap](tilemap.md) |
| Transfer Player | `scene.Manager.Switch` with a transition | [scene](scene.md) |
| Database: Actors' and Enemies' parameters, States | `stats` sheets with add, multiply and override modifiers, timed on the fixed step | [stats](stats.md) |
| Database: Items, Weapons, Armors | `inventory` item definitions, stacking containers and typed equipment slots | [inventory](inventory.md) |
| Database: Skills | `ability` sets with costs, cooldowns, charges and cast times | [ability](ability.md) |
| Enemy drop items | `loot` weighted drop tables | [loot](loot.md) |
| Quests (a plugin in RPG Maker) | `quest` books with objectives, prerequisites and rewards | [quest](quest.md) |
| Show Text, Show Choices, message variables | `dialogue` running Yarn Spinner scripts with variables and conditions | [dialogue](dialogue.md) |
| Faces and pictures in messages | `vn` cast portraits, name plates and a typewriter text box | [vn](vn.md) |
| Play BGM, Play SE | [sound](sound.md) | [sound](sound.md) |
| Movies | `cutscene` video cutscenes | [cutscene](cutscene.md) |
| Save and load | `save` slots in versioned JSON with migrations | [save](save.md) |
| Plugins | `gameplugin` plugins compiled into the game | [Plugins](plugins.md) |
| Deployment | `go tool game release` | [Building and Shipping](shipping.md) |

## Not here yet

- **Database: Actors, Classes, Enemies and Troops** as one versioned YAML database with level curves and experience: tracked in [#1756](https://github.com/nsgo-dev/game/issues/1756).
- **Map events** with pages, conditions, triggers (action, touch, autorun, parallel), switches, self switches, variables and command lists run by a resumable interpreter: tracked in [#1755](https://github.com/nsgo-dev/game/issues/1755). Until then, `trigger2d` components on map objects with a scene's `connections`, Go handlers, and `dialogue` variables cover touch and action events ([Editor & Scene Format](editor.md#connections-490)).
- **Battles**: a deterministic turn-based battle runner with party and troop, turn order, skill scopes and formulas is tracked in [#1757](https://github.com/nsgo-dev/game/issues/1757). The rpg2d template has a battle scene of its own in Go to start from.

## First project

Create a project from the RPG template with `go run github.com/nsgo-dev/game/cmd/game@latest new -template rpg2d mygame`: a town and a meadow to walk, people with branching dialogue, an inventory with equipment, one quest, turn-based battles and three save slots ([guide](template-rpg2d.md)). Open it with `go tool game editor`: its 2D viewport takes the place of the map editor, and the files it saves are the same YAML the game loads.

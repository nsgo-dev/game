# RPG database

`github.com/nsgo-dev/game/pkg/rpgdata` provides a small, versioned YAML database for RPG records that are shared by templates and games. It keeps the data engine-agnostic so a real-time RPG or roguelike can use classes and progression without adopting a turn-based battle runner.

## Records

`Database` contains `Class`, `Actor`, `Skill`, `Enemy`, and `Troop` records. Classes hold level XP, per-level stat curves, and skills learned at a level. Actors reference a class, skills declare a target scope and optional formula and cost, enemies hold HP, experience, and gold, and troops place enemy references at coordinates.

`Parse` uses strict YAML fields and validates version, IDs, class references, and troop enemy references. `Encode` validates first and writes deterministic YAML. Keep one `version: 1` document under a project data directory until the editor's data-table integration is available.

## Progression

`NewProgression` creates a level and experience value. `AddExperience` returns each level reached in order, consumes the XP required for each level, and stops at `MaxLevel`. `Class.StatAt` clamps a requested level to the available curve entries.

The rpg2d and roguelike2d templates retain their existing records until a follow-up wires them to this shared format. Battle execution, state effects, and editor controls remain separate work tracked by the RPG issues.

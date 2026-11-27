package gamecli

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"text/tabwriter"

	"github.com/nsgo-dev/game/internal/gotoolchain"
)

// projectsFS holds the project templates game new offers, each under _templates/projects/<name> as a byte-for-byte copy of the template directory of its module in this repository, which TestTemplatesMatchTemplateModules keeps identical.
//
//go:embed all:_templates/projects
var projectsFS embed.FS

const (
	// templateRepo is the module path prefix of every module in this repository.
	templateRepo = "github.com/nsgo-dev/game/"
	// templateModuleRoot is the module that publishes the default template, starter, from its template directory; every other template is the module templateModuleRoot/<name>, in the repository directory template/<name>.
	templateModuleRoot = templateRepo + "template"
)

// projectTemplate is a project game new can start from, published as well as a Go module template that nsgo init -template writes.
type projectTemplate struct {
	// name is what -template takes: lower-case letters, digits and -, starting with a letter.
	name string
	// summary is the one line game new -list prints for it.
	summary string
	// module is the Go module whose template directory nsgo init -template writes: templateModuleRoot for starter and templateModuleRoot/<name> for every other template.
	module string
	// files are the project's files: a file ending in .tmpl, nsgo.yaml and main.go are rendered with text/template and newData's fields, as nsgo init -template renders a template module's, and the rest are copied.
	files fs.FS
}

// newProjectTemplate returns the template called name, whose files game new embeds under _templates/projects/<name>.
func newProjectTemplate(name, module, summary string) projectTemplate {
	return projectTemplate{name: name, summary: summary, module: module, files: mustSub(projectsFS, "_templates/projects/"+name)}
}

// projectTemplates are the templates game new offers, the default first and the rest in the order game new -list prints them. Adding a template adds its module under template/<name>, its copy under _templates/projects/<name> and one entry here, as docs/en/templates.md describes; CI finds it from its directory.
var projectTemplates = []projectTemplate{
	newProjectTemplate("starter", templateModuleRoot, "A tap-to-score 2D game: an animated sprite on a fixed-step loop, a native HUD and headless tests, the smallest project to grow from"),
	newProjectTemplate("action3d", templateModuleRoot+"/action3d", "A third-person 3D action adventure: an orbit camera, run, jump and attack animations on a state machine, melee enemies, and a key that opens the gate to the goal"),
	newProjectTemplate("cards2d", templateModuleRoot+"/cards2d", "A single-player 2D card game: drag and drop, a draw pile, a hand and a discard pile, rules in code and data in YAML, a seeded deal, saves, and a win and a loss played through by tests"),
	newProjectTemplate("fighting2d", templateModuleRoot+"/fighting2d", "A one-on-one 2D fighting game: two fighters with special moves from motion inputs, combos, guards, rounds and a timer, against the CPU or a second player"),
	newProjectTemplate("fps3d", templateModuleRoot+"/fps3d", "A first-person shooter arena in 3D: hitscan and projectile weapons, enemies that patrol, hunt and shoot, health and ammo pickups and three waves to clear"),
	newProjectTemplate("idle", templateModuleRoot+"/idle", "An idle game: generators, upgrades, large numbers, offline progress on return and prestige, with a win and a loss"),
	newProjectTemplate("platformer2d", templateModuleRoot+"/platformer2d", "A side-scrolling platformer: a controller with coyote time and jump buffering, three rooms with camera bounds, spikes, pits, sparks to collect, a checkpoint and a goal"),
	newProjectTemplate("puzzle2d", templateModuleRoot+"/puzzle2d", "A 2D grid puzzle: slide a spark to light every lamp, with levels in YAML, undo and redo, a move limit, stars per level and a level select that saves progress"),
	newProjectTemplate("racing3d", templateModuleRoot+"/racing3d", "A 3D racer: two laps against racing-line rivals with raycast cars, checkpoints, a countdown, a best-lap ghost and a results screen"),
	newProjectTemplate("rhythm", templateModuleRoot+"/rhythm", "Pulse Lanes, a four-lane rhythm game: charts in YAML on a music clock, timing judgements, combo, score and energy, and audio and video latency calibration"),
	newProjectTemplate("roguelike2d", templateModuleRoot+"/roguelike2d", "A turn-based roguelike: floors generated from a seed, bump-to-attack combat, weighted loot, stats with equipment and tonics, two abilities, permadeath and a run summary"),
	newProjectTemplate("rpg2d", templateModuleRoot+"/rpg2d", "A top-down RPG: a town and a meadow, people with branching dialogue, an inventory with equipment, one quest, turn-based battles and three save slots"),
	newProjectTemplate("rts2d", templateModuleRoot+"/rts2d", "A 2D real-time strategy skirmish: gather crystals, build barracks, train three unit types and destroy a computer opponent's base, with fog of war, box selection and control groups"),
	newProjectTemplate("shmup2d", templateModuleRoot+"/shmup2d", "A vertical scrolling shooter: enemy waves and a stage in YAML, bullet patterns, power-ups, bombs, a boss with phases, lives and a score"),
	newProjectTemplate("towerdefense2d", templateModuleRoot+"/towerdefense2d", "A 2D tower defense game: towers with upgrades beside a road enemies follow, waves defined in YAML, money and lives, with keyboard, gamepad and touch controls"),
	newProjectTemplate("visualnovel", templateModuleRoot+"/visualnovel", "A short branching visual novel: two characters, drawn backgrounds, choices that change the ending, a backlog, auto and skip modes, save slots and two languages"),
}

// repoDir returns the slash-separated directory of this repository that holds t's files, the template directory of its module.
func (t projectTemplate) repoDir() string {
	return strings.TrimPrefix(t.module, templateRepo) + "/template"
}

// templateNames returns the names of projectTemplates, in order.
func templateNames() []string {
	names := make([]string, len(projectTemplates))
	for i, t := range projectTemplates {
		names[i] = t.name
	}
	return names
}

// writeTemplateList writes one line per template to w, its name, the default marked, beside its summary.
func writeTemplateList(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for i, t := range projectTemplates {
		name := t.name
		if i == 0 {
			name += " (default)"
		}
		fmt.Fprintf(tw, "%s\t%s\n", name, t.summary)
	}
	return tw.Flush()
}

// mustSub returns the subtree of fsys at dir, which is embedded and so always there.
func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// templateMinGoVersion returns the highest `go` directive version required across all
// embedded project templates' go.mod.tmpl (or go.mod) files, falling back to gotoolchain.DefaultMinVersion.
func templateMinGoVersion() string {
	var versions []string
	for _, t := range projectTemplates {
		for _, name := range []string{"go.mod.tmpl", "go.mod"} {
			data, err := fs.ReadFile(t.files, name)
			if err != nil {
				continue
			}
			if v, ok := gotoolchain.ParseGoModMinVersion(data); ok {
				versions = append(versions, v)
			}
			break
		}
	}
	if maxV := gotoolchain.MaxGoVersion(versions...); maxV != "" {
		return maxV
	}
	return gotoolchain.DefaultMinVersion
}

package main

import (
	"bytes"
	"math"
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/debug"
	"github.com/nsgo-dev/game/pkg/playtest"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/game/pkg/save"
)

func dirName(d dir) string {
	switch d {
	case dirUp:
		return "up"
	case dirDown:
		return "down"
	case dirLeft:
		return "left"
	case dirRight:
		return "right"
	default:
		return ""
	}
}

func makeRPGPlaytestSim(saveDir string) playtest.SimFactory {
	return func(_ uint64, src replay.ActionSource, metrics *playtest.Collector) (playtest.Sim, error) {
		c, err := loadContent()
		if err != nil {
			return playtest.Sim{}, err
		}
		g := newGame(c, save.NewFileStore(saveDir, save.WithVersion(saveVersion)), Project.Title())
		g.startNewGame()

		// Count all walkable tiles across Hollowbrook and Whisper Meadow.
		totalWalkable := 0
		for i := range c.world.Maps {
			m := &c.world.Maps[i]
			w, h := m.Size()
			for y := range h {
				for x := range w {
					if !c.world.solidAt(m, Cell{x, y}) {
						totalWalkable++
					}
				}
			}
		}

		type mapCell struct {
			mapID string
			x, y  int
		}
		seen := make(map[mapCell]bool)
		talked := make(map[string]bool)
		markSeen := func() {
			const vision = 3
			w, h := g.level.Size()
			px, py := g.pos.X(), g.pos.Y()
			for y := max(0, py-vision); y <= min(h-1, py+vision); y++ {
				for x := max(0, px-vision); x <= min(w-1, px+vision); x++ {
					dx, dy := x-px, y-py
					if dx*dx+dy*dy <= vision*vision && !c.world.solidAt(g.level, Cell{x, y}) {
						seen[mapCell{g.mapID, x, y}] = true
					}
				}
			}
		}
		markSeen()

		stats := &debug.FrameStats{}
		started := false
		prevDir := dirNone
		prevMap := g.mapID
		prevPos := g.pos
		stepCount := 0

		totalPeople := 0
		for i := range c.world.Maps {
			totalPeople += len(c.world.Maps[i].People)
		}
		totalItems := 0
		for i := range c.world.Maps {
			totalItems += len(c.world.Maps[i].Items)
		}

		const simStep = time.Second / 60

		observe := func() any {
			w, h := g.level.Size()
			npcs := make([]playtest.ExplorerNPC, len(g.level.People))
			for i, p := range g.level.People {
				npcs[i] = playtest.ExplorerNPC{
					ID:     p.ID,
					Cell:   playtest.GridCell{X: p.At.X(), Y: p.At.Y()},
					Talked: talked[p.ID],
				}
			}
			chests := make([]playtest.ExplorerChest, len(g.level.Items))
			for i, it := range g.level.Items {
				chests[i] = playtest.ExplorerChest{
					ID:     it.ID,
					Cell:   playtest.GridCell{X: it.At.X(), Y: it.At.Y()},
					Opened: g.taken[it.ID],
					Solid:  false,
				}
			}
			exits := make([]playtest.GridCell, len(g.level.Exits))
			for i, ex := range g.level.Exits {
				exits[i] = playtest.GridCell{X: ex.At.X(), Y: ex.At.Y()}
			}

			// Keep the explorer out of Thornhide's tile until equipped, since TopDownExplorer does not open the pack menu.
			bossCells := make(map[Cell]bool)
			if !g.owns("iron-sword") {
				for _, f := range g.foes {
					if f.kind.Boss {
						bossCells[f.at] = true
					}
				}
			}

			return playtest.ExplorerObservation{
				MapID:      g.mapID,
				Width:      w,
				Height:     h,
				Walkable:   func(x, y int) bool { return !c.world.solidAt(g.level, Cell{x, y}) && !bossCells[Cell{x, y}] },
				Player:     playtest.GridCell{X: g.pos.X(), Y: g.pos.Y()},
				Facing:     dirName(g.facing),
				Moving:     g.walk > simStep,
				InDialogue: g.mode == modeTalk,
				InBattle:   g.mode == modeBattle,
				NPCs:       npcs,
				Chests:     chests,
				Exits:      exits,
			}
		}

		stepFn := func(dt time.Duration) {
			src.Step()
			if !started {
				started = true
				metrics.StartTimer("goal_time")
				metrics.SetCounter("tiles_total", float64(totalWalkable))
				metrics.SetCounter("tiles_seen", float64(len(seen)))
			}
			stepCount++

			in := intent{
				confirm: src.JustPressed(actionConfirm),
			}
			x, y := src.Axis(axisX), src.Axis(axisY)
			switch {
			case math.Abs(x) < stickThreshold && math.Abs(y) < stickThreshold:
			case math.Abs(y) >= math.Abs(x) && y < 0:
				in.dir = dirUp
			case math.Abs(y) >= math.Abs(x):
				in.dir = dirDown
			case x < 0:
				in.dir = dirLeft
			default:
				in.dir = dirRight
			}
			in.turned = in.dir != dirNone && in.dir != prevDir
			prevDir = in.dir

			wasTalk := g.mode == modeTalk
			talkTarget := ""
			if g.mode == modeField && in.confirm {
				if p := g.personAt(g.pos.step(g.facing)); p != nil {
					talkTarget = p.ID
				}
			}

			g.update(dt, in)

			if !wasTalk && g.mode == modeTalk && talkTarget != "" {
				talked[talkTarget] = true
			}

			// Deterministic simulated frame-time readout so repeated headless runs stay byte-identical.
			stats.Record(2800*time.Microsecond, 2000*time.Microsecond, 800*time.Microsecond)

			markSeen()
			metrics.SetCounter("tiles_seen", float64(len(seen)))

			// Record one tile_visit event whenever the player enters a new tile (keeps the heat map informative and compact).
			if g.mapID != prevMap || g.pos != prevPos || stepCount == 1 {
				prevMap = g.mapID
				prevPos = g.pos
				ts := float64(c.world.TileSize)
				metrics.Event("tile_visit", (float64(g.pos.X())+0.5)*ts, (float64(g.pos.Y())+0.5)*ts, g.mapID)
			}

			if g.mode == modeLost {
				metrics.Fail("defeated in battle")
				return
			}

			// Goal: talk to every NPC in Hollowbrook, gather every item (including all three glowcaps), and explore at least 85% of the world.
			if len(talked) >= totalPeople && len(g.taken) >= totalItems && len(seen)*100 >= totalWalkable*85 {
				metrics.StopTimer("goal_time")
				metrics.Finish()
			}
		}

		return playtest.Sim{
			Step:       stepFn,
			Observe:    observe,
			FrameStats: stats,
		}, nil
	}
}

// TestPlaytest runs playtest.yaml's topdown-explorer bot across Hollowbrook and Whisper Meadow.
func TestPlaytest(t *testing.T) {
	playtest.RunTest(t, playtest.FileName, makeRPGPlaytestSim(t.TempDir()))
}

// TestBotReportCanonicalAndDeterministic checks that playtest.yaml is canonical and repeated runs on the same seeds write byte-identical reports.
func TestBotReportCanonicalAndDeterministic(t *testing.T) {
	raw, err := os.ReadFile(playtest.FileName)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := playtest.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := playtest.EncodeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, encoded) {
		t.Fatalf("playtest.yaml is not canonical;\n%s", encoded)
	}
	cfg.Runs.Count = 2
	makeSim := makeRPGPlaytestSim(t.TempDir())
	r1, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := playtest.Run(cfg, makeSim, playtest.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	y1, err := playtest.EncodeReport(r1.Report)
	if err != nil {
		t.Fatal(err)
	}
	y2, err := playtest.EncodeReport(r2.Report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("reports differ for identical seeds:\n--- 1 ---\n%s\n--- 2 ---\n%s", y1, y2)
	}
}

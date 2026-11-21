//go:build !release

package debug_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestConsoleCommandsAndQuotedArgs(t *testing.T) {
	c := debug.NewConsole()
	var gotArgs []string
	c.RegisterCommand("echo", "echo arguments back", func(args []string) (string, error) {
		gotArgs = slices.Clone(args)
		return strings.Join(args, "|"), nil
	})

	out, err := c.Exec(`echo "hello world" 'single quoted' plain \"escaped\"`)
	if err != nil {
		t.Fatalf("Exec(echo) error = %v", err)
	}
	wantArgs := []string{"hello world", "single quoted", "plain", `"escaped"`}
	if !slices.Equal(gotArgs, wantArgs) {
		t.Fatalf("echo args = %#v, want %#v", gotArgs, wantArgs)
	}
	if out != `hello world|single quoted|plain|"escaped"` {
		t.Fatalf("echo output = %q", out)
	}

	// Unterminated quote returns a clear error.
	if _, err := c.Exec(`echo "unterminated`); err == nil {
		t.Fatal("Exec with unterminated quote succeeded, want error")
	}

	// Unknown command returns a clear error.
	if _, err := c.Exec("nope"); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Exec(nope) err = %v, want unknown command error", err)
	}

	// Built-in help lists built-ins and custom commands.
	helpOut, err := c.Exec("help")
	if err != nil {
		t.Fatalf("Exec(help) error = %v", err)
	}
	for _, want := range []string{"clear", "get", "help", "list", "set", "echo", "echo arguments back"} {
		if !strings.Contains(helpOut, want) {
			t.Fatalf("help output %q missing %q", helpOut, want)
		}
	}

	// Single command help.
	echoHelp, err := c.Exec("help echo")
	if err != nil || !strings.Contains(echoHelp, "echo arguments back") {
		t.Fatalf("Exec(help echo) = (%q, %v)", echoHelp, err)
	}

	// Clear empties output entries while retaining history.
	if len(c.Entries()) == 0 {
		t.Fatal("expected non-empty entries before clear")
	}
	if _, err := c.Exec("clear"); err != nil {
		t.Fatalf("Exec(clear) error = %v", err)
	}
	if len(c.Entries()) != 0 {
		t.Fatalf("entries after clear = %d, want 0", len(c.Entries()))
	}
	if len(c.History()) == 0 {
		t.Fatal("clear erased command history, want history preserved")
	}
}

func TestConsoleTunablesGetSetListAndRangeErrors(t *testing.T) {
	c := debug.NewConsole()
	gravity := 9.8
	maxEnemies := 10
	godMode := false

	c.RegisterFloat("gravity", 0, 50, &gravity)
	c.RegisterInt("max_enemies", 1, 100, &maxEnemies)
	c.RegisterBool("god_mode", &godMode)

	listOut, err := c.Exec("list")
	if err != nil {
		t.Fatalf("Exec(list) error = %v", err)
	}
	for _, want := range []string{"god_mode = false", "gravity = 9.8 [0, 50]", "max_enemies = 10 [1, 100]"} {
		if !strings.Contains(listOut, want) {
			t.Fatalf("list output %q missing %q", listOut, want)
		}
	}

	// Valid updates.
	if out, err := c.Exec("set gravity 24.5"); err != nil || out != "gravity = 24.5" || gravity != 24.5 {
		t.Fatalf("set gravity = (%q, %v), gravity=%v", out, err, gravity)
	}
	if out, err := c.Exec("get gravity"); err != nil || out != "24.5" {
		t.Fatalf("get gravity = (%q, %v), want 24.5", out, err)
	}
	if _, err := c.Exec("set max_enemies 42"); err != nil || maxEnemies != 42 {
		t.Fatalf("set max_enemies err=%v, val=%d", err, maxEnemies)
	}
	if _, err := c.Exec("set god_mode on"); err != nil || !godMode {
		t.Fatalf("set god_mode on err=%v, val=%v", err, godMode)
	}
	if _, err := c.Exec("set god_mode false"); err != nil || godMode {
		t.Fatalf("set god_mode false err=%v, val=%v", err, godMode)
	}

	// Out-of-range and invalid values MUST fail without mutating variables.
	badCases := []string{
		"set gravity -0.1",
		"set gravity 50.1",
		"set gravity NaN",
		"set gravity +Inf",
		"set max_enemies 0",
		"set max_enemies 101",
		"set max_enemies notanint",
		"set god_mode maybe",
		"set unknown_var 1",
		"get unknown_var",
	}
	for _, cmd := range badCases {
		if _, err := c.Exec(cmd); err == nil {
			t.Fatalf("Exec(%q) succeeded, want error", cmd)
		}
	}
	if math.Abs(gravity-24.5) > 1e-9 || maxEnemies != 42 || godMode != false {
		t.Fatalf("variables mutated after rejected commands: gravity=%v maxEnemies=%d godMode=%v", gravity, maxEnemies, godMode)
	}
}

func TestConsoleHistoryAndTabCompletion(t *testing.T) {
	c := debug.NewConsole()
	speed := 5.0
	scale := 1.0
	c.RegisterFloat("speed", 0, 100, &speed)
	c.RegisterFloat("scale", 0, 10, &scale)
	c.RegisterCommand("spawn", "spawn entity", func([]string) (string, error) { return "ok", nil })
	c.RegisterCommand("stats", "show stats", func([]string) (string, error) { return "ok", nil })

	if c.HistoryPrev() != "" || c.HistoryNext() != "" {
		t.Fatal("empty history returned non-empty entry")
	}

	_, _ = c.Exec("spawn")
	_, _ = c.Exec("set speed 12")
	_, _ = c.Exec("get speed")

	wantHist := []string{"spawn", "set speed 12", "get speed"}
	if got := c.History(); !slices.Equal(got, wantHist) {
		t.Fatalf("History() = %#v, want %#v", got, wantHist)
	}

	if got := c.HistoryPrev(); got != "get speed" {
		t.Fatalf("HistoryPrev 1 = %q, want get speed", got)
	}
	if got := c.HistoryPrev(); got != "set speed 12" {
		t.Fatalf("HistoryPrev 2 = %q, want set speed 12", got)
	}
	if got := c.HistoryPrev(); got != "spawn" {
		t.Fatalf("HistoryPrev 3 = %q, want spawn", got)
	}
	// Clamps at oldest.
	if got := c.HistoryPrev(); got != "spawn" {
		t.Fatalf("HistoryPrev clamp = %q, want spawn", got)
	}
	if got := c.HistoryNext(); got != "set speed 12" {
		t.Fatalf("HistoryNext 1 = %q, want set speed 12", got)
	}
	if got := c.HistoryNext(); got != "get speed" {
		t.Fatalf("HistoryNext 2 = %q, want get speed", got)
	}
	if got := c.HistoryNext(); got != "" {
		t.Fatalf("HistoryNext past end = %q, want empty", got)
	}

	// Command completion.
	if got := c.Complete("s"); !slices.Equal(got, []string{"set", "spawn", "stats"}) {
		t.Fatalf("Complete(s) = %#v, want [set spawn stats]", got)
	}
	if got := c.Complete("he"); !slices.Equal(got, []string{"help"}) {
		t.Fatalf("Complete(he) = %#v, want [help]", got)
	}
	// Tunable completion for set and get.
	if got := c.Complete("set s"); !slices.Equal(got, []string{"set scale", "set speed"}) {
		t.Fatalf("Complete(set s) = %#v, want [set scale set speed]", got)
	}
	if got := c.Complete("get sp"); !slices.Equal(got, []string{"get speed"}) {
		t.Fatalf("Complete(get sp) = %#v, want [get speed]", got)
	}
	if got := c.Complete("help sp"); !slices.Equal(got, []string{"help spawn"}) {
		t.Fatalf("Complete(help sp) = %#v, want [help spawn]", got)
	}
}

func TestConsoleStoragePersistence(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{}
	})
	st := app.Storage()

	c1 := debug.NewConsole()
	g1 := 9.8
	n1 := 5
	b1 := false
	c1.RegisterFloat("gravity", 0, 100, &g1)
	c1.RegisterInt("lives", 1, 99, &n1)
	c1.RegisterBool("debug_ai", &b1)

	_, _ = c1.Exec("set gravity 33.25")
	_, _ = c1.Exec("set lives 12")
	_, _ = c1.Exec("set debug_ai true")

	if err := c1.SaveTunables(st, "dev.tunables"); err != nil {
		t.Fatalf("SaveTunables error = %v", err)
	}

	// Restore into a fresh console with different initial values.
	c2 := debug.NewConsole()
	g2 := 1.0
	n2 := 1
	b2 := false
	c2.RegisterFloat("gravity", 0, 100, &g2)
	c2.RegisterInt("lives", 1, 99, &n2)
	c2.RegisterBool("debug_ai", &b2)

	if err := c2.LoadTunables(st, "dev.tunables"); err != nil {
		t.Fatalf("LoadTunables error = %v", err)
	}
	if g2 != 33.25 || n2 != 12 || !b2 {
		t.Fatalf("restored tunables = (%v, %d, %v), want (33.25, 12, true)", g2, n2, b2)
	}

	// Missing key is a clean no-op.
	if err := c2.LoadTunables(st, "does.not.exist"); err != nil {
		t.Fatalf("LoadTunables(missing) error = %v", err)
	}

	// Out-of-range persisted value fails without mutating current values.
	if err := st.SetItem("dev.bad", `{"gravity": 999}`); err != nil {
		t.Fatalf("SetItem error = %v", err)
	}
	if err := c2.LoadTunables(st, "dev.bad"); err == nil {
		t.Fatal("LoadTunables(out-of-range) succeeded, want error")
	}
	if g2 != 33.25 {
		t.Fatalf("gravity mutated to %v after rejected LoadTunables", g2)
	}
}

func TestConsoleViewInteractionAndOverlayComposition(t *testing.T) {
	c := debug.NewConsole()
	gravity := 9.8
	c.RegisterFloat("gravity", 0, 100, &gravity)
	c.RegisterCommand("ping", "reply pong", func(args []string) (string, error) {
		return "pong", nil
	})

	dbg := debug.New(debug.Options{
		Visible: true,
		Console: c,
	})

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style:    core.Style{Width: core.Pt(600), Height: core.Pt(400)},
			Children: []core.Node{dbg.View(cx)},
		}
	})

	if !app.Exists("debug-overlay") || !app.Exists("debug-console") {
		t.Fatal("expected both debug-overlay and debug-console mounted")
	}
	if got := app.Find("debug-overlay").Props.PointerEvents; got != core.PointerEventsNone {
		t.Fatalf("debug-overlay PointerEvents = %v, want PointerEventsNone", got)
	}

	// Type and submit a tunable update via Return.
	app.Type("debug-console-input", "set gravity 42.5")
	app.Submit("debug-console-input")
	if gravity != 42.5 {
		t.Fatalf("gravity = %v after console submit, want 42.5", gravity)
	}
	if !slices.Contains(app.Texts(), "gravity = 42.5") {
		t.Fatalf("console output %v missing 'gravity = 42.5'", app.Texts())
	}

	// Type and execute via the Run button.
	app.Type("debug-console-input", "ping")
	app.Press("debug-console-run")
	if !slices.Contains(app.Texts(), "pong") {
		t.Fatalf("console output %v missing 'pong'", app.Texts())
	}

	// Unknown command shows error in output.
	app.Type("debug-console-input", "bogus")
	app.Submit("debug-console-input")
	foundErr := false
	for _, txt := range app.Texts() {
		if strings.Contains(txt, "unknown command") {
			foundErr = true
			break
		}
	}
	if !foundErr {
		t.Fatalf("console output %v missing unknown command error", app.Texts())
	}
}

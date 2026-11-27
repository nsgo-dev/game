package gamecli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/tooling/audioconv"
)

// newTestApp returns an app writing to buffers it also returns, whose editor launch fails the test: tests that open the editor replace it.
func newTestApp(t *testing.T) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	return &app{
		stdout: &stdout,
		stderr: &stderr,
		audio:  &audioconv.Encoder{},
		launch: func(*editor.Editor) { t.Error("editor launched") },
		nsgo: func(args []string, _, _ io.Writer) error {
			t.Errorf("go tool nsgo %v ran", args)
			return nil
		},
		goRun: func(dir string, args []string, _, _ io.Writer) error {
			t.Errorf("go %v ran in %s", args, dir)
			return nil
		},
		recentProjectsPath: filepath.Join(t.TempDir(), "recent-projects.yaml"),
		layoutPath:         filepath.Join(t.TempDir(), "layout.yaml"),
	}, &stdout, &stderr
}

func TestHelpListsEveryCommand(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"-help"}, {"--help"}} {
		a, stdout, stderr := newTestApp(t)
		if code := run(args, a); code != 0 {
			t.Fatalf("%v exit %d (stderr: %s)", args, code, stderr)
		}
		out := stdout.String()
		for _, c := range commands {
			if !strings.Contains(out, "\n  "+c.name+" ") || !strings.Contains(out, c.summary) {
				t.Errorf("%v does not list %s:\n%s", args, c.name, out)
			}
		}
		if !strings.Contains(out, "\n  help ") {
			t.Errorf("%v does not list help:\n%s", args, out)
		}
		if stderr.Len() != 0 {
			t.Errorf("%v wrote to stderr: %q", args, stderr)
		}
	}
}

func TestEveryFormerCommandIsASubcommand(t *testing.T) {
	want := []string{"pack", "audio", "manifest", "tex", "editor", "mcp", "release", "replay", "playtest", "bench", "new", "dev", "check", "assets", "generate", "mv", "index", "library", "credits", "pixelfx", "navmesh", "merge", "schema", "completion"}
	if len(commands) != len(want) {
		t.Fatalf("%d commands, want %d", len(commands), len(want))
	}
	for i, name := range want {
		if commands[i].name != name {
			t.Errorf("commands[%d] = %s, want %s", i, commands[i].name, name)
		}
		if lookup(name) != commands[i] {
			t.Errorf("lookup(%q) does not find it", name)
		}
	}
}

func TestHelpForEachCommand(t *testing.T) {
	for _, c := range commands {
		a, stdout, stderr := newTestApp(t)
		if code := run([]string{"help", c.name}, a); code != 0 {
			t.Fatalf("help %s exit %d (stderr: %s)", c.name, code, stderr)
		}
		out := stdout.String()
		if !strings.HasPrefix(out, "usage: game "+c.name+" [flags]") || !strings.Contains(out, c.summary) {
			t.Errorf("help %s:\n%s", c.name, out)
		}

		// -h on the command shows the same usage, on stderr as the flag package writes it.
		a, _, stderr = newTestApp(t)
		if code := run([]string{c.name, "-h"}, a); code != 0 {
			t.Fatalf("%s -h exit %d", c.name, code)
		}
		if stderr.String() != out {
			t.Errorf("%s -h = %q, want help's %q", c.name, stderr, out)
		}
	}

	a, stdout, _ := newTestApp(t)
	if code := run([]string{"help", "pack"}, a); code != 0 {
		t.Fatal(code)
	}
	for _, flag := range []string{"-in", "-out", "-padding", "-trim"} {
		if !strings.Contains(stdout.String(), "  "+flag+" ") && !strings.Contains(stdout.String(), "  "+flag+"\n") {
			t.Errorf("help pack does not show %s:\n%s", flag, stdout)
		}
	}

	a, stdout, _ = newTestApp(t)
	if code := run([]string{"help", "help"}, a); code != 0 || !strings.HasPrefix(stdout.String(), "usage: game help") {
		t.Fatalf("help help exit %d:\n%s", code, stdout)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "usage: game <command>"},
		{[]string{"build"}, `game: unknown command "build"`},
		{[]string{"help", "build"}, `game help: unknown command "build"`},
		{[]string{"help", "pack", "audio"}, "usage: game help [command]"},
		{[]string{"pack", "-nope"}, "flag provided but not defined: -nope"},
		{[]string{"mcp", "install", "-nope"}, "flag provided but not defined: -nope"},
		{[]string{"mcp", "install", "-project"}, "flag needs an argument: -project"},
		{[]string{"mcp", "install", "extra"}, "game mcp: expected no arguments after install, got 1"},
	}
	for _, tc := range cases {
		a, stdout, stderr := newTestApp(t)
		if code := run(tc.args, a); code != 2 {
			t.Errorf("%q exit %d, want 2", tc.args, code)
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("%q stderr = %q, want %q", tc.args, stderr, tc.want)
		}
		if stdout.Len() != 0 {
			t.Errorf("%q wrote to stdout: %q", tc.args, stdout)
		}
	}
}

func TestMCPInstallHelpAndUsage(t *testing.T) {
	a, helpOut, helpErr := newTestApp(t)
	if code := run([]string{"help", "mcp"}, a); code != 0 || helpErr.Len() != 0 {
		t.Fatalf("help mcp exit %d (stderr: %s)", code, helpErr)
	}
	wantUsage := helpOut.String()

	for _, args := range [][]string{
		{"mcp", "install", "-h"},
		{"mcp", "install", "-help"},
		{"mcp", "install", "--help"},
	} {
		a, stdout, stderr := newTestApp(t)
		if code := run(args, a); code != 0 {
			t.Errorf("%v exit %d, want 0 (stderr: %s)", args, code, stderr)
		}
		if stdout.Len() != 0 {
			t.Errorf("%v wrote to stdout: %q", args, stdout)
		}
		if stderr.String() != wantUsage {
			t.Errorf("%v stderr = %q, want %q", args, stderr, wantUsage)
		}
		if got := strings.Count(stderr.String(), "usage: "); got != 1 {
			t.Errorf("%v printed usage %d times, want 1:\n%s", args, got, stderr)
		}
	}

	for _, tc := range []struct {
		args    []string
		wantErr string
	}{
		{[]string{"mcp", "install", "-nope"}, "flag provided but not defined: -nope"},
		{[]string{"mcp", "install", "-project"}, "flag needs an argument: -project"},
		{[]string{"mcp", "install", "extra"}, "game mcp: expected no arguments after install, got 1"},
	} {
		a, stdout, stderr := newTestApp(t)
		if code := run(tc.args, a); code != 2 {
			t.Errorf("%v exit %d, want 2", tc.args, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("%v wrote to stdout: %q", tc.args, stdout)
		}
		got := stderr.String()
		if n := strings.Count(got, tc.wantErr); n != 1 {
			t.Errorf("%v error %q appeared %d times, want 1:\n%s", tc.args, tc.wantErr, n, got)
		}
		if n := strings.Count(got, wantUsage); n != 1 {
			t.Errorf("%v usage appeared %d times, want 1:\n%s", tc.args, n, got)
		}
		if strings.Contains(got, "Usage of ") {
			t.Errorf("%v printed Go's default usage:\n%s", tc.args, got)
		}
	}
}

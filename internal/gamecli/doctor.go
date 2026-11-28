package gamecli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/pkg/project"
)

type doctorFinding struct {
	Severity string `json:"severity"`
	File     string `json:"file,omitempty"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

var doctorCommand = &command{name: "doctor", args: "[dir]", summary: "Check a project module, game.yaml and nsgo.yaml before building or releasing", define: defineDoctor, complete: map[string]completion{"": filePaths}}

func defineDoctor(a *app, fs *flag.FlagSet) func() error {
	jsonOut := fs.Bool("json", false, "write findings as JSON")
	return func() error {
		dir := "."
		if fs.NArg() > 1 {
			return usagef("expected at most one directory")
		}
		if fs.NArg() == 1 {
			dir = fs.Arg(0)
		}
		findings := doctor(dir)
		if *jsonOut {
			enc := json.NewEncoder(a.stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(findings); err != nil {
				return err
			}
		} else {
			for _, f := range findings {
				loc := f.File
				if loc != "" {
					loc += ": "
				}
				fmt.Fprintf(a.stdout, "%s%s: %s", loc, f.Severity, f.Message)
				if f.Fix != "" {
					fmt.Fprintf(a.stdout, "; fix: %s", f.Fix)
				}
				fmt.Fprintln(a.stdout)
			}
		}
		for _, f := range findings {
			if f.Severity == "error" {
				return errors.New("project needs attention")
			}
		}
		if len(findings) == 0 {
			fmt.Fprintln(a.stdout, "doctor: no problems")
		}
		return nil
	}
}

func doctor(dir string) []doctorFinding {
	var out []doctorFinding
	add := func(sev, file, msg, fix string) { out = append(out, doctorFinding{sev, file, msg, fix}) }
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		add("error", "go.mod", "cannot read module file", "run doctor in a Go module")
		return out
	}
	text := string(data)
	if !strings.Contains(text, "tool github.com/nsgo-dev/game/cmd/game") {
		add("error", "go.mod", "the game tool is not declared", "run go get -tool github.com/nsgo-dev/game/cmd/game")
	}
	if !strings.Contains(text, "github.com/nsgo-dev/game") {
		add("error", "go.mod", "the game module is not required", "run go get github.com/nsgo-dev/game")
	}
	if !strings.Contains(text, "github.com/nsgo-dev/nsgo") {
		add("error", "go.mod", "the NSGo module is not required", "run go get -tool github.com/nsgo-dev/nsgo/cmd/nsgo")
	}
	f, err := project.Check(os.DirFS(dir))
	if err != nil {
		add("error", "game.yaml", err.Error(), "run game project check and game project sync")
	}
	if f == nil {
		return out
	}
	if strings.HasPrefix(f.ID, "dev.nsgo.") {
		add("warning", "game.yaml", fmt.Sprintf("app ID %q is a default dev.nsgo ID", f.ID), "set a release application ID")
	}
	if f.Export == nil || len(f.Export.Presets) == 0 {
		add("note", "game.yaml", "no export presets are declared", "add export.presets before making a release")
	}
	return out
}

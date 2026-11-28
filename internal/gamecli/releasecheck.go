package gamecli

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editormcp"
	"go.yaml.in/yaml/v3"
)

type releaseCheckReport struct {
	Version  int            `yaml:"version"`
	Template string         `yaml:"template"`
	OS       string         `yaml:"os"`
	Passed   bool           `yaml:"passed"`
	Checks   []releaseCheck `yaml:"checks"`
}

type releaseCheck struct {
	Name     string        `yaml:"name"`
	Status   string        `yaml:"status"`
	Duration time.Duration `yaml:"duration"`
	Error    string        `yaml:"error,omitempty"`
}

func runMCPReleaseSelftest(a *app, dir, out string) error {
	started := time.Now()
	rep := releaseCheckReport{Version: 1, Template: filepathBase(dir), OS: runtime.GOOS, Passed: true}
	check := func(name string, fn func() error) {
		began := time.Now()
		item := releaseCheck{Name: name, Status: "pass"}
		if err := fn(); err != nil {
			item.Status, item.Error, rep.Passed = "fail", err.Error(), false
		}
		item.Duration = time.Since(began)
		rep.Checks = append(rep.Checks, item)
	}
	check("project", func() error { _, err := os.Stat(dir); return err })
	check("editor-mcp-tools", func() error {
		ed, err := editor.NewDemo()
		if err != nil {
			return err
		}
		defer ed.Close()
		s := editormcp.New(ed, editormcp.Options{})
		defer s.Close()
		if len(s.Tools()) == 0 {
			return fmt.Errorf("editor MCP registered no tools")
		}
		return nil
	})
	data, err := yaml.Marshal(rep)
	if err != nil {
		return err
	}
	if out == "" {
		out = "release-check.yaml"
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "release MCP selftest: %s (%s, %s)\n", map[bool]string{true: "passed", false: "failed"}[rep.Passed], out, time.Since(started).Round(time.Millisecond))
	if !rep.Passed {
		return fmt.Errorf("release MCP selftest failed")
	}
	return nil
}

func filepathBase(dir string) string {
	if dir == "" {
		return "."
	}
	for len(dir) > 1 && dir[len(dir)-1] == '/' {
		dir = dir[:len(dir)-1]
	}
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' {
			return dir[i+1:]
		}
	}
	return dir
}

package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/navbake3d"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// navmeshCommand bakes the navigation meshes of a scene's level geometry, one per agent type game.yaml declares, into the navigation mesh file the scene names, as navbake3d.Bake bakes them.
var navmeshCommand = &command{
	name:    "navmesh",
	args:    "<scene.yaml>",
	summary: "Bake a scene's glTF level geometry into a navigation mesh per agent type of game.yaml's navigation section, written to the file the scene's navMesh names",
	define:  defineNavmesh,
	complete: map[string]completion{
		"": filePaths,
	},
}

func defineNavmesh(a *app, fs *flag.FlagSet) func() error {
	inArg := inFlag(fs, "", "project directory, whose game.yaml holds the navigation settings and from which the scene's paths start (defaults to the nearest directory at or above the working directory holding game.yaml, nsgo.yaml or go.mod)")
	outArg := outFlag(fs, "", "navigation mesh `file` to write (defaults to the file the scene's navMesh names)")

	return func() error {
		if fs.NArg() != 1 {
			return usagef("expected one scene file, got %d arguments", fs.NArg())
		}
		dir, err := projectDir(*inArg)
		if err != nil {
			return err
		}
		scenePath := fs.Arg(0)
		if _, err := projectPath(dir, scenePath); err != nil {
			return err
		}
		fsys := os.DirFS(dir)
		settings, agents, err := navigationSettings(fsys)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Join(dir, project.FileName), err)
		}
		data, err := os.ReadFile(scenePath)
		if err != nil {
			return err
		}
		doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{File: scenePath})
		if err != nil {
			return err
		}
		resolved, err := sceneformat.Resolve(doc, sceneformat.FSScenes{FS: fsys})
		if err != nil {
			return fmt.Errorf("%s: %w", scenePath, err)
		}
		out := strings.TrimSpace(*outArg)
		if out == "" {
			if resolved.NavMesh == "" {
				return usagef("%s names no navMesh file; set navMesh in the scene or pass -out", scenePath)
			}
			out = filepath.Join(dir, filepath.FromSlash(resolved.NavMesh))
		}
		var g navbake3d.Geometry
		if err := g.AddSceneFile(resolved, fsys); err != nil {
			return fmt.Errorf("%s: %w", scenePath, err)
		}
		f, err := navbake3d.Bake(&g, settings, agents)
		if err != nil {
			return fmt.Errorf("%s: %w", scenePath, err)
		}
		yml, err := navbake3d.Encode(f)
		if err != nil {
			return err
		}
		if d := filepath.Dir(out); d != "." {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return err
			}
		}
		if err := os.WriteFile(out, yml, 0o644); err != nil {
			return err
		}
		parts := make([]string, len(f.Meshes))
		for i, m := range f.Meshes {
			parts[i] = fmt.Sprintf("%s %d polygons", m.Agent.Name, len(m.Polygons))
		}
		fmt.Fprintf(a.stdout, "%s: %s\n", out, strings.Join(parts, ", "))
		return nil
	}
}

// navigationSettings returns the bake settings and agent types of the game.yaml at the root of fsys, or the defaults and one agent type called project.DefaultNavAgent when there is no game.yaml or it has no navigation section.
func navigationSettings(fsys fs.FS) (navbake3d.Settings, []navbake3d.AgentType, error) {
	defaults := []navbake3d.AgentType{{Name: project.DefaultNavAgent}}
	f, err := project.ReadFS(fsys)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return navbake3d.Settings{}, defaults, nil
	case err != nil:
		return navbake3d.Settings{}, nil, err
	case f.Navigation == nil:
		return navbake3d.Settings{}, defaults, nil
	}
	n := f.Navigation
	s := navbake3d.Settings{
		CellSize:               n.CellSize,
		CellHeight:             n.CellHeight,
		MinRegionArea:          n.MinRegionArea,
		MaxEdgeLength:          n.MaxEdgeLength,
		MaxSimplificationError: n.MaxSimplificationError,
		MaxVertsPerPoly:        n.MaxVertsPerPoly,
		DetailSampleDistance:   n.DetailSampleDistance,
		DetailSampleMaxError:   n.DetailSampleMaxError,
	}
	if len(n.Agents) == 0 {
		return s, defaults, nil
	}
	agents := make([]navbake3d.AgentType, len(n.Agents))
	for i, a := range n.Agents {
		agents[i] = navbake3d.AgentType{Name: a.Name, Radius: a.Radius, Height: a.Height, MaxSlope: a.MaxSlope, MaxClimb: a.MaxClimb}
	}
	return s, agents, nil
}

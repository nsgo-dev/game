// Command clipinventory writes a deterministic Markdown inventory for rigged glTF models.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type document struct {
	Meshes []struct {
		Name       string `json:"name"`
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
		} `json:"primitives"`
	} `json:"meshes"`
	Skins []struct {
		Name   string `json:"name"`
		Joints []int  `json:"joints"`
	} `json:"skins"`
	Nodes []struct {
		Name string `json:"name"`
	} `json:"nodes"`
	Accessors []struct {
		Min []float64 `json:"min"`
		Max []float64 `json:"max"`
	} `json:"accessors"`
	Animations []struct {
		Name     string `json:"name"`
		Samplers []struct {
			Input int `json:"input"`
		} `json:"samplers"`
	} `json:"animations"`
}

type model struct {
	Path                 string
	Name                 string
	Meshes, Skins, Bones int
	Clips                []clip
}
type clip struct {
	Name     string
	Duration float64
}

func main() {
	var out string
	flag.StringVar(&out, "out", "README.md", "output Markdown file")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "clipinventory: pass one or more .gltf files")
		os.Exit(2)
	}
	models := make([]model, 0, flag.NArg())
	for _, path := range flag.Args() {
		m, err := readModel(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		models = append(models, m)
	}
	data := render(models)
	if err := os.WriteFile(out, []byte(data), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func render(models []model) string {
	models = append([]model(nil), models...)
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	var b strings.Builder
	b.WriteString("# CC0 Adventurers\n\n<!-- Generated from the models by tools/clipinventory. DO NOT EDIT. -->\n\nRegenerate from the repository root with `go run ./tools/clipinventory -out library/packs/cc0-adventurers/README.md library/packs/cc0-adventurers/mage.gltf library/packs/cc0-adventurers/knight.gltf`.\n\nBoth models are placeholder fixtures (one triangle skinned to one joint) released as CC0-1.0. They fix the twelve clip names a game can wire its animations to, so the KayKit Adventurers models (CC0) can replace them under the same names once skinned models draw in scenes (#1382).\n\n")
	for _, m := range models {
		fmt.Fprintf(&b, "## %s\n\n| Metric | Count |\n| --- | ---: |\n| Meshes | %d |\n| Skins | %d |\n| Bones | %d |\n\n| Clip | Duration (s) |\n| --- | ---: |\n", m.Name, m.Meshes, m.Skins, m.Bones)
		for _, c := range m.Clips {
			fmt.Fprintf(&b, "| `%s` | %.3f |\n", c.Name, c.Duration)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func readModel(path string) (model, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return model{}, fmt.Errorf("%s: %w", path, err)
	}
	var d document
	if err := json.Unmarshal(raw, &d); err != nil {
		return model{}, fmt.Errorf("%s: %w", path, err)
	}
	m := model{Path: path, Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Meshes: len(d.Meshes), Skins: len(d.Skins)}
	for _, s := range d.Skins {
		m.Bones += len(s.Joints)
	}
	for _, a := range d.Animations {
		if len(a.Samplers) == 0 {
			return model{}, fmt.Errorf("%s: animation %q has no samplers", path, a.Name)
		}
		// A clip lasts until the last key of its longest sampler; glTF requires min and max on every animation input accessor.
		var end float64
		for _, s := range a.Samplers {
			if s.Input < 0 || s.Input >= len(d.Accessors) {
				return model{}, fmt.Errorf("%s: animation %q has invalid input accessor %d", path, a.Name, s.Input)
			}
			acc := d.Accessors[s.Input]
			if len(acc.Min) == 0 || len(acc.Max) == 0 {
				return model{}, fmt.Errorf("%s: animation %q input accessor %d has no bounds", path, a.Name, s.Input)
			}
			end = max(end, acc.Max[0])
		}
		m.Clips = append(m.Clips, clip{Name: a.Name, Duration: end})
	}
	sort.Slice(m.Clips, func(i, j int) bool { return m.Clips[i].Name < m.Clips[j].Name })
	return m, nil
}

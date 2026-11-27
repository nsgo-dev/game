package assetcheck

import (
	"io/fs"
	"path"
	"strconv"

	"github.com/nsgo-dev/game/pkg/cutscene"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"go.yaml.in/yaml/v3"
)

type l10nIndex struct {
	tables int
	keys   map[string]bool
}

// checkCutscene checks the cutscene file at p: that cutscene.Parse accepts it, that its referenced video/audio files and scene file exist, that its actor and camera object IDs exist in the referenced scene, and that its dialogue line IDs exist in the project's localization string tables.
func (c *checker) checkCutscene(p string) {
	c.report.Cutscenes++
	data, ok := c.readFile(p)
	if !ok {
		return
	}
	doc, err := cutscene.Parse(data)
	if err != nil {
		c.yamlError(p, data, err)
		return
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		c.yamlError(p, data, err)
		return
	}
	lines := yamlLineCount(data)
	lineAt := func(steps ...string) int { return min(yamlLine(&root, steps), lines) }
	proj := c.projectRoot(path.Dir(p))

	if doc.EffectiveKind() == cutscene.KindVideo {
		if doc.Source != "" && !hasScheme(doc.Source) {
			c.fileRef(p, lineAt("source"), proj, doc.Source, "cutscene source")
		}
		return
	}

	var objIDs map[string]bool
	if doc.Scene != "" {
		if scenePath, ok := c.fileRef(p, lineAt("scene"), proj, doc.Scene, "cutscene scene"); ok {
			objIDs = c.loadSceneObjectIDs(scenePath, proj)
		}
	}

	var loc *l10nIndex
	for ti := range doc.Tracks {
		tr := &doc.Tracks[ti]
		tIdx := strconv.Itoa(ti)
		switch tr.Kind {
		case cutscene.TrackActor:
			if objIDs != nil && !objIDs[tr.Target] {
				c.problem(p, lineAt("tracks", tIdx, "target"), "cutscene actor target %q: scene %q has no object %q", tr.Target, doc.Scene, tr.Target)
			}
		case cutscene.TrackCamera:
			for ci := range tr.Clips {
				cl := &tr.Clips[ci]
				if cl.Target != "" && objIDs != nil && !objIDs[cl.Target] {
					c.problem(p, lineAt("tracks", tIdx, "clips", strconv.Itoa(ci), "target"), "cutscene camera target %q: scene %q has no object %q", cl.Target, doc.Scene, cl.Target)
				}
			}
		case cutscene.TrackDialogue:
			if loc == nil {
				loc = c.l10nForProject(proj)
			}
			for ci := range tr.Clips {
				cl := &tr.Clips[ci]
				if !loc.keys[cl.Line] {
					line := lineAt("tracks", tIdx, "clips", strconv.Itoa(ci), "line")
					if loc.tables == 0 {
						c.problem(p, line, "dialogue line %q: no localization tables found in project", cl.Line)
					} else {
						c.problem(p, line, "dialogue line %q: not found in localization tables", cl.Line)
					}
				}
			}
		case cutscene.TrackSound, cutscene.TrackMusic:
			for ci := range tr.Clips {
				cl := &tr.Clips[ci]
				if !hasScheme(cl.Source) {
					c.fileRef(p, lineAt("tracks", tIdx, "clips", strconv.Itoa(ci), "source"), proj, cl.Source, "cutscene audio")
				}
			}
		}
	}
}

// loadSceneObjectIDs returns the set of all object IDs in the scene at scenePath (resolving inherited and instanced scenes), or nil if the scene does not parse or resolve (which checkScene reports on the scene itself).
func (c *checker) loadSceneObjectIDs(scenePath, proj string) map[string]bool {
	if ids, ok := c.sceneObjs[scenePath]; ok {
		return ids
	}
	data, err := fs.ReadFile(c.fsys, scenePath)
	if err != nil {
		c.sceneObjs[scenePath] = nil
		return nil
	}
	doc, err := sceneformat.Parse(data)
	if err != nil {
		c.sceneObjs[scenePath] = nil
		return nil
	}
	if doc.RefersToScenes() {
		projectFS := c.fsys
		if proj != "." {
			sub, err := fs.Sub(c.fsys, proj)
			if err != nil {
				c.sceneObjs[scenePath] = nil
				return nil
			}
			projectFS = sub
		}
		resolved, err := sceneformat.Resolve(doc, &sceneFiles{fsys: projectFS})
		if err != nil {
			c.sceneObjs[scenePath] = nil
			return nil
		}
		doc = resolved
	}
	ids := make(map[string]bool)
	for li := range doc.Layers {
		sceneformat.WalkObjects(doc.Layers[li].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			ids[o.ID] = true
			return true
		})
	}
	c.sceneObjs[scenePath] = ids
	return ids
}

// l10nForProject discovers and caches all YAML localization string tables (l10n.ParseYAML) under project root proj.
func (c *checker) l10nForProject(proj string) *l10nIndex {
	if idx, ok := c.l10nByRoot[proj]; ok {
		return idx
	}
	idx := &l10nIndex{keys: make(map[string]bool)}
	c.l10nByRoot[proj] = idx
	for _, p := range c.otherYAMLs {
		if c.projectRoot(path.Dir(p)) != proj {
			continue
		}
		data, err := fs.ReadFile(c.fsys, p)
		if err != nil {
			continue
		}
		if !looksLikeL10nStringTable(data) {
			continue
		}
		tbl, err := l10n.ParseYAML(data)
		if err != nil {
			continue
		}
		idx.tables++
		for k := range tbl.Strings {
			idx.keys[k] = true
		}
		for k := range tbl.Plurals {
			idx.keys[k] = true
		}
	}
	return idx
}

func looksLikeL10nStringTable(data []byte) bool {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return false
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return false
	}
	hasVersion, hasLocale := false, false
	for i := 0; i+1 < len(m.Content); i += 2 {
		switch m.Content[i].Value {
		case "version":
			hasVersion = true
		case "locale":
			hasLocale = true
		}
	}
	return hasVersion && hasLocale
}

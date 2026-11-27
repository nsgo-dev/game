package assetcheck

import (
	"io/fs"
	"path"

	"github.com/nsgo-dev/game/pkg/project"
	"go.yaml.in/yaml/v3"
)

type soundProjectAudio struct {
	loaded bool
	has    bool
	groups map[string]struct{}
}

func (c *checker) projectAudio(root string) *soundProjectAudio {
	if c.audioByRoot == nil {
		c.audioByRoot = make(map[string]*soundProjectAudio)
	}
	if info, ok := c.audioByRoot[root]; ok {
		return info
	}
	info := &soundProjectAudio{loaded: true, groups: map[string]struct{}{}}
	c.audioByRoot[root] = info
	p := project.FileName
	if root != "." {
		p = path.Join(root, project.FileName)
	}
	data, err := fs.ReadFile(c.fsys, p)
	if err != nil {
		return info
	}
	f, err := project.Parse(data)
	if err != nil {
		// game.yaml parse errors are already reported by checkRefs.
		return info
	}
	info.has = true
	if f.Audio != nil {
		for _, g := range f.Audio.Concurrency {
			info.groups[g.Name] = struct{}{}
		}
	}
	return info
}

// checkSoundFile checks concurrency group references in *.sound.yaml / *.sound.yml files against
// the project's game.yaml audio.concurrency declarations.
func (c *checker) checkSoundFile(p string) {
	c.report.Sounds++
	data, err := fs.ReadFile(c.fsys, p)
	if err != nil {
		c.problem(p, 0, "%v", unwrapPathError(err))
		return
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		c.problem(p, 0, "sound: decode: %v", err)
		return
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return
	}
	var concNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Kind == yaml.ScalarNode && root.Content[i].Value == "concurrency" {
			concNode = root.Content[i+1]
			break
		}
	}
	if concNode == nil || concNode.Kind != yaml.ScalarNode || concNode.Value == "" {
		return
	}
	proj := c.projectAudio(c.projectRoot(path.Dir(p)))
	if !proj.has {
		return
	}
	if _, ok := proj.groups[concNode.Value]; !ok {
		c.problemAt(p, concNode.Line, concNode.Column, "sound concurrency group %q is not declared in %s audio.concurrency", concNode.Value, project.FileName)
	}
}

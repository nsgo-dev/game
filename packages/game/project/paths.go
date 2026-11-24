package project

import (
	"fmt"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// The fields of a project file that hold a path to a file or folder of the project, in PathRef.Field and in messages.
const (
	FieldAutoloadScene    = "autoload scene"
	FieldAutoloadResource = "autoload resource"
	FieldAssetsCache      = "assets cache"
	FieldAtlasIn          = "assets atlas in"
	FieldAtlasOut         = "assets atlas out"
	FieldTextureIn        = "assets texture in"
	FieldTextureOut       = "assets texture out"
	FieldAudioIn          = "assets audio in"
	FieldAudioOut         = "assets audio out"
	FieldFontIn           = "assets font in"
	FieldFontOut          = "assets font out"
	FieldModelIn          = "assets model in"
	FieldModelOut         = "assets model out"
	FieldManifestIn       = "assets manifest in"
	FieldManifestOut      = "assets manifest out"
)

// PathRef is one path a project file holds to a file or folder of the project, slash-separated and relative to the project root.
type PathRef struct {
	// Field says what the path is, one of the Field constants, such as FieldAtlasIn.
	Field string
	// Node is the YAML scalar that writes the path in the document PathRefs parsed: its Value is the path, and its Line and Column say where it is written.
	Node *yaml.Node
	// Dir reports whether the path names a folder rather than a file.
	Dir bool
	// Output reports whether the path need not exist before game assets runs: what a tool writes, such as an asset target's out, and the manifest's in, the folder the other targets usually write into.
	Output bool
}

// pathField is one path field of a File: what it is, the YAML keys and sequence indexes that lead to it from the document's root, and the field itself.
type pathField struct {
	field       string
	dir, output bool
	keys        []string
	value       *string
}

// pathFields returns every non-empty path f holds to a file or folder of the project, in the order Encode writes them: the autoloaded scenes and resources, then the assets section's cache and each target's in and out. export.output is the directory builds are written to, which nothing in the project reads, and is not one of them. It is the one list of game.yaml's path fields, which PathRefs and EachPath both follow.
func pathFields(f *File) []pathField {
	var out []pathField
	add := func(field string, dir, output bool, value *string, keys ...string) {
		if *value != "" {
			out = append(out, pathField{field: field, dir: dir, output: output, keys: keys, value: value})
		}
	}
	if a := f.Autoload; a != nil {
		for i := range a.Scenes {
			add(FieldAutoloadScene, false, false, &a.Scenes[i].Path, "autoload", "scenes", strconv.Itoa(i), "path")
		}
		for i := range a.Resources {
			add(FieldAutoloadResource, false, false, &a.Resources[i].Path, "autoload", "resources", strconv.Itoa(i), "path")
		}
	}
	if a := f.Assets; a != nil {
		add(FieldAssetsCache, false, true, &a.Cache, "assets", "cache")
		for i := range a.Atlases {
			add(FieldAtlasIn, true, false, &a.Atlases[i].In, "assets", "atlases", strconv.Itoa(i), "in")
			add(FieldAtlasOut, true, true, &a.Atlases[i].Out, "assets", "atlases", strconv.Itoa(i), "out")
		}
		for i := range a.Textures {
			add(FieldTextureIn, false, false, &a.Textures[i].In, "assets", "textures", strconv.Itoa(i), "in")
			add(FieldTextureOut, false, true, &a.Textures[i].Out, "assets", "textures", strconv.Itoa(i), "out")
		}
		for i := range a.Audio {
			add(FieldAudioIn, false, false, &a.Audio[i].In, "assets", "audio", strconv.Itoa(i), "in")
			add(FieldAudioOut, false, true, &a.Audio[i].Out, "assets", "audio", strconv.Itoa(i), "out")
		}
		for i := range a.Fonts {
			add(FieldFontIn, false, false, &a.Fonts[i].In, "assets", "fonts", strconv.Itoa(i), "in")
			add(FieldFontOut, false, true, &a.Fonts[i].Out, "assets", "fonts", strconv.Itoa(i), "out")
		}
		for i := range a.Models {
			add(FieldModelIn, false, false, &a.Models[i].In, "assets", "models", strconv.Itoa(i), "in")
			add(FieldModelOut, false, true, &a.Models[i].Out, "assets", "models", strconv.Itoa(i), "out")
		}
		if m := a.Manifest; m != nil {
			add(FieldManifestIn, true, true, &m.In, "assets", "manifest", "in")
			add(FieldManifestOut, false, true, &m.Out, "assets", "manifest", "out")
		}
	}
	return out
}

// EachPath calls visit with every non-empty path f holds to a file or folder of the project, as PathRefs lists them, so that visit can read or change it in place, as a tool that moves a file does.
func (f *File) EachPath(visit func(field string, path *string)) {
	for _, pf := range pathFields(f) {
		visit(pf.field, pf.value)
	}
}

// PathRefs parses data as a project file, refusing what Parse refuses, and returns every path it holds to a file or folder of the project with the YAML node that writes it, in the order EachPath visits them, so that a tool can check each path or rewrite it in place.
func PathRefs(data []byte) ([]PathRef, error) {
	f, err := Parse(data)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("project: decode: %w", err) // Parse read it, so this does not happen.
	}
	var out []PathRef
	for _, pf := range pathFields(f) {
		n := lookupNode(&doc, pf.keys)
		if n == nil || n.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("project: %s %q is not written as a YAML scalar", pf.field, *pf.value)
		}
		out = append(out, PathRef{Field: pf.field, Node: n, Dir: pf.dir, Output: pf.output})
	}
	return out, nil
}

// maxAliases bounds how many aliases lookupNode follows in a row, so a cycle cannot loop.
const maxAliases = 64

// lookupNode returns the node keys lead to from n, a document or a node in it, following aliases: a key steps into a mapping and a decimal index into a sequence. It returns nil when there is no such node.
func lookupNode(n *yaml.Node, keys []string) *yaml.Node {
	deref := func(n *yaml.Node) *yaml.Node {
		for range maxAliases {
			if n == nil || n.Kind != yaml.AliasNode {
				return n
			}
			n = n.Alias
		}
		return nil
	}
	n = deref(n)
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = deref(n.Content[0])
	}
	for _, k := range keys {
		switch {
		case n == nil:
			return nil
		case n.Kind == yaml.MappingNode:
			var next *yaml.Node
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == k {
					next = n.Content[i+1]
				}
			}
			n = deref(next)
		case n.Kind == yaml.SequenceNode:
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(n.Content) {
				return nil
			}
			n = deref(n.Content[i])
		default:
			return nil
		}
	}
	return n
}

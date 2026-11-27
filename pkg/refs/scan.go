package refs

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/resource"
	"go.yaml.in/yaml/v3"
)

// FileKind is what Scan read a file as.
type FileKind string

const (
	// KindProject is a game.yaml project file.
	KindProject FileKind = "project"
	// KindScene is a sceneformat scene document.
	KindScene FileKind = "scene"
	// KindPrefab is a prefab YAML document (*.prefab.yaml or *.prefab.yml).
	KindPrefab FileKind = "prefab"
	// KindDataTable is a YAML data table whose rows reference project files.
	KindDataTable FileKind = "data_table"
	// KindScreen is a gameui *.screen.yaml HUD or menu screen document.
	KindScreen FileKind = "screen"
	// KindStringTable is a versioned YAML localization string table (l10n.StringTable).
	KindStringTable FileKind = "string_table"
	// KindResource is a resource file.
	KindResource FileKind = "resource"
	// KindTiled is a Tiled map (.tmj) or tileset (.tsj).
	KindTiled FileKind = "tiled"
	// KindLDtk is an LDtk project (.ldtk).
	KindLDtk FileKind = "ldtk"
	// KindTileset is a YAML tileset (*.tileset.yaml, tilemap.TilesetDoc).
	KindTileset FileKind = "tileset"
	// KindTerrain is a YAML 3D terrain definition (*.terrain.yaml, world3d.TerrainDoc).
	KindTerrain FileKind = "terrain"
	// KindAtlas is a sprite atlas: a JSON file with top-level frames and meta objects, as TexturePacker and Aseprite write it.
	KindAtlas FileKind = "atlas"
	// KindGLTF is a glTF 2.0 model in JSON form (.gltf), whose buffers and images may be separate files.
	KindGLTF FileKind = "gltf"
	// KindCutscene is a cutscene file (*.cutscene.yaml or *.cutscene.yml).
	KindCutscene FileKind = "cutscene"
	// KindGridMap is a world3d GridMap YAML file (*.gridmap.yaml or *.gridmap.yml), which references a glTF mesh library.
	KindGridMap FileKind = "gridmap"
	// KindStates is a *.states.yaml sprite animation state machine file.
	KindStates FileKind = "states"
)

// Fields name what a reference is, in Ref.Field and in messages. A reference in game.yaml has the Field project.PathRefs gives it, such as project.FieldAtlasIn; the two autoload ones are repeated here.
const (
	FieldAutoloadScene    = project.FieldAutoloadScene
	FieldAutoloadResource = project.FieldAutoloadResource
	FieldInherits         = "inherits"
	FieldNavMesh          = "navigation mesh"
	FieldScene            = "scene"
	FieldPrefab           = "prefab"
	FieldDataTable        = "data table"
	FieldMaterial         = "material"
	FieldModel            = "model"
	FieldGridMap          = "grid map"
	FieldMeshLibrary      = "mesh library"
	FieldStates           = "states"
	FieldClip             = "clip"
	FieldParticles        = "particles"
	FieldSkyHDR           = "sky hdr"
	FieldReflectionProbe  = "reflection probe cubemap"
	FieldLightProbeGrid   = "light probe grid"
	FieldTilemapSource    = "tilemap source"
	FieldTilesetFile      = "tileset file"
	FieldTexture          = "texture"
	FieldExternalTileset  = "external tileset"
	FieldTilesetImage     = "tileset image"
	FieldTileImage        = "tile image"
	FieldImageLayer       = "image layer"
	FieldLevelBackground  = "level background"
	FieldExternalLevel    = "external level"
	FieldAtlasImage       = "atlas image"
	FieldModelBuffer      = "model buffer"
	FieldModelImage       = "model image"
	FieldCutsceneSource   = "cutscene source"
	FieldCutsceneAudio    = "cutscene audio"
	// FieldSkeletalRig is a .skel.yaml rig path referenced by an object's "skeletal" component.
	FieldSkeletalRig = "skeletal rig"
	// FieldScreenSprite is a sprite image or atlas path referenced by a *.screen.yaml node.
	FieldScreenSprite = "sprite"
	// FieldTextKey is a localization text key referenced by a *.screen.yaml node.
	FieldTextKey = "text key"
	// FieldStringKey is a localization string entry key in a YAML string table.
	FieldStringKey = "string key"
	// FieldPluralKey is a localization plural entry key in a YAML string table.
	FieldPluralKey        = "plural key"
	FieldTerrainHeightmap = "terrain heightmap"
	FieldTerrainSplatMap  = "terrain splat map"
	FieldTerrainWeightMap = "terrain weight map"
	FieldTerrainTexture   = "terrain texture"
	FieldTerrainMesh      = "terrain foliage mesh"
	FieldTerrainDensity   = "terrain density map"
)

// TextKeyRef is one occurrence of a localization text key in a project file: either a screen node's textKey reference or a string table's key definition.
type TextKeyRef struct {
	// File is the slash-separated path of the file that holds the key.
	File string
	// Line and Column are where the key is written in File (both 1-based).
	Line, Column int
	// Field says what the occurrence is: FieldTextKey, FieldStringKey, or FieldPluralKey.
	Field string
	// Key is the localization key string (for example "hud.hint.lever").
	Key string
	// NodeID is the screen node ID when the occurrence is in a screen file, or empty in a string table.
	NodeID string

	span span
}

// Ref is one reference from a file to another.
type Ref struct {
	// File is the slash-separated path of the file that holds the reference.
	File string
	// Line and Column are where its value is written in File, both counting from 1 and Column in characters.
	Line, Column int
	// Field says what the reference is, one of the Field constants, such as FieldMaterial, or in game.yaml one of package project's.
	Field string
	// Value is the path as File writes it.
	Value string
	// Target is the slash-separated path of the file Value names, or "" when Value cannot name a file in the scanned directory: an absolute path, a URI, a path with backslashes or one leading outside it.
	Target string
	// FromFile reports whether Value is relative to File's directory, as Tiled and LDtk write paths, rather than to the project root.
	FromFile bool
	// Dir reports whether Value names a folder rather than a file, as an assets target's in may in game.yaml.
	Dir bool
	// Output reports whether Value names what a tool writes, such as an assets target's out in game.yaml, which need not exist yet.
	Output bool

	span span
	// uri reports whether Value is a percent-encoded URI reference, as glTF writes its buffers' and images' uri, so a rewrite encodes the new path the same way.
	uri bool
}

// File is one file Scan read references or a uid from.
type File struct {
	// Path is the file's slash-separated path.
	Path string
	// Kind is what the file was read as.
	Kind FileKind
	// UID is the stable ID in the header of a scene or resource file, or "".
	UID string
	// Root is the project root the file's root-relative paths start from, "." for the scanned directory itself.
	Root string

	data   []byte
	starts []int
	// uid is the node of the uid value, also when it is empty or null, and uidKey its key; doc is the root mapping of a YAML file.
	uid, uidKey, doc *yaml.Node
}

// Problem is one thing wrong that Scan found or Project.Check reports.
type Problem struct {
	// File is the slash-separated path of the file at fault.
	File string
	// Line is the 1-based line that holds the problem, or 0 when it is about the whole file.
	Line int
	// Message says what is wrong.
	Message string
}

// String formats p as "file:line: message", or "file: message" without a line.
func (p Problem) String() string {
	if p.Line > 0 {
		return p.File + ":" + strconv.Itoa(p.Line) + ": " + p.Message
	}
	return p.File + ": " + p.Message
}

// Project is what Scan found in a directory.
type Project struct {
	// Files are the files read, in path order.
	Files []*File
	// Refs are the references found, in file order and then in the order each file writes them.
	Refs []Ref
	// TextKeys are the localization text key references and definitions found in *.screen.yaml files and YAML string tables, in file order and then document order.
	TextKeys []TextKeyRef
	// Problems are what makes the references unsafe to follow or rewrite: a game.yaml or an index that does not parse, and a uid two files of one project share. A reference to a file that is gone is not one of them; Check reports those.
	Problems []Problem

	fsys fs.FS
	// paths are every file in the scanned directory, in path order, and exists marks them.
	paths  []string
	exists map[string]bool
	// dirs marks the directories in the scanned directory.
	dirs  map[string]bool
	files map[string]*File
	// roots caches the project root of each directory.
	roots map[string]string
	// indexes holds the parsed index of each project root that has a valid one.
	indexes map[string]*Index
	// uids lists the files holding each uid, in path order.
	uids map[string][]string
	// indexProblems are the Problems about game.yaml and index files, which game index cannot fix.
	indexProblems []Problem
	// unread are YAML files that should have been read for references but do not parse.
	unread []string
	// hashes caches the SHA-256 of files read for Moved.
	hashes map[string]string
	// pending are the YAML files Scan did not read as any kind of file, and pendingUIDs the files among them holding each uid, read when Moved first needs them.
	pending     []string
	pendingUIDs map[string][]string
	// packs records installed library packs declared in scanned game.yaml files so PlanMove can refuse moving individual tracked files out of a pack folder.
	packs []scannedPack
	// modEntries marks files named as a mod's entry script in a mod.yaml manifest, which are never classified as assets.
	modEntries map[string]bool
	// xmlTSX marks .tsx files that parsed as Tiled XML (<tileset>).
	xmlTSX map[string]bool
	// mapTSX marks .tsx files referenced as external tilesets from a Tiled map (.tmj or .tmx) or scene tilesetFile.
	mapTSX map[string]bool
}

// modManifestFileName is the filename of a script mod manifest (modding.ManifestFile).
const modManifestFileName = "mod.yaml"

// scannedPack holds the scanned paths of one installed pack from a game.yaml file.
type scannedPack struct {
	file  string
	name  string
	dir   string
	files []string
}

// Scan walks fsys, such as os.DirFS of a game's directory, and reads every file the package comment lists for its references, uids and project indexes. Directories whose names start with "." or "_", and those named testdata, are skipped, as the go command skips them. A file that does not parse is skipped; game check reports it. Scan returns an error only when fsys's root cannot be read.
func Scan(fsys fs.FS) (*Project, error) {
	p := &Project{
		fsys:       fsys,
		exists:     make(map[string]bool),
		dirs:       make(map[string]bool),
		files:      make(map[string]*File),
		roots:      make(map[string]string),
		indexes:    make(map[string]*Index),
		uids:       make(map[string][]string),
		hashes:     make(map[string]string),
		modEntries: make(map[string]bool),
		xmlTSX:     make(map[string]bool),
		mapTSX:     make(map[string]bool),
	}
	var yamls, jsons, others, indexes, tmxs, tsxs []string
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			if name == "." {
				return err
			}
			return nil
		}
		if d.IsDir() {
			if name != "." && skipDir(d.Name()) {
				return fs.SkipDir
			}
			p.dirs[name] = true
			return nil
		}
		p.paths = append(p.paths, name)
		p.exists[name] = true
		lower := strings.ToLower(d.Name())
		switch {
		case d.Name() == IndexFileName:
			indexes = append(indexes, name)
		case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
			yamls = append(yamls, name)
		case strings.HasSuffix(lower, ".tmj"), strings.HasSuffix(lower, ".tsj"), strings.HasSuffix(lower, ".ldtk"), strings.HasSuffix(lower, ".gltf"):
			jsons = append(jsons, name)
		case strings.HasSuffix(lower, ".json"):
			others = append(others, name)
		case strings.HasSuffix(lower, ".tmx"):
			tmxs = append(tmxs, name)
		case strings.HasSuffix(lower, ".tsx"):
			tsxs = append(tsxs, name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("refs: %w", err)
	}
	for _, name := range indexes {
		p.readIndex(name)
	}
	var pending []string
	for _, name := range yamls {
		if !p.readYAML(name, false) {
			pending = append(pending, name)
		}
	}
	for _, name := range jsons {
		p.readJSON(name, strings.ToLower(path.Ext(name)))
	}
	// A .json file a Tiled map names as an external tileset is a Tiled tileset, as older Tiled versions named them; any other with top-level frames and meta objects is a sprite atlas.
	tilesets := make(map[string]bool)
	for _, r := range p.Refs {
		if r.Field == FieldExternalTileset && strings.EqualFold(path.Ext(r.Target), ".json") {
			tilesets[r.Target] = true
		}
	}
	for _, name := range others {
		switch {
		case tilesets[name]:
			p.readJSON(name, ".tsj")
		case isAtlas(p.fsys, name):
			p.readJSON(name, formatAtlas)
		}
	}
	// A YAML file another file names as a scene or grid map is read as such, whatever its name; reading it may name more.
	for changed := true; changed; {
		changed = false
		for _, r := range p.Refs {
			if (r.Field == FieldScene || r.Field == FieldInherits || r.Field == FieldAutoloadScene) && slices.Contains(pending, r.Target) {
				pending = slices.DeleteFunc(pending, func(s string) bool { return s == r.Target })
				p.readYAML(r.Target, true)
				changed = true
				break
			}
			if r.Field == FieldGridMap && slices.Contains(pending, r.Target) {
				pending = slices.DeleteFunc(pending, func(s string) bool { return s == r.Target })
				p.readGridMapYAML(r.Target)
				changed = true
				break
			}
		}
	}
	// Classify .tsx files before scanning data tables so Project.IsAsset distinguishes Tiled XML tilesets from TSX mod scripts.
	for _, name := range tmxs {
		p.readTMXTilesets(name)
	}
	for _, name := range tsxs {
		if data, err := fs.ReadFile(p.fsys, name); err == nil && isTiledTSX(data) {
			p.xmlTSX[name] = true
		}
	}
	for _, r := range p.Refs {
		if (r.Field == FieldExternalTileset || r.Field == FieldTilesetFile) && r.Target != "" && strings.EqualFold(path.Ext(r.Target), ".tsx") {
			p.mapTSX[r.Target] = true
		}
	}
	var stillPending []string
	for _, name := range pending {
		if !p.readDataTableYAML(name) {
			stillPending = append(stillPending, name)
		}
	}
	p.pending = stillPending
	slices.SortFunc(p.Files, func(a, b *File) int { return cmp.Compare(a.Path, b.Path) })
	slices.SortStableFunc(p.Refs, func(a, b Ref) int { return cmp.Compare(a.File, b.File) })
	slices.SortStableFunc(p.TextKeys, func(a, b TextKeyRef) int { return cmp.Compare(a.File, b.File) })
	for _, f := range p.Files {
		if f.UID == "" {
			continue
		}
		holders := p.holders(f.UID, f.Root)
		if len(holders) > 1 && holders[0] != f.Path {
			p.problem(f.Path, f.uid.Line, "uid %q is also the uid of %s: one of them is a copy, which game index gives a new uid", f.UID, holders[0])
		}
	}
	return p, nil
}

// holders returns the files of the project at root that hold the uid id, in path order. Files of different projects may share a uid, as two games made from one template do, since each project's index is its own.
func (p *Project) holders(id, root string) []string {
	var out []string
	for _, h := range p.uids[id] {
		if p.files[h].Root == root {
			out = append(out, h)
		}
	}
	return out
}

// skipDir reports whether the walk skips a directory called name, as the go command skips it.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata"
}

func (p *Project) problem(file string, line int, format string, args ...any) {
	p.Problems = append(p.Problems, Problem{File: file, Line: line, Message: fmt.Sprintf(format, args...)})
}

// Exists reports whether the scanned directory held a file at name when Scan walked it.
func (p *Project) Exists(name string) bool { return p.exists[name] }

// Root returns the project root of the file at name: the nearest directory above it that holds game.yaml, nsgo.yaml or go.mod, or "." when none does.
func (p *Project) Root(name string) string { return p.rootOf(path.Dir(name)) }

func (p *Project) rootOf(dir string) string {
	if r, ok := p.roots[dir]; ok {
		return r
	}
	root := "."
	if dir != "." {
		if p.exists[path.Join(dir, project.FileName)] || p.exists[path.Join(dir, project.NSGoFileName)] || p.exists[path.Join(dir, "go.mod")] {
			root = dir
		} else {
			root = p.rootOf(path.Dir(dir))
		}
	}
	p.roots[dir] = root
	return root
}

// Index returns the parsed index at the project root root, or nil when it has none or it does not parse.
func (p *Project) Index(root string) *Index { return p.indexes[root] }

func (p *Project) readIndex(name string) {
	data, err := fs.ReadFile(p.fsys, name)
	if err != nil {
		p.problem(name, 0, "%v", unwrapPathError(err))
		p.indexProblems = append(p.indexProblems, p.Problems[len(p.Problems)-1])
		return
	}
	ix, err := ParseIndex(data)
	if err != nil {
		msgs := []string{err.Error()}
		if te, ok := errors.AsType[*yaml.TypeError](err); ok {
			msgs = te.Errors
		}
		for _, m := range msgs {
			line, msg := splitYAMLLine(m)
			p.problem(name, line, "%s", msg)
			p.indexProblems = append(p.indexProblems, p.Problems[len(p.Problems)-1])
		}
		return
	}
	p.indexes[path.Dir(name)] = ix
}

// yamlLineRE finds the "line N: " that yaml's errors carry.
var yamlLineRE = regexp.MustCompile(`line (\d+): `)

// splitYAMLLine takes the first "line N: " out of a yaml message and returns N and the rest, or 0 and msg when it names no line.
func splitYAMLLine(msg string) (int, string) {
	m := yamlLineRE.FindStringSubmatchIndex(msg)
	if m == nil {
		return 0, msg
	}
	n, err := strconv.Atoi(msg[m[2]:m[3]])
	if err != nil {
		return 0, msg
	}
	return n, msg[:m[0]] + msg[m[1]:]
}

func unwrapPathError(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	return err
}

// readYAML reads the YAML file at name as game.yaml, a resource file, or a scene when its name says so or asScene is set. It reports whether the file was read as one of them; a file that is none of them, or does not parse, is left for a later reference to name as a scene.
func (p *Project) readYAML(name string, asScene bool) bool {
	data, err := fs.ReadFile(p.fsys, name)
	if err != nil {
		return false
	}
	if path.Base(name) == project.FileName {
		p.readProject(name, data)
		return true
	}
	if path.Base(name) == modManifestFileName {
		p.readModManifest(name, data)
		return true
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		if asScene || isSceneName(name) || isPrefabName(name) || isCutsceneName(name) || isScreenName(name) || isGridMapName(name) || isTilesetName(name) {
			p.unread = append(p.unread, name)
		}
		return false
	}
	doc := root.Content[0]
	f := &File{Path: name, data: data, starts: lineStarts(data), doc: doc, Root: p.Root(name)}
	switch {
	case isResourceKind(field(doc, "type")):
		f.Kind = KindResource
		p.add(f)
		if field(doc, "type").Value == string(resource.KindMaterial) {
			p.ref(f, field(doc, "texture"), FieldTexture)
		}
		return true
	case isCutsceneName(name):
		f.Kind = KindCutscene
		p.add(f)
		p.cutsceneRefs(f, doc)
		return true
	case isScreenName(name):
		f.Kind = KindScreen
		p.add(f)
		p.screenRefs(f, field(doc, "nodes"), 0)
		return true
	case isStatesName(name):
		f.Kind = KindStates
		p.add(f)
		for _, st := range items(field(doc, "states")) {
			p.ref(f, field(st, "clip"), FieldClip)
		}
		return true
	case isPrefabName(name):
		f.Kind = KindPrefab
		p.add(f)
		p.prefabRefs(f, doc)
		return true
	case asScene || isSceneName(name):
		f.Kind = KindScene
		p.add(f)
		p.sceneRefs(f, doc)
		return true
	case isTilesetName(name):
		f.Kind = KindTileset
		p.add(f)
		p.ref(f, field(doc, "image"), FieldTilesetImage)
		return true
	case isStringTableDoc(doc):
		f.Kind = KindStringTable
		p.add(f)
		p.stringTableKeys(f, doc)
		return true
	case isGridMapName(name):
		f.Kind = KindGridMap
		p.add(f)
		p.ref(f, field(doc, "meshLibrary"), FieldMeshLibrary)
		return true
	case isTerrainName(name):
		f.Kind = KindTerrain
		p.add(f)
		p.terrainRefs(f, doc)
		return true
	}
	return false
}

// readGridMapYAML reads the YAML file at name as a world3d GridMap document referenced by a scene.
func (p *Project) readGridMapYAML(name string) {
	data, err := fs.ReadFile(p.fsys, name)
	if err != nil {
		return
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		p.unread = append(p.unread, name)
		return
	}
	doc := root.Content[0]
	f := &File{Path: name, Kind: KindGridMap, data: data, starts: lineStarts(data), doc: doc, Root: p.Root(name)}
	p.add(f)
	p.ref(f, field(doc, "meshLibrary"), FieldMeshLibrary)
}

// readProject reads the paths of the game.yaml at name through package project, which owns its schema. A game.yaml that project.Parse refuses is a problem, and its paths are neither checked nor rewritten until it is fixed.
func (p *Project) readProject(name string, data []byte) {
	f := &File{Path: name, Kind: KindProject, data: data, starts: lineStarts(data), Root: p.Root(name)}
	p.add(f)
	paths, err := project.PathRefs(data)
	if err != nil {
		line, msg := splitYAMLLine(err.Error())
		p.problem(name, line, "%s", msg)
		p.indexProblems = append(p.indexProblems, p.Problems[len(p.Problems)-1])
		return
	}
	hasPack := false
	for _, pr := range paths {
		if pr.Field == project.FieldPackDir {
			hasPack = true
		}
		if r := p.ref(f, pr.Node, pr.Field); r != nil {
			r.Dir, r.Output = pr.Dir, pr.Output
		}
	}
	if hasPack {
		if proj, err := project.Parse(data); err == nil {
			for _, pk := range proj.Packs {
				dir := path.Join(f.Root, pk.Dir)
				files := make([]string, len(pk.Files))
				for i, rel := range pk.Files {
					files[i] = path.Join(dir, rel)
				}
				p.packs = append(p.packs, scannedPack{file: name, name: pk.Name, dir: dir, files: files})
			}
		}
	}
}

// isSceneName reports whether name is a scene file by its name, *.scene.yaml or *.scene.yml.
func isSceneName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".scene.yaml") || strings.HasSuffix(lower, ".scene.yml")
}

// isPrefabName reports whether name is a prefab file by its name, *.prefab.yaml or *.prefab.yml.
func isPrefabName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".prefab.yaml") || strings.HasSuffix(lower, ".prefab.yml")
}

// isPrefabFileRef reports whether n holds a path-valued prefab reference (such as "prefabs/slime.prefab.yaml") rather than a bare prefab name looked up in an AssetResolver.
func (p *Project) isPrefabFileRef(root string, n *yaml.Node) bool {
	if n == nil || n.Kind != yaml.ScalarNode || n.Value == "" || (n.Tag != "" && n.Tag != "!!str") {
		return false
	}
	if !validPath(n.Value) {
		return false
	}
	lower := strings.ToLower(n.Value)
	if strings.ContainsRune(n.Value, '/') || strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		return true
	}
	return p.exists[path.Join(root, n.Value)]
}

// prefabRefs records the references of a *.prefab.yaml document: the prefab it inherits, any scene/prefab/material/model/gridMap/particle/animator/skeletal references, and nested layers if the prefab is scene-shaped.
func (p *Project) prefabRefs(f *File, doc *yaml.Node) {
	p.ref(f, field(doc, "inherits"), FieldInherits)
	p.ref(f, field(doc, "scene"), FieldScene)
	if pf := field(doc, "prefab"); p.isPrefabFileRef(f.Root, pf) {
		p.ref(f, pf, FieldPrefab)
	}
	p.ref(f, field(doc, "material"), FieldMaterial)
	p.ref(f, field(doc, "model"), FieldModel)
	p.ref(f, field(doc, "gridMap"), FieldGridMap)
	p.animatorRefs(f, field(doc, "components"))
	p.ref(f, field(field(doc, "particles"), "file"), FieldParticles)
	p.skeletalComponentRef(f, doc)
	for _, l := range items(field(doc, "layers")) {
		p.tilemapRefs(f, field(l, "tilemap"))
		p.objectRefs(f, field(l, "objects"), 0)
	}
}

// readDataTableYAML inspects a pending YAML file as a data table (either a canonical version/rows document or a mapping holding sequences of row mappings) and records any project file path references found in row cells. It returns true when the file is recorded as KindDataTable.
func (p *Project) readDataTableYAML(name string) bool {
	data, err := fs.ReadFile(p.fsys, name)
	if err != nil {
		return false
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return false
	}
	doc := root.Content[0]
	f := &File{Path: name, Kind: KindDataTable, data: data, starts: lineStarts(data), doc: doc, Root: p.Root(name)}
	var found []Ref
	for _, kv := range pairs(doc) {
		seq := kv[1]
		if seq == nil || seq.Kind != yaml.SequenceNode {
			continue
		}
		for _, row := range items(seq) {
			if row == nil || row.Kind != yaml.MappingNode {
				continue
			}
			for _, cell := range pairs(row) {
				p.collectDataTableCellRefs(f, cell[1], &found)
			}
		}
	}
	if len(found) == 0 {
		return false
	}
	p.add(f)
	p.Refs = append(p.Refs, found...)
	return true
}

// readModManifest records the entry script named by a mod.yaml manifest so Project.IsAsset never classifies it as a tileset asset.
func (p *Project) readModManifest(name string, data []byte) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return
	}
	entry := field(root.Content[0], "entry")
	if entry == nil || entry.Kind != yaml.ScalarNode || entry.Value == "" || (entry.Tag != "" && entry.Tag != "!!str") {
		return
	}
	entryVal := strings.TrimPrefix(entry.Value, "./")
	if !validPath(entryVal) || hasVolume(entryVal) || hasScheme(entryVal) {
		return
	}
	p.modEntries[path.Join(path.Dir(name), entryVal)] = true
}

// readTMXTilesets records .tsx external tileset paths referenced by a Tiled XML map (.tmx) so Project.IsAsset classifies them as tilesets.
func (p *Project) readTMXTilesets(name string) {
	data, err := fs.ReadFile(p.fsys, name)
	if err != nil {
		return
	}
	for _, src := range parseTMXTilesetSources(data) {
		if target, ok := resolveFrom(path.Dir(name), src); ok && strings.EqualFold(path.Ext(target), ".tsx") {
			p.mapTSX[target] = true
		}
	}
}

func (p *Project) collectDataTableCellRefs(f *File, n *yaml.Node, out *[]Ref) {
	n = deref(n)
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "" || (n.Tag != "" && n.Tag != "!!str") || !validPath(n.Value) || hasVolume(n.Value) || hasScheme(n.Value) {
			return
		}
		target := path.Join(f.Root, n.Value)
		_, indexed := p.indexes[f.Root].Lookup(strings.TrimPrefix(target, rootPrefix(f.Root)))
		if !p.exists[target] && !indexed && !isKnownProjectYAMLRef(n.Value) && !p.IsAsset(target) {
			return
		}
		r := Ref{File: f.Path, Line: n.Line, Column: n.Column, Field: FieldDataTable, Value: n.Value, Target: target}
		if s, err := yamlSpan(f.data, f.starts, n); err == nil {
			r.span = s
		} else {
			r.span = span{start: -1}
		}
		*out = append(*out, r)
	case yaml.SequenceNode:
		for _, elem := range items(n) {
			if elem != nil && elem.Kind == yaml.ScalarNode {
				p.collectDataTableCellRefs(f, elem, out)
			}
		}
	}
}

func isKnownProjectYAMLRef(v string) bool {
	lower := strings.ToLower(v)
	for _, suf := range []string{
		".prefab.yaml", ".prefab.yml",
		".scene.yaml", ".scene.yml",
		".material.yaml", ".material.yml",
		".tileset.yaml", ".tileset.yml",
		".terrain.yaml", ".terrain.yml",
		".cutscene.yaml", ".cutscene.yml",
		".screen.yaml", ".screen.yml",
		".states.yaml", ".states.yml",
		".gridmap.yaml", ".gridmap.yml",
		".particles.yaml", ".particles.yml",
		".skel.yaml", ".skel.yml",
	} {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return false
}

// isCutsceneName reports whether name is a cutscene file by its name, *.cutscene.yaml or *.cutscene.yml.
func isCutsceneName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".cutscene.yaml") || strings.HasSuffix(lower, ".cutscene.yml")
}

// cutsceneRefs records the scene, video source and timeline audio references of a cutscene document. URI sources such as asset: or https: name no project-relative file and are skipped.
func (p *Project) cutsceneRefs(f *File, doc *yaml.Node) {
	p.ref(f, field(doc, "scene"), FieldScene)
	if src := field(doc, "source"); src != nil && !hasScheme(src.Value) {
		p.ref(f, src, FieldCutsceneSource)
	}
	for _, tr := range items(field(doc, "tracks")) {
		kind := field(tr, "kind")
		if kind == nil || (kind.Value != "sound" && kind.Value != "music") {
			continue
		}
		for _, cl := range items(field(tr, "clips")) {
			if src := field(cl, "source"); src != nil && !hasScheme(src.Value) {
				p.ref(f, src, FieldCutsceneAudio)
			}
		}
	}
}

// isScreenName reports whether name is a gameui screen file by its name, *.screen.yaml or *.screen.yml.
func isScreenName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".screen.yaml") || strings.HasSuffix(lower, ".screen.yml")
}

// isStringTableDoc reports whether doc is a versioned YAML localization string table (l10n.StringTable):
// a mapping with scalar version and locale keys and only keys from {version, locale, strings, plurals}.
func isStringTableDoc(doc *yaml.Node) bool {
	if doc == nil || doc.Kind != yaml.MappingNode {
		return false
	}
	hasVersion, hasLocale, hasEntries := false, false, false
	for i := 0; i+1 < len(doc.Content); i += 2 {
		k, v := doc.Content[i].Value, deref(doc.Content[i+1])
		switch k {
		case "version":
			if v == nil || v.Kind != yaml.ScalarNode {
				return false
			}
			hasVersion = true
		case "locale":
			if v == nil || v.Kind != yaml.ScalarNode || v.Value == "" {
				return false
			}
			hasLocale = true
		case "strings", "plurals":
			if v != nil && v.Kind == yaml.MappingNode {
				hasEntries = true
			}
		default:
			return false
		}
	}
	return hasVersion && hasLocale && hasEntries
}

// isGridMapName reports whether name is a 3D grid map file by its name, *.gridmap.yaml or *.gridmap.yml.
func isGridMapName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".gridmap.yaml") || strings.HasSuffix(lower, ".gridmap.yml")
}

// isStatesName reports whether name is a sprite state machine file, *.states.yaml or *.states.yml.
func isStatesName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".states.yaml") || strings.HasSuffix(lower, ".states.yml")
}

// isResourceKind reports whether n is a scalar naming a resource type.
func isResourceKind(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && slices.Contains(resource.Kinds(), resource.Kind(n.Value))
}

// add records f and its uid. An empty or null uid, which sceneformat and resource read as no uid, is kept as f.uid without f.UID, so that game index fills it in.
func (p *Project) add(f *File) {
	if f.Kind == KindScene || f.Kind == KindPrefab || f.Kind == KindResource || f.Kind == KindScreen || f.Kind == KindStates {
		k, n := entry(f.doc, "uid")
		switch {
		case n == nil || n.Kind != yaml.ScalarNode:
		case resource.ValidUID(n.Value):
			f.UID, f.uid, f.uidKey = n.Value, n, k
			p.uids[f.UID] = append(p.uids[f.UID], f.Path)
			slices.Sort(p.uids[f.UID])
		case n.Value == "" || n.Tag == "!!null":
			f.uid, f.uidKey = n, k
		}
	}
	p.Files = append(p.Files, f)
	p.files[f.Path] = f
}

// sceneRefs records the references of the scene document doc.
func (p *Project) sceneRefs(f *File, doc *yaml.Node) {
	p.ref(f, field(doc, "inherits"), FieldInherits)
	// game navmesh writes the navigation mesh file, so it need not exist yet.
	if r := p.ref(f, field(doc, "navMesh"), FieldNavMesh); r != nil {
		r.Output = true
	}
	if env := field(doc, "environment"); env != nil {
		if sky := field(env, "sky"); sky != nil {
			p.ref(f, field(sky, "hdr"), FieldSkyHDR)
		}
	}
	for _, kv := range pairs(field(doc, "tilemaps")) {
		p.tilemapRefs(f, kv[1])
	}
	for _, kv := range pairs(field(doc, "layerOverrides")) {
		p.tilemapRefs(f, field(kv[1], "tilemap"))
	}
	for _, kv := range pairs(field(doc, "overrides")) {
		if pf := field(kv[1], "prefab"); p.isPrefabFileRef(f.Root, pf) {
			p.ref(f, pf, FieldPrefab)
		}
		p.ref(f, field(kv[1], "material"), FieldMaterial)
		p.ref(f, field(kv[1], "model"), FieldModel)
		p.ref(f, field(kv[1], "gridMap"), FieldGridMap)
		p.animatorRefs(f, field(kv[1], "components"))
		p.ref(f, field(field(kv[1], "particles"), "file"), FieldParticles)
		p.ref(f, field(field(kv[1], "reflectionProbe"), "cubemap"), FieldReflectionProbe)
		p.ref(f, field(field(kv[1], "lightProbeGrid"), "data"), FieldLightProbeGrid)
		p.skeletalComponentRef(f, kv[1])
	}
	for _, l := range items(field(doc, "layers")) {
		p.tilemapRefs(f, field(l, "tilemap"))
		p.objectRefs(f, field(l, "objects"), 0)
	}
}

// tilemapRefs records the references of the scene tilemap tm: its source and the YAML tileset a layered grid names.
func (p *Project) tilemapRefs(f *File, tm *yaml.Node) {
	p.ref(f, field(tm, "source"), FieldTilemapSource)
	p.ref(f, field(tm, "tilesetFile"), FieldTilesetFile)
}

// isTilesetName reports whether name is a YAML tileset by its name, *.tileset.yaml or *.tileset.yml.
func isTilesetName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".tileset.yaml") || strings.HasSuffix(lower, ".tileset.yml")
}

// isTerrainName reports whether name is a YAML 3D terrain definition by its name, *.terrain.yaml or *.terrain.yml.
func isTerrainName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".terrain.yaml") || strings.HasSuffix(lower, ".terrain.yml")
}

// terrainRefs records the asset references of a *.terrain.yaml document: its heightmap, splat maps, per-layer materials/weight maps/textures, and foliage meshes/materials/density maps.
func (p *Project) terrainRefs(f *File, doc *yaml.Node) {
	p.ref(f, field(doc, "heightmap"), FieldTerrainHeightmap)
	for _, sm := range items(field(doc, "splatMaps")) {
		p.ref(f, sm, FieldTerrainSplatMap)
	}
	for _, l := range items(field(doc, "layers")) {
		p.ref(f, field(l, "material"), FieldMaterial)
		p.ref(f, field(l, "weightMap"), FieldTerrainWeightMap)
		p.ref(f, field(l, "albedo"), FieldTerrainTexture)
		p.ref(f, field(l, "normal"), FieldTerrainTexture)
		p.ref(f, field(l, "metallicRoughness"), FieldTerrainTexture)
		p.ref(f, field(l, "height"), FieldTerrainTexture)
	}
	for _, fl := range items(field(doc, "foliage")) {
		p.ref(f, field(fl, "mesh"), FieldTerrainMesh)
		p.ref(f, field(fl, "material"), FieldMaterial)
		p.ref(f, field(fl, "densityMap"), FieldTerrainDensity)
	}
}

// maxDepth bounds how deep objectRefs follows children, well past sceneformat.MaxObjectDepth, so a hostile file cannot exhaust the stack.
const maxDepth = 1024

// objectRefs records the references of the objects in the sequence objs and their children.
func (p *Project) objectRefs(f *File, objs *yaml.Node, depth int) {
	if depth > maxDepth {
		return
	}
	for _, o := range items(objs) {
		p.ref(f, field(o, "scene"), FieldScene)
		if pf := field(o, "prefab"); p.isPrefabFileRef(f.Root, pf) {
			p.ref(f, pf, FieldPrefab)
		}
		p.ref(f, field(o, "material"), FieldMaterial)
		p.ref(f, field(o, "model"), FieldModel)
		p.ref(f, field(o, "gridMap"), FieldGridMap)
		p.animatorRefs(f, field(o, "components"))
		p.ref(f, field(field(o, "particles"), "file"), FieldParticles)
		p.ref(f, field(field(o, "reflectionProbe"), "cubemap"), FieldReflectionProbe)
		p.ref(f, field(field(o, "lightProbeGrid"), "data"), FieldLightProbeGrid)
		p.skeletalComponentRef(f, o)
		for _, kv := range pairs(field(o, "overrides")) {
			if pf := field(kv[1], "prefab"); p.isPrefabFileRef(f.Root, pf) {
				p.ref(f, pf, FieldPrefab)
			}
			p.ref(f, field(kv[1], "material"), FieldMaterial)
			p.ref(f, field(kv[1], "model"), FieldModel)
			p.ref(f, field(kv[1], "gridMap"), FieldGridMap)
			p.animatorRefs(f, field(kv[1], "components"))
			p.ref(f, field(field(kv[1], "particles"), "file"), FieldParticles)
			p.ref(f, field(field(kv[1], "reflectionProbe"), "cubemap"), FieldReflectionProbe)
			p.ref(f, field(field(kv[1], "lightProbeGrid"), "data"), FieldLightProbeGrid)
			p.skeletalComponentRef(f, kv[1])
		}
		p.objectRefs(f, field(o, "children"), depth+1)
	}
}

func (p *Project) skeletalComponentRef(f *File, obj *yaml.Node) {
	comps := field(obj, "components")
	skel := field(comps, "skeletal")
	p.ref(f, field(skel, "rig"), FieldSkeletalRig)
}

// screenRefs records sprite path references and localization textKey usages in a *.screen.yaml node tree.
func (p *Project) screenRefs(f *File, nodes *yaml.Node, depth int) {
	if depth > maxDepth {
		return
	}
	for _, n := range items(nodes) {
		p.ref(f, field(n, "sprite"), FieldScreenSprite)
		p.ref(f, field(n, "under"), FieldScreenSprite)
		p.ref(f, field(n, "progress"), FieldScreenSprite)
		p.ref(f, field(n, "over"), FieldScreenSprite)
		var nodeID string
		if idNode := field(n, "id"); idNode != nil && idNode.Kind == yaml.ScalarNode {
			nodeID = idNode.Value
		}
		p.textKeyRef(f, field(n, "textKey"), FieldTextKey, nodeID)
		p.screenRefs(f, field(n, "children"), depth+1)
	}
}

// stringTableKeys records localization key definitions in an l10n YAML string table's strings and plurals maps.
func (p *Project) stringTableKeys(f *File, doc *yaml.Node) {
	for _, kv := range pairs(field(doc, "strings")) {
		p.textKeyRef(f, kv[0], FieldStringKey, "")
	}
	for _, kv := range pairs(field(doc, "plurals")) {
		p.textKeyRef(f, kv[0], FieldPluralKey, "")
	}
}

// textKeyRef records a localization key scalar occurrence in f.
func (p *Project) textKeyRef(f *File, n *yaml.Node, what, nodeID string) *TextKeyRef {
	if n == nil || n.Kind != yaml.ScalarNode || n.Value == "" || (n.Tag != "" && n.Tag != "!!str") {
		return nil
	}
	r := TextKeyRef{File: f.Path, Line: n.Line, Column: n.Column, Field: what, Key: n.Value, NodeID: nodeID}
	if s, err := yamlSpan(f.data, f.starts, n); err == nil {
		r.span = s
	} else {
		r.span = span{start: -1}
	}
	p.TextKeys = append(p.TextKeys, r)
	return &p.TextKeys[len(p.TextKeys)-1]
}

// TextKeyUsages returns every occurrence of key in the scanned project across screen files and string tables, in file and line order.
func (p *Project) TextKeyUsages(key string) []TextKeyRef {
	var out []TextKeyRef
	for _, r := range p.TextKeys {
		if r.Key == key {
			out = append(out, r)
		}
	}
	return out
}

func (p *Project) animatorRefs(f *File, comps *yaml.Node) {
	if anim := field(comps, "animator"); anim != nil {
		p.ref(f, field(anim, "states"), FieldStates)
	}
}

// ref records the root-relative reference whose value is the node n of f, when n is a non-empty string, and returns it, or nil.
func (p *Project) ref(f *File, n *yaml.Node, what string) *Ref {
	if n == nil || n.Kind != yaml.ScalarNode || n.Value == "" || (n.Tag != "" && n.Tag != "!!str") {
		return nil
	}
	r := Ref{File: f.Path, Line: n.Line, Column: n.Column, Field: what, Value: n.Value}
	if s, err := yamlSpan(f.data, f.starts, n); err == nil {
		r.span = s
	} else {
		r.span = span{start: -1}
	}
	if validPath(n.Value) {
		r.Target = path.Join(f.Root, n.Value)
	}
	p.Refs = append(p.Refs, r)
	return &p.Refs[len(p.Refs)-1]
}

// field returns the value of key in the mapping n, following aliases, or nil.
func field(n *yaml.Node, key string) *yaml.Node {
	_, v := entry(n, key)
	return v
}

// entry returns the key node and the value of key in the mapping n, following aliases, or nils.
func entry(n *yaml.Node, key string) (k, v *yaml.Node) {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i], deref(n.Content[i+1])
		}
	}
	return nil, nil
}

// items returns the items of the sequence n, or nil.
func items(n *yaml.Node) []*yaml.Node {
	n = deref(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]*yaml.Node, len(n.Content))
	for i, c := range n.Content {
		out[i] = deref(c)
	}
	return out
}

// pairs returns the key and value of each entry of the mapping n, or nil.
func pairs(n *yaml.Node) [][2]*yaml.Node {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out [][2]*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, [2]*yaml.Node{n.Content[i], deref(n.Content[i+1])})
	}
	return out
}

func deref(n *yaml.Node) *yaml.Node {
	for range maxDepth {
		if n == nil || n.Kind != yaml.AliasNode {
			return n
		}
		n = n.Alias
	}
	return nil
}

// formatAtlas is the format readJSON reads a sprite atlas as.
const formatAtlas = "atlas"

// jsonFields are the paths, with "*" for any index or key and "**" for any run of layers' groups, of the string values in Tiled and LDtk files, glTF models and sprite atlases that name a file relative to the file. format is the extension of the Tiled, LDtk or glTF file, ".tsj" also for a tileset named .json, or formatAtlas.
var jsonFields = []struct {
	format string
	path   []string
	field  string
}{
	{".tmj", []string{"tilesets", "*", "source"}, FieldExternalTileset},
	{".tmj", []string{"tilesets", "*", "image"}, FieldTilesetImage},
	{".tmj", []string{"tilesets", "*", "tiles", "*", "image"}, FieldTileImage},
	{".tmj", []string{"**", "image"}, FieldImageLayer},
	{".tsj", []string{"image"}, FieldTilesetImage},
	{".tsj", []string{"tiles", "*", "image"}, FieldTileImage},
	{".ldtk", []string{"defs", "tilesets", "*", "relPath"}, FieldTilesetImage},
	{".ldtk", []string{"levels", "*", "bgRelPath"}, FieldLevelBackground},
	{".ldtk", []string{"levels", "*", "externalRelPath"}, FieldExternalLevel},
	{formatAtlas, []string{"meta", "image"}, FieldAtlasImage},
	{".gltf", []string{"buffers", "*", "uri"}, FieldModelBuffer},
	{".gltf", []string{"images", "*", "uri"}, FieldModelImage},
}

// matchJSON reports whether path matches pattern: "*" matches one step, and a leading "**" matches one or more "layers", index pairs, as Tiled nests group layers.
func matchJSON(pattern, path []string) bool {
	if len(pattern) > 0 && pattern[0] == "**" {
		rest := pattern[1:]
		n := len(path) - len(rest)
		if n < 2 || n%2 != 0 {
			return false
		}
		for i := 0; i < n; i += 2 {
			if path[i] != "layers" {
				return false
			}
		}
		return matchJSON(rest, path[n:])
	}
	if len(pattern) != len(path) {
		return false
	}
	for i := range pattern {
		if pattern[i] != "*" && pattern[i] != path[i] {
			return false
		}
	}
	return true
}

// readJSON reads the references of the Tiled or LDtk file, glTF model or sprite atlas at name, read as format, one of jsonFields' formats. A glTF buffer or image held in a data URI names no file and is not a reference.
func (p *Project) readJSON(name, format string) {
	data, err := fs.ReadFile(p.fsys, name)
	if err != nil {
		return
	}
	kind := KindTiled
	switch format {
	case ".ldtk":
		kind = KindLDtk
	case formatAtlas:
		kind = KindAtlas
	case ".gltf":
		kind = KindGLTF
	}
	f := &File{Path: name, Kind: kind, data: data, starts: lineStarts(data), Root: p.Root(name)}
	var refs []Ref
	err = jsonStrings(data, func(jp []string, s span, value string) {
		if value == "" || (format == ".gltf" && strings.HasPrefix(value, "data:")) {
			return
		}
		for _, jf := range jsonFields {
			if jf.format != format || !matchJSON(jf.path, jp) {
				continue
			}
			line, col := position(data, f.starts, s.start)
			r := Ref{File: name, Line: line, Column: col, Field: jf.field, Value: value, FromFile: true, span: s}
			target := value
			if format == ".gltf" {
				// A glTF URI is percent-encoded, as gltf.LoadFS reads it.
				r.uri = true
				if u, err := url.PathUnescape(value); err == nil {
					target = u
				}
			}
			r.Target, _ = resolveFrom(path.Dir(name), target)
			refs = append(refs, r)
			return
		}
	})
	if err != nil {
		return
	}
	p.add(f)
	p.Refs = append(p.Refs, refs...)
}

// isAtlas reports whether the JSON file at name is a sprite atlas: an object with top-level frames, an object or array, and meta, an object, as package tooling/assetcheck finds atlases.
func isAtlas(fsys fs.FS, name string) bool {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil {
		return false
	}
	frames, meta := bytes.TrimSpace(top["frames"]), bytes.TrimSpace(top["meta"])
	return len(frames) > 0 && (frames[0] == '{' || frames[0] == '[') && len(meta) > 0 && meta[0] == '{'
}

// resolveFrom joins ref, a path written in a file, to dir, the directory it is relative to, and returns the slash-separated path in the scanned directory, or false when ref cannot name a file there.
func resolveFrom(dir, ref string) (string, bool) {
	if strings.ContainsRune(ref, '\\') || path.IsAbs(ref) || hasVolume(ref) || hasScheme(ref) {
		return "", false
	}
	p := path.Join(dir, ref)
	if !validPath(p) {
		return "", false
	}
	return p, true
}

// hasVolume reports whether ref starts with a Windows drive letter, such as C:.
func hasVolume(ref string) bool {
	return len(ref) >= 2 && ref[1] == ':' && ('a' <= ref[0]|0x20 && ref[0]|0x20 <= 'z')
}

// hasScheme reports whether ref starts with a URI scheme of two or more letters, such as asset: or https:.
func hasScheme(ref string) bool {
	i := strings.IndexByte(ref, ':')
	if i < 2 {
		return false
	}
	for j := range i {
		b := ref[j]
		letter := 'a' <= b|0x20 && b|0x20 <= 'z'
		if !letter && (j == 0 || !('0' <= b && b <= '9' || b == '+' || b == '-' || b == '.')) {
			return false
		}
	}
	return true
}

// RelPath returns how a file in dir, or at the project root dir, writes the path of target: target relative to dir, with ".." for each directory up, both slash-separated paths in the same file system.
func RelPath(dir, target string) string {
	if dir == "." {
		return target
	}
	d, t := strings.Split(dir, "/"), strings.Split(target, "/")
	i := 0
	for i < len(d) && i < len(t)-1 && d[i] == t[i] {
		i++
	}
	return strings.Repeat("../", len(d)-i) + strings.Join(t[i:], "/")
}

// sha returns the SHA-256 of the file at name, reading it once, with CRLF normalized to LF for text files.
func (p *Project) sha(name string) (string, error) {
	if h, ok := p.hashes[name]; ok {
		return h, nil
	}
	data, err := fs.ReadFile(p.fsys, name)
	if err != nil {
		return "", err
	}
	h := hashContent(name, data)
	p.hashes[name] = h
	return h, nil
}

// Moved returns where the file at target, a path the scan found no file at, is now, as the project index at its root says: the scene or resource file holding the uid the index lists for target, or else the only file in the project with target's extension and the contents the index lists for it that the index does not list itself. It returns false when the index does not list target or the file cannot be found again.
func (p *Project) Moved(target string) (string, bool) {
	if p.exists[target] {
		return "", false
	}
	root := p.Root(target)
	ix := p.indexes[root]
	e, ok := ix.Lookup(strings.TrimPrefix(target, rootPrefix(root)))
	if !ok {
		return "", false
	}
	if e.UID != "" {
		var found []string
		holders := p.uids[e.UID]
		if len(holders) == 0 {
			// A scene not named *.scene.yaml that no reference names any more was not read; find the uid among the other YAML files.
			holders = p.otherUIDs()[e.UID]
		}
		for _, h := range holders {
			if p.Root(h) == root {
				found = append(found, h)
			}
		}
		if len(found) == 1 {
			return found[0], true
		}
		return "", false
	}
	ext := strings.ToLower(path.Ext(target))
	var found string
	for _, c := range p.paths {
		if strings.ToLower(path.Ext(c)) != ext || p.Root(c) != root {
			continue
		}
		if _, listed := ix.Lookup(strings.TrimPrefix(c, rootPrefix(root))); listed {
			continue
		}
		if h, err := p.sha(c); err != nil || h != e.SHA256 {
			continue
		}
		if found != "" {
			return "", false
		}
		found = c
	}
	return found, found != ""
}

// otherUIDs returns the files holding each uid among the YAML files Scan did not read, reading them the first time.
func (p *Project) otherUIDs() map[string][]string {
	if p.pendingUIDs != nil {
		return p.pendingUIDs
	}
	p.pendingUIDs = make(map[string][]string)
	for _, name := range p.pending {
		data, err := fs.ReadFile(p.fsys, name)
		if err != nil {
			continue
		}
		var root yaml.Node
		if yaml.Unmarshal(data, &root) != nil || len(root.Content) == 0 {
			continue
		}
		if n := field(root.Content[0], "uid"); n != nil && n.Kind == yaml.ScalarNode && resource.ValidUID(n.Value) && field(root.Content[0], "version") != nil {
			p.pendingUIDs[n.Value] = append(p.pendingUIDs[n.Value], name)
		}
	}
	return p.pendingUIDs
}

// pendingUID returns the uid of name when it is one of the YAML files Scan did not read and holds a uid, or "".
func (p *Project) pendingUID(name string) string {
	for id, holders := range p.otherUIDs() {
		if slices.Contains(holders, name) {
			return id
		}
	}
	return ""
}

// rootPrefix returns what a path inside root starts with: "" for the scanned directory, else root and a slash.
func rootPrefix(root string) string {
	if root == "." {
		return ""
	}
	return root + "/"
}

// Check returns the problems Scan found and every reference to a file that is not there, at the file and line that holds the reference, saying where the file went when Moved finds it. Problems are sorted by file, then line, then message.
func (p *Project) Check() []Problem {
	out := slices.Clone(p.Problems)
	for _, r := range p.Refs {
		if r.Target == "" || r.Output || p.Found(r) {
			continue
		}
		msg := fmt.Sprintf("%s %q: not found", r.Field, r.Value)
		if hint := p.MovedHint(r); hint != "" {
			msg += "; " + hint
		}
		out = append(out, Problem{File: r.File, Line: r.Line, Message: msg})
	}
	slices.SortFunc(out, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Message, b.Message))
	})
	return slices.Compact(out)
}

// Found reports whether r names something that was there when Scan walked the directory: a file, or a folder when r.Dir is set.
func (p *Project) Found(r Ref) bool {
	if r.Dir {
		return p.dirs[r.Target]
	}
	return p.exists[r.Target]
}

// MovedHint returns what to tell a person about r when its target is gone and Moved finds it, such as `it moved to "materials/rock.material.yaml"; run game index to update the reference`, or "" when Moved does not find it.
func (p *Project) MovedHint(r Ref) string {
	if r.Target == "" || r.Dir {
		return ""
	}
	to, ok := p.Moved(r.Target)
	if !ok {
		return ""
	}
	return fmt.Sprintf("it moved to %q; run game index to update the reference", p.valueFor(r, r.File, to))
}

// uriFor returns the percent-encoded URI reference of the relative path rel, keeping value, the URI written now, when it already names rel, so that a reference whose path does not change keeps its spelling.
func uriFor(value, rel string) string {
	if u, err := url.PathUnescape(value); err == nil && u == rel {
		return value
	}
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// valueFor returns how r, held by the file now at file, writes the path of target.
func (p *Project) valueFor(r Ref, file, target string) string {
	if r.FromFile {
		rel := RelPath(path.Dir(file), target)
		if r.uri {
			return uriFor(r.Value, rel)
		}
		return rel
	}
	return strings.TrimPrefix(target, rootPrefix(p.Root(r.File)))
}

package project

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// Resolver loads a project's assets from its files, as the scene editor and the game read them: the sprite atlases, scene files, materials, physics materials, YAML tilesets and Tiled and LDtk maps a scene names. It is a sceneformat.AssetResolver that also implements sceneformat.SceneResolver, sceneformat.MaterialResolver, sceneformat.PhysicsMaterialResolver and sceneformat.FileResolver, and it lists the project's atlases (AtlasNames) for the editor's asset browser.
//
// A scene names an atlas the way `game check` (package assetcheck) finds it: the name is the path of an atlas JSON file without its .json extension, either from the project root or as the file's base name, and when several atlases match, the one nearest the scene wins (NearestAtlases). An atlas JSON is a JSON file with top-level frames and meta objects, as TexturePacker and Aseprite export them; its meta.image is read from the project relative to the JSON file, so the atlas draws the project's own image. Scene files, materials, tilesets and tilemap sources are paths from the project root.
//
// The walk for atlases skips the directories the go command skips (names starting with "." or "_", and testdata) and the directories of other projects, those holding a game.yaml, nsgo.yaml or go.mod of their own. A Resolver lists the atlases and parses each one once, then serves them until Refresh; scene files and the files ReadFile reads are read again on every request, so a scene saved since is seen at once. A Resolver is safe for use by multiple goroutines.
type Resolver struct {
	files *projectFiles
	// dir is the slash-separated directory, from the project root, that atlases are found nearest to: "." for the root.
	dir string
}

// projectFiles is what the Resolvers over one project share: its file system and what has been read from it.
type projectFiles struct {
	fsys fs.FS

	mu sync.Mutex
	// materials loads the project's materials and physics materials; Refresh replaces it.
	materials *resource.Library
	// listed reports whether atlasPaths holds the walk's result.
	listed bool
	// atlasPaths are the project's atlas JSON files, sorted, and listErr is why the walk failed, if it did.
	atlasPaths []string
	listErr    error
	// atlases caches each atlas file loaded, or why it did not load, by path.
	atlases map[string]loadedAtlas
	// ldtkLoaded caches LDtkOptions read from game.yaml until Refresh.
	ldtkLoaded bool
	ldtkOpts   tilemap.LDtkOptions
}

// loadedAtlas is an atlas file's load result.
type loadedAtlas struct {
	atlas *sprite.Atlas
	err   error
}

// NewResolver returns a Resolver over fsys, the project's files with paths from its root, such as os.DirFS of the directory holding game.yaml. It finds atlases nearest the project root; ForScene returns one that finds them nearest a scene.
func NewResolver(fsys fs.FS) *Resolver {
	return &Resolver{files: &projectFiles{
		fsys:      fsys,
		materials: resource.NewLibrary(fsys),
		atlases:   make(map[string]loadedAtlas),
	}, dir: "."}
}

// ForScene returns a Resolver over the same files and caches as r that finds atlases nearest the scene file at scenePath, a slash-separated path from the project root such as "scenes/level.scene.yaml", as `game check` finds a scene's atlases. An empty or invalid scenePath finds them nearest the project root.
func (r *Resolver) ForScene(scenePath string) *Resolver {
	dir := "."
	if scenePath != "" && fs.ValidPath(scenePath) {
		dir = path.Dir(scenePath)
	}
	return &Resolver{files: r.files, dir: dir}
}

// Refresh forgets the atlases, materials and physics materials r has listed and loaded, so the next request reads the project's files again: call it after the project's atlases or material files change on disk. It is shared with every Resolver ForScene returned. Until then every scene that names one material file gets the same value.
func (r *Resolver) Refresh() {
	f := r.files
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed, f.atlasPaths, f.listErr = false, nil, nil
	f.ldtkLoaded, f.ldtkOpts = false, tilemap.LDtkOptions{}
	clear(f.atlases)
	f.materials = resource.NewLibrary(f.fsys)
}

// Atlas returns the atlas name names, found as Resolver describes. It fails when no atlas matches, when several match at the same distance (name it by its path from the project root instead), and when the matching file or its image does not load.
func (r *Resolver) Atlas(name string) (*sprite.Atlas, error) {
	f := r.files
	f.mu.Lock()
	defer f.mu.Unlock()
	paths, err := f.list()
	if err != nil {
		return nil, err
	}
	switch matches := NearestAtlases(paths, r.dir, name); len(matches) {
	case 0:
		return nil, fmt.Errorf("project: atlas %q not found: no atlas JSON named %s.json in the project", name, path.Base(name))
	case 1:
		return f.atlas(matches[0])
	default:
		return nil, fmt.Errorf("project: atlas %q is ambiguous: %s all match; name it by its path", name, strings.Join(matches, ", "))
	}
}

// AtlasNames returns the names a scene can give the project's atlases, sorted: each atlas's base name when that name finds it from r's scene, as Atlas would, and otherwise its path from the project root without .json. It lists an atlas that does not load too; Atlas reports why. It lets a Resolver serve as the editor's editor.AtlasLister, and returns nil when the project's files cannot be listed.
func (r *Resolver) AtlasNames() []string {
	f := r.files
	f.mu.Lock()
	defer f.mu.Unlock()
	paths, err := f.list()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		stem := strings.TrimSuffix(p, path.Ext(p))
		name := path.Base(stem)
		if m := NearestAtlases(paths, r.dir, name); len(m) != 1 || m[0] != p {
			name = stem
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// Scene reads and parses the scene file at p, a slash-separated path from the project root, as sceneformat.FSScenes does, and names each atlas it names, by its sprites, its overrides' sprites and its inline tilemaps' tilesets, by the atlas's path from the project root without .json when the name finds one atlas nearest p. sceneformat.Build looks up every atlas of a scene and of the scenes it instances or inherits through one resolver, which finds them nearest the open scene, so without this an instanced scene in another directory would draw the atlas nearest the scene instancing it, not the one `game check` finds nearest the instanced file. A name that finds no atlas, or several, is left as it is, for Atlas to report.
func (r *Resolver) Scene(p string) (*sceneformat.SceneDoc, error) {
	doc, err := sceneformat.FSScenes{FS: r.files.fsys}.Scene(p)
	if err != nil || doc == nil {
		return doc, err
	}
	f := r.files
	f.mu.Lock()
	paths, lerr := f.list()
	f.mu.Unlock()
	if lerr != nil {
		return doc, nil
	}
	dir := path.Dir(p)
	rename := func(name *string) {
		if m := NearestAtlases(paths, dir, *name); len(m) == 1 {
			*name = strings.TrimSuffix(m[0], path.Ext(m[0]))
		}
	}
	renameSprite := func(s *sceneformat.SpriteRef) {
		if s != nil && s.Atlas != "" {
			rename(&s.Atlas)
		}
	}
	renameOverrides := func(overrides map[string]sceneformat.ObjectOverride) {
		for id, o := range overrides {
			if o.Sprite != nil {
				sp := *o.Sprite
				renameSprite(&sp)
				o.Sprite = &sp
				overrides[id] = o
			}
		}
	}
	var renameObjects func(objs []sceneformat.ObjectDoc)
	renameObjects = func(objs []sceneformat.ObjectDoc) {
		for i := range objs {
			o := &objs[i]
			renameSprite(o.Sprite)
			renameOverrides(o.Overrides)
			renameObjects(o.Children)
		}
	}
	renameTileset := func(tm *sceneformat.TilemapDoc) {
		if tm != nil && tm.Tileset != "" {
			rename(&tm.Tileset)
		}
	}
	for i := range doc.Layers {
		renameObjects(doc.Layers[i].Objects)
		renameTileset(doc.Layers[i].Tilemap)
	}
	renameOverrides(doc.Overrides)
	for _, tm := range doc.Tilemaps {
		renameTileset(tm)
	}
	return doc, nil
}

// Material loads the material file at p, a slash-separated path from the project root, through a resource.Library shared by every Resolver over the project, so every scene that names one file gets the same value until Refresh.
func (r *Resolver) Material(p string) (*resource.Material, error) {
	return r.files.library().Material(p)
}

// PhysicsMaterial loads the physics material file at p, a .physics.yaml that a body2d or body3d component names by its path from the project root, through the same resource.Library as Material, so sceneformat.BuildPhysics, which the editor's Play runs, finds the project's physics materials.
func (r *Resolver) PhysicsMaterial(p string) (*resource.PhysicsMaterial, error) {
	return r.files.library().PhysicsMaterial(p)
}

// Library returns the resource.Library that loads the project's materials and physics materials, so callers such as the scene editor can reload edited material files in place (resource.Library.Reload) until Refresh replaces it.
func (r *Resolver) Library() *resource.Library {
	if r == nil || r.files == nil {
		return nil
	}
	return r.files.library()
}

// library returns the resource.Library that loads the project's materials, which Refresh replaces.
func (f *projectFiles) library() *resource.Library {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.materials
}

// ReadFile reads the file at p, a slash-separated path from the project root, so that sceneformat.Build reads through it the YAML tilesets layered tilemaps name and the Tiled and LDtk maps tilemap sources name, with the tilesets those refer to, and the editor's tile palette lists their tiles, as sceneformat.FSAssets does. Their images are left as URIs, as FSAssets leaves them.
func (r *Resolver) ReadFile(p string) ([]byte, error) {
	return fs.ReadFile(r.files.fsys, p)
}

// LDtkOptions returns the tilemap.LDtkOptions derived from game.yaml's physics.ldtkIntGrid section, if present,
// implementing sceneformat.LDtkOptionsResolver so scenes referencing .ldtk sources map IntGrid values from project settings.
func (r *Resolver) LDtkOptions() tilemap.LDtkOptions {
	f := r.files
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.ldtkLoaded {
		f.ldtkLoaded = true
		if pf, err := ReadFS(f.fsys); err == nil && pf != nil {
			f.ldtkOpts = pf.LDtkOptions(nil)
		}
	}
	return tilemap.LDtkOptions{
		ResolveImage:      f.ldtkOpts.ResolveImage,
		IntGridFlags:      maps.Clone(f.ldtkOpts.IntGridFlags),
		IntGridValueFlags: maps.Clone(f.ldtkOpts.IntGridValueFlags),
	}
}

// list returns the project's atlas files, walking it on first use. f.mu is held.
func (f *projectFiles) list() ([]string, error) {
	if f.listed {
		return f.atlasPaths, f.listErr
	}
	var paths []string
	err := fs.WalkDir(f.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			// A directory or file that cannot be read holds no atlas the project can load.
			return nil
		}
		if d.IsDir() {
			if p != "." && (SkipDir(d.Name()) || f.otherProject(p)) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.EqualFold(path.Ext(p), ".json") && f.isAtlas(p) {
			paths = append(paths, p)
		}
		return nil
	})
	slices.Sort(paths)
	f.listed, f.atlasPaths, f.listErr = true, paths, err
	return paths, err
}

// SkipDir reports whether a walk of a project's files skips the directory called name, as the go command skips it: a name starting with "." or "_", or testdata. Resolver and `game check` both skip them.
func SkipDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata"
}

// otherProject reports whether the directory dir holds a project of its own: a game.yaml, nsgo.yaml or go.mod.
func (f *projectFiles) otherProject(dir string) bool {
	for _, name := range []string{FileName, NSGoFileName, "go.mod"} {
		if _, err := fs.Stat(f.fsys, path.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// isAtlas reports whether the JSON file at p is an atlas (IsAtlasJSON).
func (f *projectFiles) isAtlas(p string) bool {
	data, err := fs.ReadFile(f.fsys, p)
	return err == nil && IsAtlasJSON(data)
}

// IsAtlasJSON reports whether data is a JSON object with top-level frames (an object or an array) and meta (an object), as TexturePacker and Aseprite export an atlas: the test Resolver and `game check` both tell a project's atlas JSON files from its other JSON files by.
func IsAtlasJSON(data []byte) bool {
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil {
		return false
	}
	frames, meta := jsonKind(top["frames"]), jsonKind(top["meta"])
	return (frames == '{' || frames == '[') && meta == '{'
}

// jsonKind returns the first byte of the JSON value v, which tells an object ('{') and an array ('[') from other values, or 0 for none.
func jsonKind(v json.RawMessage) byte {
	if len(v) == 0 {
		return 0
	}
	return v[0]
}

// atlas loads the atlas file at p once, with its meta.image read from the project relative to it. f.mu is held.
func (f *projectFiles) atlas(p string) (*sprite.Atlas, error) {
	if l, ok := f.atlases[p]; ok {
		return l.atlas, l.err
	}
	a, err := f.loadAtlas(p)
	f.atlases[p] = loadedAtlas{a, err}
	return a, err
}

// loadAtlas reads and loads the atlas file at p and its image.
func (f *projectFiles) loadAtlas(p string) (*sprite.Atlas, error) {
	data, err := fs.ReadFile(f.fsys, p)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Meta struct {
			Image string `json:"image"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, loaderr.Locate(err, p, data, "export the atlas again from TexturePacker or Aseprite")
	}
	var img core.ImageSource
	if ref := meta.Meta.Image; ref != "" {
		imgPath, err := ResolveRef(path.Dir(p), ref)
		if err != nil {
			return nil, fmt.Errorf("%s: meta.image: %w", p, err)
		}
		if img.Data, err = fs.ReadFile(f.fsys, imgPath); err != nil {
			return nil, fmt.Errorf("%s: meta.image: %w", p, err)
		}
	}
	a, err := sprite.LoadAtlas(img, data)
	if err != nil {
		return nil, loaderr.WithFile(err, p)
	}
	return a, nil
}

// ResolveRef joins ref, a slash-separated path written in a file of a project, to dir, the slash-separated directory from the project root it is relative to, and returns the path from the project root, or an error saying why ref cannot name a file there: it refuses a path with backslashes, an absolute path or one starting with a Windows drive letter such as C:, a URI with a scheme of two or more letters (HasScheme), and a path that leads outside the root. `game check` (package assetcheck) reads the paths files write with it, and Resolver reads an atlas's meta.image with it, so the two accept the same images.
func ResolveRef(dir, ref string) (string, error) {
	switch {
	case strings.ContainsRune(ref, '\\'):
		return "", fmt.Errorf("%q uses backslashes; write paths with forward slashes", ref)
	case path.IsAbs(ref) || hasVolume(ref):
		return "", fmt.Errorf("%q is an absolute path; write it relative to the file", ref)
	case HasScheme(ref):
		return "", fmt.Errorf("%q is a URI, not a path relative to the file", ref)
	}
	p := path.Join(dir, ref)
	if !fs.ValidPath(p) {
		return "", fmt.Errorf("%q leads outside the checked directory", ref)
	}
	return p, nil
}

// hasVolume reports whether ref starts with a Windows drive letter, such as C:.
func hasVolume(ref string) bool {
	return len(ref) >= 2 && ref[1] == ':' && ('a' <= ref[0]|0x20 && ref[0]|0x20 <= 'z')
}

// HasScheme reports whether ref starts with a URI scheme of two or more letters, such as asset: or https:, which ResolveRef refuses as a path.
func HasScheme(ref string) bool {
	i := strings.IndexByte(ref, ':')
	if i < 2 {
		return false
	}
	for j := 0; j < i; j++ {
		b := ref[j]
		letter := 'a' <= b|0x20 && b|0x20 <= 'z'
		if !letter && (j == 0 || !('0' <= b && b <= '9' || b == '+' || b == '-' || b == '.')) {
			return false
		}
	}
	return true
}

// NearestAtlases returns which of atlases, the slash-separated paths of a project's atlas JSON files from its root, a scene in the directory from (a path from the root, "." for the root itself) names name, as `game check` and Resolver find them: the files whose path without its extension is name, or whose base name without its extension is name, keeping only those nearest from, counting the directories up from from and down to the file. It returns them sorted; more than one means name is ambiguous there, and none that it names no atlas.
func NearestAtlases(atlases []string, from, name string) []string {
	if name == "" {
		return nil
	}
	var best []string
	bestDist := -1
	for _, p := range atlases {
		if !AtlasNamed(p, name) {
			continue
		}
		d := dirDistance(from, path.Dir(p))
		switch {
		case bestDist < 0 || d < bestDist:
			best, bestDist = []string{p}, d
		case d == bestDist:
			best = append(best, p)
		}
	}
	slices.Sort(best)
	return best
}

// AtlasNamed reports whether name names the atlas JSON file at p, a slash-separated path from the project root, as NearestAtlases matches it before choosing the nearest: p without its extension is name, cleaned and taken from the project root, or p's base name without its extension is name.
func AtlasNamed(p, name string) bool {
	if name == "" {
		return false
	}
	stem := strings.TrimSuffix(p, path.Ext(p))
	return stem == path.Join(".", name) || path.Base(stem) == name
}

// dirDistance returns how many directories lie between a and b: the steps up from a to the directory they share, plus the steps down from it to b. "." is the root, with no components.
func dirDistance(a, b string) int {
	as, bs := dirParts(a), dirParts(b)
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return len(as) - n + len(bs) - n
}

// dirParts splits a slash-separated directory into its components, with none for ".".
func dirParts(dir string) []string {
	if dir == "." || dir == "" {
		return nil
	}
	return strings.Split(dir, "/")
}

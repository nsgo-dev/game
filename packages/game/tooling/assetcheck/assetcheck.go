// Package assetcheck checks a game's scene files, Tiled and LDtk maps and sprite atlases, and the references between them, and reports each problem with the file and line that holds it. The game check command runs it, so a broken reference fails in CI instead of when a player reaches the level.
//
// Check walks a directory and reads:
//
//   - scenes, files named *.scene.yaml or *.scene.yml, parsed and validated by sceneformat, whose tilemap sources, sprite and tileset atlases and materials must exist and load, whose glTF models (.gltf or .glb) must exist and load with gltf.LoadFS and hold the node or mesh an object names, whose sprite frames and tile IDs must be in their atlases, which must resolve with sceneformat.Resolve when they instance or inherit other scenes, and whose connections must name objects of the scene, counting the objects the scenes it instances and inherits bring, and, given the game's component types (Options.Components), events and handlers those objects' components declare;
//   - 3D world files named *.gridmap.yaml/.yml, *.terrain.yaml/.yml and *.foliage.yaml/.yml, parsed and validated by world3d, whose referenced mesh libraries (and their tile names for .gltf libraries), heightmaps, splat maps, layer textures, terrain definitions, foliage meshes, materials and density maps must exist and decode;
//   - Tiled maps (.tmj) and tilesets (.tsj), whose external tilesets must be JSON tilesets (.tsj or .json) that exist, whose tileset and image-layer images must exist, and which must load with tilemap.LoadTiled;
//   - LDtk projects (.ldtk), whose tileset images and level backgrounds must exist, whose levels must not be saved in separate files, which tilemap.LoadLDtkAll does not read, and which must load with tilemap.LoadLDtkAll;
//   - sprite atlases, JSON files with top-level frames and meta objects as TexturePacker and Aseprite write them, whose meta.image must exist, whose frames must lie inside it, and which must load with sprite.LoadAtlas;
//   - every other reference refs.Scan finds, which must name a file that exists, or a folder for game.yaml's asset inputs that are folders: the scene a scene inherits or an object instances, references in scenes not named *.scene.yaml that a reference names as a scene, a material's texture, the paths game.yaml autoloads and builds assets from, and the rest; what a tool writes, such as an asset target's out, need not exist yet. game.yaml must parse, and so must the project indexes refs reads, with no uid shared by two files of one project.
//
// A reference to a file that is gone says where the file went when the project index (refs.IndexFileName) finds it again (refs.Project.Moved), and that game index updates the reference.
//
// A file a map or tileset names is resolved from that file's directory, as Tiled and LDtk write them. A scene's tilemap source and material paths are resolved from its project root, the nearest directory above the scene that holds game.yaml, nsgo.yaml or go.mod, or the checked directory when none does. A scene names an atlas by name, as a sceneformat.AssetResolver resolves it: the name is the path of an atlas JSON file without its .json extension, either from the project root or as the file's base name, and when several atlases in the project match, the one nearest the scene, counting the directories up from the scene and down to the atlas, wins. A JSON file of that name that does not parse, or is not an atlas, is reported when a scene names it. PNG, JPEG and GIF images must also decode; other images only have to exist.
//
// Directories whose names start with "." or "_", and those named testdata, are skipped, as the go command skips them.
package assetcheck

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // decode GIF images' headers
	_ "image/jpeg" // decode JPEG images' headers
	_ "image/png"  // decode PNG images' headers
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/refs"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// Problem is one thing wrong with a file Check read.
type Problem struct {
	// File is the slash-separated path of the file at fault, relative to the checked directory.
	File string
	// Line is the 1-based line of the file that holds the problem, or 0 when the problem is about the whole file, such as a loader error that names no place in it.
	Line int
	// Message says what is wrong.
	Message string
}

// String formats p as "file:line: message", or "file: message" when it has no line, as compilers and go vet print theirs.
func (p Problem) String() string {
	if p.Line > 0 {
		return p.File + ":" + strconv.Itoa(p.Line) + ": " + p.Message
	}
	return p.File + ": " + p.Message
}

// Report is what Check found.
type Report struct {
	// Scenes, Maps, Tilesets and Atlases count the files of each kind that were checked: scene files, Tiled and LDtk maps, Tiled tilesets, and sprite atlases.
	Scenes, Maps, Tilesets, Atlases int
	// GridMaps, Terrains and Foliage count the 3D world YAML files that were checked: *.gridmap.yaml, *.terrain.yaml and *.foliage.yaml.
	GridMaps, Terrains, Foliage int
	// Problems are the problems found, sorted by file, then line, then message.
	Problems []Problem
}

// OK reports whether Check found no problems.
func (r *Report) OK() bool { return len(r.Problems) == 0 }

// Options configures CheckWith.
type Options struct {
	// Components, when non-nil, are the game's component types, whose events and handlers a scene's connections name: every object's components are checked against them, as sceneformat.ParseWith checks them, and so is every connection, so a connection naming an event or handler its object does not declare, or a handler argument it does not take, is a Problem at the line that names it. In a scene that instances or inherits other scenes, the components its overrides write and every connection of the resolved scene are checked too, and an override that breaks a connection the other scenes bring is a Problem at the override; a fault a scene brings unchanged from another file is reported on that file only. Nil checks a connection's objects only, since the command line cannot know the types a game registers in Go.
	Components *sceneformat.ComponentRegistry
}

// Check is CheckWith without Options: it checks that every connection names objects that exist, but not their events and handlers.
func Check(fsys fs.FS) (*Report, error) {
	return CheckWith(fsys, Options{})
}

// CheckWith walks fsys, such as os.DirFS of a game's directory, checks every scene, map, tileset and atlas in it as the package comment describes, and returns what it found. It returns an error only when fsys's root cannot be read; a file it cannot read or parse is a Problem.
func CheckWith(fsys fs.FS, opts Options) (*Report, error) {
	c := &checker{
		fsys:       fsys,
		components: opts.Components,
		atlases:    make(map[string]*atlasInfo),
		checked:    make(map[string]bool),
		broken:     make(map[string]bool),
		images:     make(map[string]*imageInfo),
		materials:  make(map[string]bool),
		meshLibs:   make(map[string]*meshLibInfo),
		models:     make(map[string]*gltf.Document),
		roots:      make(map[string]string),
	}
	var scenes, gridMaps, terrains, foliages, tiledMaps, tilesets, ldtks, jsons []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			c.problem(p, 0, "%v", unwrapPathError(err))
			return nil
		}
		if d.IsDir() {
			if p != "." && skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		switch {
		case strings.HasSuffix(name, ".scene.yaml"), strings.HasSuffix(name, ".scene.yml"):
			scenes = append(scenes, p)
		case strings.HasSuffix(name, ".gridmap.yaml"), strings.HasSuffix(name, ".gridmap.yml"):
			gridMaps = append(gridMaps, p)
		case strings.HasSuffix(name, ".terrain.yaml"), strings.HasSuffix(name, ".terrain.yml"):
			terrains = append(terrains, p)
		case strings.HasSuffix(name, ".foliage.yaml"), strings.HasSuffix(name, ".foliage.yml"):
			foliages = append(foliages, p)
		case strings.HasSuffix(name, ".tmj"):
			tiledMaps = append(tiledMaps, p)
		case strings.HasSuffix(name, ".tsj"):
			tilesets = append(tilesets, p)
		case strings.HasSuffix(name, ".ldtk"):
			ldtks = append(ldtks, p)
		case strings.HasSuffix(name, ".json"):
			jsons = append(jsons, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("assetcheck: %w", err)
	}
	if c.refs, err = refs.Scan(fsys); err != nil {
		return nil, fmt.Errorf("assetcheck: %w", err)
	}

	// Atlases first, so that scenes find their frames.
	for _, p := range jsons {
		c.sniffAtlas(p)
	}
	for _, p := range c.atlasPaths {
		c.checkAtlas(c.atlases[p])
	}
	for _, p := range tilesets {
		c.checkTiledTileset(p)
	}
	for _, p := range tiledMaps {
		c.checkTiledMap(p)
	}
	for _, p := range ldtks {
		c.checkLDtk(p)
	}
	for _, p := range scenes {
		c.checkScene(p)
	}
	for _, p := range gridMaps {
		c.checkGridMap(p)
	}
	for _, p := range terrains {
		c.checkTerrain(p)
	}
	for _, p := range foliages {
		c.checkFoliage(p)
	}
	c.checkRefs()
	return c.finish(), nil
}

// skipDir reports whether the walk skips a directory called name, as the go command skips it.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata"
}

// checker holds what one Check has read so far.
type checker struct {
	fsys   fs.FS
	report Report
	// atlasPaths are the atlas files found, in walk order, and atlases describes each.
	atlasPaths []string
	atlases    map[string]*atlasInfo
	// nonAtlases are the other JSON files found, in walk order, which matter only when a scene names one as an atlas.
	nonAtlases []nonAtlas
	// checked marks the map and tileset files already checked, which a reference to them does not check again.
	checked map[string]bool
	// broken marks the files with a problem, whose loaders are not run again to report the same fault in other words.
	broken map[string]bool
	// images caches what each referenced image turned out to be.
	images map[string]*imageInfo
	// materials marks the material files already checked.
	materials map[string]bool
	// meshLibs caches parsed glTF 2.0 mesh libraries referenced by *.gridmap.yaml files.
	meshLibs map[string]*meshLibInfo
	// models holds each glTF model loaded so far, nil for one that does not load.
	models map[string]*gltf.Document
	// roots caches the project root of each directory.
	roots map[string]string
	// refs is what refs.Scan found: every reference between the files, and the project indexes that say where a moved file went.
	refs *refs.Project
	// components are Options.Components.
	components *sceneformat.ComponentRegistry
}

// problem records a problem in file at line (0 for none).
func (c *checker) problem(file string, line int, format string, args ...any) {
	c.broken[file] = true
	c.report.Problems = append(c.report.Problems, Problem{File: file, Line: line, Message: fmt.Sprintf(format, args...)})
}

// finish sorts the problems, drops repeats and returns the report.
func (c *checker) finish() *Report {
	r := &c.report
	slices.SortFunc(r.Problems, func(a, b Problem) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Message, b.Message))
	})
	r.Problems = slices.Compact(r.Problems)
	return r
}

// readFile reads p, recording a problem and returning false when it cannot.
func (c *checker) readFile(p string) ([]byte, bool) {
	data, err := fs.ReadFile(c.fsys, p)
	if err != nil {
		c.problem(p, 0, "%v", unwrapPathError(err))
		return nil, false
	}
	return data, true
}

// unwrapPathError drops the operation and path an fs.PathError adds, which the problem's file already says.
func unwrapPathError(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	return err
}

// resolve joins ref, a path written in a file, to dir, the directory it is relative to, and returns the slash-separated path in the checked directory, or an error saying why ref cannot name a file there.
func resolve(dir, ref string) (string, error) {
	switch {
	case strings.ContainsRune(ref, '\\'):
		return "", fmt.Errorf("%q uses backslashes; write paths with forward slashes", ref)
	case path.IsAbs(ref) || hasVolume(ref):
		return "", fmt.Errorf("%q is an absolute path; write it relative to the file", ref)
	case hasScheme(ref):
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

// hasScheme reports whether ref starts with a URI scheme of two or more letters, such as asset: or https:.
func hasScheme(ref string) bool {
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

// errNotFound is why a referenced file that does not exist cannot be read.
var errNotFound = errors.New("not found")

// fileMissing returns why p cannot be read as a file, or nil when it can.
func (c *checker) fileMissing(p string) error {
	info, err := fs.Stat(c.fsys, p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errNotFound
	case err != nil:
		return unwrapPathError(err)
	case info.IsDir():
		return errors.New("is a directory")
	}
	return nil
}

// fileRef checks that ref, written at line of file and relative to dir, names a file that exists. what describes the reference in messages, such as "external tileset". It returns the resolved path and whether it is usable.
func (c *checker) fileRef(file string, line int, dir, ref, what string) (string, bool) {
	p, err := resolve(dir, ref)
	if err != nil {
		c.problem(file, line, "%s %v", what, err)
		return "", false
	}
	if err := c.fileMissing(p); err != nil {
		c.problem(file, line, "%s %q: %v%s", what, ref, err, c.movedHint(dir, p, err))
		return "", false
	}
	return p, true
}

// imageInfo is what a referenced image file turned out to be.
type imageInfo struct {
	// err is why the image cannot be used, or nil.
	err error
	// width and height are the image's size in pixels, when it decoded.
	width, height int
	sized         bool
}

// imageRef checks that ref, written at line of file and relative to dir, names an image that exists and, for the formats the standard library decodes, decodes. It returns the image, or nil after recording a problem.
func (c *checker) imageRef(file string, line int, dir, ref, what string) *imageInfo {
	p, err := resolve(dir, ref)
	if err != nil {
		c.problem(file, line, "%s %v", what, err)
		return nil
	}
	img := c.image(p)
	if img.err != nil {
		c.problem(file, line, "%s %q: %v%s", what, ref, img.err, c.movedHint(dir, p, img.err))
		return nil
	}
	return img
}

// image reads the image at p once and returns what it is.
func (c *checker) image(p string) *imageInfo {
	if img, ok := c.images[p]; ok {
		return img
	}
	img := &imageInfo{}
	c.images[p] = img
	if img.err = c.fileMissing(p); img.err != nil {
		return img
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif":
	default:
		return img
	}
	f, err := c.fsys.Open(p)
	if err != nil {
		img.err = unwrapPathError(err)
		return img
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		img.err = fmt.Errorf("does not decode: %w", err)
		return img
	}
	img.width, img.height, img.sized = cfg.Width, cfg.Height, true
	return img
}

// projectRoot returns the project root of the files in dir: the nearest directory at or above it that holds game.yaml, nsgo.yaml or go.mod, or "." when none does, as refs.Project.Root finds it.
func (c *checker) projectRoot(dir string) string {
	if r, ok := c.roots[dir]; ok {
		return r
	}
	root := "."
	if dir != "." {
		if c.fileMissing(path.Join(dir, project.FileName)) == nil || c.fileMissing(path.Join(dir, project.NSGoFileName)) == nil || c.fileMissing(path.Join(dir, "go.mod")) == nil {
			root = dir
		} else {
			root = c.projectRoot(path.Dir(dir))
		}
	}
	c.roots[dir] = root
	return root
}

// movedHint returns, for a reference written relative to dir to the file p that could not be read because of err, where the project index says the file went, as "; it moved to ..." in the reference's own form, or "" when err is not that the file is gone or the index does not know where it went.
func (c *checker) movedHint(dir, p string, err error) string {
	if !errors.Is(err, errNotFound) || c.refs == nil {
		return ""
	}
	to, ok := c.refs.Moved(p)
	if !ok {
		return ""
	}
	return fmt.Sprintf("; it moved to %q; run game index to update the reference", refs.RelPath(dir, to))
}

// checkRefs reports what refs.Scan found wrong, a game.yaml or an index that does not parse or a shared uid, and checks every reference refs.Scan found that the rest of the check did not already report at its file and line: what it names must exist, as a folder when the reference names one, unless a tool writes it.
func (c *checker) checkRefs() {
	type fileLine struct {
		file string
		line int
	}
	reported := make(map[fileLine]bool, len(c.report.Problems))
	for _, p := range c.report.Problems {
		reported[fileLine{p.File, p.Line}] = true
	}
	for _, p := range c.refs.Problems {
		c.problem(p.File, p.Line, "%s", p.Message)
	}
	for _, r := range c.refs.Refs {
		if r.Target == "" || r.Output || reported[fileLine{r.File, r.Line}] {
			continue
		}
		missing := c.fileMissing
		if r.Dir {
			missing = c.dirMissing
		}
		if err := missing(r.Target); err != nil {
			hint := ""
			if h := c.refs.MovedHint(r); h != "" && errors.Is(err, errNotFound) {
				hint = "; " + h
			}
			c.problem(r.File, r.Line, "%s %q: %v%s", r.Field, r.Value, err, hint)
		}
	}
}

// dirMissing returns why p cannot be read as a folder, or nil when it can.
func (c *checker) dirMissing(p string) error {
	info, err := fs.Stat(c.fsys, p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errNotFound
	case err != nil:
		return unwrapPathError(err)
	case !info.IsDir():
		return errors.New("is not a directory")
	}
	return nil
}

// plural returns "n one" when n is 1 and "n many" otherwise.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// Summary returns a line counting the files checked and the problems found, such as "1 scene, 2 maps, 0 tilesets, 1 atlas: 3 problems".
func (r *Report) Summary() string {
	counts := plural(r.Scenes, "scene", "scenes") + ", " + plural(r.Maps, "map", "maps") + ", " + plural(r.Tilesets, "tileset", "tilesets") + ", " + plural(r.Atlases, "atlas", "atlases")
	if r.GridMaps > 0 {
		counts += ", " + plural(r.GridMaps, "grid map", "grid maps")
	}
	if r.Terrains > 0 {
		counts += ", " + plural(r.Terrains, "terrain", "terrains")
	}
	if r.Foliage > 0 {
		counts += ", " + plural(r.Foliage, "foliage file", "foliage files")
	}
	if r.OK() {
		return counts + ": no problems"
	}
	return counts + ": " + plural(len(r.Problems), "problem", "problems")
}

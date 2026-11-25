// Command gameschema writes the JSON Schema of every YAML format the game packages define, for editors and AI tools to validate and complete the files a game's author writes, and with -check verifies that the committed schemas match the Go types.
//
// Run it from packages/game: with no flags it rewrites apps/website/public/schemas, which the website publishes at https://game.nsgo.dev/schemas/; CI runs it with -check.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/cutscene"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/navbake3d"
	"github.com/nsgo-dev/game/packages/game/playtest"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
	"github.com/nsgo-dev/game/packages/game/voxel"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "gameschema: %v\n", err)
		os.Exit(1)
	}
}

// format is one YAML format with a published schema.
type format struct {
	// url is where the schema is published; its last element is the schema's file name.
	url   string
	title string
	// typ is the Go type a loader decodes the format into.
	typ reflect.Type
	// pkg is the directory of typ's package relative to packages/game, whose doc comments describe the schema's fields.
	pkg string
	// version, when non-zero, is the only value the schema allows for the root's version property.
	version int
	// fieldName maps a struct field to its key, or skips it; nil uses jsonschema.YAMLField.
	fieldName func(reflect.StructField) jsonschema.Field
	// types overrides the schemas of Go types; nil uses durationTypes.
	types map[reflect.Type]*jsonschema.Schema
}

// sceneV2Fields names the Go fields of the scene document types that scene format version 2 added; the version 1 schema leaves them out, since Validate refuses them in a version 1 document.
var sceneV2Fields = map[string]bool{"Inherits": true, "LayerOverrides": true, "Overrides": true, "Scene": true, "Model": true, "ModelNode": true, "ModelMesh": true, "Level": true, "TilesetFile": true, "TileLayers": true}

// sceneV3Fields names the Go fields of the scene document types that scene format version 3 added; the version 1 and version 2 schemas leave them out, since Validate refuses them in older versions.
var sceneV3Fields = map[string]bool{"ParallaxX": true, "ParallaxY": true, "RepeatX": true, "RepeatY": true, "RepeatWidth": true, "RepeatHeight": true}

// sceneV1Field maps a scene document field as jsonschema.YAMLField does, skipping the fields versions 2 and 3 added.
func sceneV1Field(f reflect.StructField) jsonschema.Field {
	if sceneV2Fields[f.Name] || sceneV3Fields[f.Name] {
		return jsonschema.Field{Skip: true}
	}
	return jsonschema.YAMLField(f)
}

// sceneV2Field maps a scene document field as jsonschema.YAMLField does, skipping the fields version 3 added.
func sceneV2Field(f reflect.StructField) jsonschema.Field {
	if sceneV3Fields[f.Name] {
		return jsonschema.Field{Skip: true}
	}
	return jsonschema.YAMLField(f)
}

// cutsceneV2Fields names the Go fields of Cutscene that version 2 added; the version 1 schema leaves them out, since Validate refuses them in a version 1 document.
var cutsceneV2Fields = map[string]bool{"Kind": true, "Scene": true, "Length": true, "Tracks": true}

// cutsceneV1Field maps a Cutscene field as jsonschema.YAMLField does, skipping the timeline fields version 2 added.
func cutsceneV1Field(f reflect.StructField) jsonschema.Field {
	if cutsceneV2Fields[f.Name] {
		return jsonschema.Field{Skip: true}
	}
	return jsonschema.YAMLField(f)
}

// formats lists every format gameschema writes a schema for. A format that gets a typed YAML loader adds its entry here, and a newer version of a format adds a new entry beside earlier ones, since files that name an old schema keep naming it.
var formats = []format{
	{
		url:       sceneformat.SchemaURL,
		title:     "NSGo game scene (version 1)",
		typ:       reflect.TypeFor[sceneformat.SceneDoc](),
		pkg:       "sceneformat",
		version:   1,
		fieldName: sceneV1Field,
	},
	{
		url:       sceneformat.SchemaURLV2,
		title:     "NSGo game scene (version 2)",
		typ:       reflect.TypeFor[sceneformat.SceneDoc](),
		pkg:       "sceneformat",
		version:   2,
		fieldName: sceneV2Field,
	},
	{
		url:     sceneformat.SchemaURLV3,
		title:   "NSGo game scene (version 3)",
		typ:     reflect.TypeFor[sceneformat.SceneDoc](),
		pkg:     "sceneformat",
		version: 3,
	},
	{
		url:     tilemap.TilesetSchemaURL,
		title:   "NSGo game tileset (version 1)",
		typ:     reflect.TypeFor[tilemap.TilesetDoc](),
		pkg:     "tilemap",
		version: tilemap.TilesetFormatVersion,
		// ParseTileset decodes durations straight into time.Duration, which go.yaml.in/yaml/v3 reads only from a string.
		types: durationStringTypes,
	},
	{
		url:     importsettings.SchemaURL,
		title:   "NSGo game import settings (version 1)",
		typ:     reflect.TypeFor[importsettings.File](),
		pkg:     "tooling/importsettings",
		version: importsettings.CurrentVersion,
	},
	{
		url:     voxel.SchemaURL,
		title:   "NSGo game voxel world (version 1)",
		typ:     reflect.TypeFor[voxel.WorldDoc](),
		pkg:     "voxel",
		version: voxel.FormatVersion,
	},
	{
		url:     l10n.SchemaURL,
		title:   "NSGo game localization string table (version 1)",
		typ:     reflect.TypeFor[l10n.StringTable](),
		pkg:     "l10n",
		version: l10n.StringTableVersion,
	},
	{
		url:     world3d.TerrainSchemaURL,
		title:   "NSGo game terrain definition (version 1)",
		typ:     reflect.TypeFor[world3d.TerrainDoc](),
		pkg:     "world3d",
		version: world3d.TerrainFormatVersion,
	},
	{
		url:     world3d.GridMapSchemaURL,
		title:   "NSGo game 3D grid map (version 1)",
		typ:     reflect.TypeFor[world3d.GridMapDoc](),
		pkg:     "world3d",
		version: world3d.GridMapFormatVersion,
	},
	{
		url:     world3d.FoliageSchemaURL,
		title:   "NSGo game 3D foliage layers (version 1)",
		typ:     reflect.TypeFor[world3d.FoliageDoc](),
		pkg:     "world3d",
		version: world3d.FoliageFormatVersion,
	},
	{
		url:     navbake3d.SchemaURL,
		title:   "NSGo game navigation mesh (version 1)",
		typ:     reflect.TypeFor[navbake3d.File](),
		pkg:     "navbake3d",
		version: navbake3d.FormatVersion,
	},
	{
		url:     project.SchemaURL,
		title:   "NSGo game project (version 1)",
		typ:     reflect.TypeFor[project.File](),
		pkg:     "project",
		version: project.CurrentVersion,
	},
	{
		url:     ai.BTSchemaURL,
		title:   "NSGo game behavior tree (version 1)",
		typ:     reflect.TypeFor[ai.BehaviorTreeDoc](),
		pkg:     "ai",
		version: ai.BTFormatVersion,
	},
	{
		url:       cutscene.SchemaURL,
		title:     "NSGo game cutscene (version 1)",
		typ:       reflect.TypeFor[cutscene.Cutscene](),
		pkg:       "cutscene",
		version:   cutscene.MinVersion,
		fieldName: cutsceneV1Field,
	},
	{
		url:     cutscene.SchemaURLV2,
		title:   "NSGo game cutscene (version 2)",
		typ:     reflect.TypeFor[cutscene.Cutscene](),
		pkg:     "cutscene",
		version: cutscene.CurrentVersion,
	},
	{
		url:     playtest.SchemaURL,
		title:   "NSGo game playtest (version 1)",
		typ:     reflect.TypeFor[playtest.Config](),
		pkg:     "playtest",
		version: playtest.CurrentVersion,
	},
}

// durationTypes maps duration types to their YAML schema forms: time.Duration accepts a Go duration string or integer nanoseconds; cutscene.Duration accepts only Go duration strings such as "1.5s".
var durationTypes = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[time.Duration]():     {Types: []jsonschema.Type{jsonschema.TypeString, jsonschema.TypeInteger}},
	reflect.TypeFor[cutscene.Duration](): {Types: []jsonschema.Type{jsonschema.TypeString}},
}

// durationStringTypes allows a time.Duration written only as a Go duration string such as "250ms", for formats whose loader decodes it with no integer fallback.
var durationStringTypes = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[time.Duration](): {Types: []jsonschema.Type{jsonschema.TypeString}},
}

// schemaFile is one generated schema and the file name it is written under.
type schemaFile struct {
	name   string
	schema *jsonschema.Schema
	data   []byte
}

// generate builds every format's schema, reading descriptions from the package sources under root (packages/game).
func generate(root string) ([]schemaFile, error) {
	files := make([]schemaFile, 0, len(formats))
	for _, f := range formats {
		docs, err := jsonschema.PackageDocs(filepath.Join(root, filepath.FromSlash(f.pkg)))
		if err != nil {
			return nil, err
		}
		types := f.types
		if types == nil {
			types = durationTypes
		}
		s, err := jsonschema.Reflect(f.typ, jsonschema.Options{ID: f.url, Title: f.title, Docs: docs, FieldName: f.fieldName, Types: types})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.url, err)
		}
		if f.version != 0 {
			v := s.Property("version")
			if v == nil {
				return nil, fmt.Errorf("%s: no version property", f.url)
			}
			// The one allowed value lies within the Go type's range, so the type's bounds would only repeat it, as Reflect leaves them out for a const tag.
			v.Const = int64(f.version)
			v.Minimum, v.Maximum = nil, nil
		}
		if conn := s.Definitions["ConnectionDoc"]; conn != nil {
			args := conn.Property("args")
			if args == nil {
				return nil, fmt.Errorf("%s: ConnectionDoc has no args property", f.url)
			}
			// Args is a map[string]any, which Reflect allows any value in, but sceneformat.Validate takes only scalars, so an editor flags the rest as the loader does.
			args.AdditionalProperties = &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeBoolean, jsonschema.TypeNumber, jsonschema.TypeString}}
		}
		if f.url == tilemap.TilesetSchemaURL {
			// Properties are map[string]any, which Reflect allows any value in, but tilemap.ValidateTileset takes only scalars, so an editor flags the rest as the loader does.
			scalar := &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeBoolean, jsonschema.TypeNumber, jsonschema.TypeString}}
			props := []*jsonschema.Schema{s.Property("properties")}
			for _, def := range []string{"TileDoc", "TileShapeDoc"} {
				d := s.Definitions[def]
				if d == nil {
					return nil, fmt.Errorf("%s: no %s definition", f.url, def)
				}
				props = append(props, d.Property("properties"))
			}
			for _, p := range props {
				if p == nil {
					return nil, fmt.Errorf("%s: a properties field is missing", f.url)
				}
				p.AdditionalProperties = scalar
			}
		}
		if f.url == cutscene.SchemaURL {
			if !slices.Contains(s.Required, "source") {
				s.Required = append(s.Required, "source")
			}
		}
		if f.url == ai.BTSchemaURL {
			btValSchema := func(desc string) *jsonschema.Schema {
				return &jsonschema.Schema{
					Description: desc,
					Types:       []jsonschema.Type{jsonschema.TypeBoolean, jsonschema.TypeNumber, jsonschema.TypeString, jsonschema.TypeArray},
					Items:       &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeNumber}},
				}
			}
			if params := s.Property("params"); params != nil {
				params.AdditionalProperties = btValSchema("")
			}
			if keyDef := s.Definitions["BTBlackboardKeyDoc"]; keyDef != nil {
				if def := keyDef.Property("default"); def != nil {
					*def = *btValSchema(def.Description)
				}
			}
			if nodeDef := s.Definitions["BTNodeDoc"]; nodeDef != nil {
				if val := nodeDef.Property("value"); val != nil {
					*val = *btValSchema(val.Description)
				}
				if params := nodeDef.Property("params"); params != nil {
					params.AdditionalProperties = btValSchema("")
				}
			}
		}
		if f.url == playtest.SchemaURL {
			if botDef := s.Definitions["BotConfig"]; botDef != nil {
				if params := botDef.Property("params"); params != nil {
					params.AdditionalProperties = &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeBoolean, jsonschema.TypeNumber, jsonschema.TypeString}}
				}
			}
		}
		data, err := s.Marshal()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.url, err)
		}
		files = append(files, schemaFile{name: path.Base(f.url), schema: s, data: data})
	}
	return files, nil
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("gameschema", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "the packages/game directory, whose package sources describe the schemas")
	out := fs.String("out", filepath.Join("..", "..", "apps", "website", "public", "schemas"), "directory the schemas are written to")
	check := fs.Bool("check", false, "verify that -out holds exactly the generated schemas without modifying it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments %q", fs.Args())
	}
	files, err := generate(*root)
	if err != nil {
		return err
	}
	if *check {
		return checkDir(*out, files)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		p := filepath.Join(*out, f.name)
		if err := os.WriteFile(p, f.data, 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, p)
	}
	return nil
}

// checkDir reports every schema in dir that is missing or differs from its generated form, and every .json file in dir that gameschema does not generate.
func checkDir(dir string, files []schemaFile) error {
	var problems []string
	want := make([]string, 0, len(files))
	for _, f := range files {
		want = append(want, f.name)
		got, err := os.ReadFile(filepath.Join(dir, f.name))
		switch {
		case errors.Is(err, os.ErrNotExist):
			problems = append(problems, f.name+" is missing")
		case err != nil:
			return err
		case !bytes.Equal(got, f.data):
			problems = append(problems, f.name+" differs from the Go types")
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && !slices.Contains(want, e.Name()) {
			problems = append(problems, e.Name()+" is not generated by gameschema; remove it")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("schemas in %s are out of date (run go run ./cmd/gameschema from packages/game):\n%s", dir, strings.Join(problems, "\n"))
	}
	return nil
}

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

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
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
}

// sceneV2Fields names the Go fields of the scene document types that scene format version 2 added; the version 1 schema leaves them out, since Validate refuses them in a version 1 document.
var sceneV2Fields = map[string]bool{"Inherits": true, "LayerOverrides": true, "Overrides": true, "Scene": true}

// sceneV1Field maps a scene document field as jsonschema.YAMLField does, skipping the fields version 2 added.
func sceneV1Field(f reflect.StructField) jsonschema.Field {
	if sceneV2Fields[f.Name] {
		return jsonschema.Field{Skip: true}
	}
	return jsonschema.YAMLField(f)
}

// formats lists every format gameschema writes a schema for. A format that gets a typed YAML loader adds its entry here, and a version 2 of a format adds a second entry beside version 1, since files that name the old schema keep naming it.
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
		url:     sceneformat.SchemaURLV2,
		title:   "NSGo game scene (version 2)",
		typ:     reflect.TypeFor[sceneformat.SceneDoc](),
		pkg:     "sceneformat",
		version: 2,
	},
	{
		url:     importsettings.SchemaURL,
		title:   "NSGo game import settings (version 1)",
		typ:     reflect.TypeFor[importsettings.File](),
		pkg:     "tooling/importsettings",
		version: importsettings.CurrentVersion,
	},
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
		s, err := jsonschema.Reflect(f.typ, jsonschema.Options{ID: f.url, Title: f.title, Docs: docs, FieldName: f.fieldName})
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

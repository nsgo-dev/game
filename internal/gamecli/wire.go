package gamecli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
)

// wireCommand generates deterministic, zero-allocation Go and TypeScript codecs from *.wire.yaml schemas, and with -check verifies that committed generated files match their schemas.
var wireCommand = &command{
	name:    "wire",
	args:    "[schema.wire.yaml ...]",
	summary: "Generate Go and TypeScript codecs from *.wire.yaml schemas, or check that the committed ones are current",
	define:  defineWire,
	complete: map[string]completion{
		"in":   filePaths,
		"out":  filePaths,
		"lang": oneOf("go", "ts"),
		"":     filePaths,
	},
}

// wireLoadedSchema is one parsed *.wire.yaml file.
type wireLoadedSchema struct {
	path   string
	name   string
	schema *wire.Schema
}

// wireGeneratedFile is one generated codec file and the path it belongs at.
type wireGeneratedFile struct {
	path string
	data []byte
}

func defineWire(a *app, fs *flag.FlagSet) func() error {
	inPath := fs.String("in", ".", "input *.wire.yaml `file` or directory containing *.wire.yaml files")
	outPath := fs.String("out", "", "output `directory` (or .go/.ts file for a single input); defaults to the input directory")
	pkgName := fs.String("pkg", "", "override the Go package `name` (defaults to each schema's package field)")
	lang := fs.String("lang", "go", "target `language` for code generation: go or ts")
	self := fs.Bool("self", false, "omit the pkg/online/netplay/wire runtime import (used when generating Go inside pkg/online/netplay/wire)")
	check := fs.Bool("check", false, "verify that generated files in -out are up to date without writing")
	return func() error {
		if *lang != "go" && *lang != "ts" {
			return usagef("unsupported -lang %q (supported: go, ts)", *lang)
		}

		inputPaths, defaultOutDir, err := wireCollectInputs(*inPath, fs.Args())
		if err != nil {
			return err
		}
		if len(inputPaths) == 0 {
			return fmt.Errorf("no *.wire.yaml files found in %s", *inPath)
		}

		out := *outPath
		if out == "" {
			out = defaultOutDir
		}
		singleFileOut := (*lang == "go" && strings.HasSuffix(out, ".go")) || (*lang == "ts" && strings.HasSuffix(out, ".ts"))
		if singleFileOut && len(inputPaths) != 1 {
			return usagef("-out %q targets a single file, but %d input schemas were given", out, len(inputPaths))
		}

		loaded, err := wireLoadSchemas(inputPaths)
		if err != nil {
			return err
		}

		// Build full schema list for cross-file type resolution, keeping input schemas in sorted order.
		allSchemas := make([]*wire.Schema, 0, len(loaded.all))
		for _, ls := range loaded.all {
			allSchemas = append(allSchemas, ls.schema)
		}
		if err := wire.ValidateAll(allSchemas); err != nil {
			return err
		}

		var files []wireGeneratedFile
		switch *lang {
		case "go":
			files = make([]wireGeneratedFile, 0, len(loaded.targets))
			for _, target := range loaded.targets {
				var extras []*wire.Schema
				for _, ls := range loaded.all {
					if ls.path != target.path {
						extras = append(extras, ls.schema)
					}
				}
				code, err := wire.GenerateGo(target.schema, wire.GenerateOptions{
					Package:      *pkgName,
					SourceName:   target.name,
					Self:         *self,
					ExtraSchemas: extras,
				})
				if err != nil {
					return fmt.Errorf("%s: %w", target.path, err)
				}
				var dstPath string
				if singleFileOut {
					dstPath = out
				} else {
					dstPath = filepath.Join(out, wireOutputFileName(target.name))
				}
				files = append(files, wireGeneratedFile{path: dstPath, data: code})
			}
		case "ts":
			files = make([]wireGeneratedFile, 0, len(loaded.targets)+2)
			if !singleFileOut {
				files = append(files, wireGeneratedFile{
					path: filepath.Join(out, "runtime.ts"),
					data: wire.GenerateTSRuntime(),
				})
			}
			targetNames := make([]string, 0, len(loaded.targets))
			for _, target := range loaded.targets {
				var extras []wire.NamedSchema
				for _, ls := range loaded.all {
					if ls.path != target.path {
						extras = append(extras, wire.NamedSchema{Name: ls.name, Schema: ls.schema})
					}
				}
				code, err := wire.GenerateTS(target.schema, wire.TSGenerateOptions{
					SourceName:   target.name,
					EmbedRuntime: singleFileOut,
					ExtraSchemas: extras,
				})
				if err != nil {
					return fmt.Errorf("%s: %w", target.path, err)
				}
				var dstPath string
				if singleFileOut {
					dstPath = out
				} else {
					dstPath = filepath.Join(out, wire.TSOutputFileName(target.name))
				}
				files = append(files, wireGeneratedFile{path: dstPath, data: code})
				targetNames = append(targetNames, target.name)
			}
			if !singleFileOut {
				files = append(files, wireGeneratedFile{
					path: filepath.Join(out, "index.ts"),
					data: wire.GenerateTSIndex(targetNames),
				})
			}
		}

		if *check {
			return wireCheckFiles(files)
		}

		for _, f := range files {
			if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, f.path)
		}
		return nil
	}
}

func wireCollectInputs(inFlag string, positional []string) ([]string, string, error) {
	if len(positional) > 0 {
		paths := make([]string, 0, len(positional))
		for _, p := range positional {
			abs, err := filepath.Abs(p)
			if err != nil {
				return nil, "", err
			}
			paths = append(paths, abs)
		}
		slices.Sort(paths)
		return paths, filepath.Dir(paths[0]), nil
	}

	info, err := os.Stat(inFlag)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		abs, err := filepath.Abs(inFlag)
		if err != nil {
			return nil, "", err
		}
		return []string{abs}, filepath.Dir(abs), nil
	}
	entries, err := os.ReadDir(inFlag)
	if err != nil {
		return nil, "", err
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".wire.yaml") {
			abs, err := filepath.Abs(filepath.Join(inFlag, e.Name()))
			if err != nil {
				return nil, "", err
			}
			paths = append(paths, abs)
		}
	}
	slices.Sort(paths)
	return paths, inFlag, nil
}

type wireSchemaSet struct {
	targets []wireLoadedSchema
	all     []wireLoadedSchema
}

func wireLoadSchemas(paths []string) (*wireSchemaSet, error) {
	byPath := make(map[string]wireLoadedSchema)
	var targets []wireLoadedSchema
	var queue []string

	for _, p := range paths {
		s, err := wire.LoadFile(p)
		if err != nil {
			return nil, err
		}
		ls := wireLoadedSchema{path: p, name: filepath.Base(p), schema: s}
		byPath[p] = ls
		targets = append(targets, ls)
		for _, imp := range s.Imports {
			queue = append(queue, filepath.Join(filepath.Dir(p), filepath.FromSlash(imp)))
		}
	}

	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		if _, ok := byPath[abs]; ok {
			continue
		}
		s, err := wire.LoadFile(abs)
		if err != nil {
			return nil, err
		}
		ls := wireLoadedSchema{path: abs, name: filepath.Base(abs), schema: s}
		byPath[abs] = ls
		for _, imp := range s.Imports {
			queue = append(queue, filepath.Join(filepath.Dir(abs), filepath.FromSlash(imp)))
		}
	}

	all := make([]wireLoadedSchema, 0, len(byPath))
	for _, ls := range byPath {
		all = append(all, ls)
	}
	slices.SortFunc(all, func(a, b wireLoadedSchema) int {
		return strings.Compare(a.path, b.path)
	})
	return &wireSchemaSet{targets: targets, all: all}, nil
}

func wireOutputFileName(schemaBaseName string) string {
	stem := strings.TrimSuffix(schemaBaseName, ".wire.yaml")
	if stem == schemaBaseName {
		stem = strings.TrimSuffix(schemaBaseName, filepath.Ext(schemaBaseName))
	}
	return stem + "_wire.go"
}

func wireCheckFiles(files []wireGeneratedFile) error {
	var problems []string
	for _, f := range files {
		got, err := os.ReadFile(f.path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			problems = append(problems, f.path+" is missing")
		case err != nil:
			return err
		case !bytes.Equal(got, f.data):
			problems = append(problems, f.path+" is out of date")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("generated wire codecs are stale (run go generate ./pkg/online/netplay/wire or game wire):\n%s", strings.Join(problems, "\n"))
	}
	return nil
}

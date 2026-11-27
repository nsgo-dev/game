// Command gamewire generates deterministic, zero-allocation codecs from *.wire.yaml schemas, and with -check verifies that committed generated files match their schemas.
//
// Run it directly or through go generate:
//
//	go run ./cmd/gamewire -in pkg/online/netplay/wire -out pkg/online/netplay/wire -self
//	go run ./cmd/gamewire -in pkg/online/netplay/wire -out pkg/online/netplay/wire -self -check
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/pkg/online/netplay/wire"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "gamewire: %v\n", err)
		os.Exit(1)
	}
}

type loadedSchema struct {
	path   string
	name   string
	schema *wire.Schema
}

type generatedFile struct {
	path string
	data []byte
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("gamewire", flag.ContinueOnError)
	fs.SetOutput(stderr)
	inPath := fs.String("in", ".", "input *.wire.yaml file or directory containing *.wire.yaml files")
	outPath := fs.String("out", "", "output directory (or .go file for a single input); defaults to the input directory")
	pkgName := fs.String("pkg", "", "override Go package name (defaults to each schema's package field)")
	lang := fs.String("lang", "go", "target language for code generation (go)")
	self := fs.Bool("self", false, "omit the pkg/net/wire runtime import (used when generating inside pkg/net/wire)")
	check := fs.Bool("check", false, "verify that generated files in -out are up to date without writing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *lang != "go" {
		return fmt.Errorf("unsupported -lang %q (supported: go)", *lang)
	}

	inputPaths, defaultOutDir, err := collectInputs(*inPath, fs.Args())
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
	singleFileOut := strings.HasSuffix(out, ".go")
	if singleFileOut && len(inputPaths) != 1 {
		return fmt.Errorf("-out %q targets a single .go file, but %d input schemas were given", out, len(inputPaths))
	}

	loaded, err := loadSchemasWithImports(inputPaths)
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

	files := make([]generatedFile, 0, len(loaded.targets))
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
			dstPath = filepath.Join(out, outputFileName(target.name))
		}
		files = append(files, generatedFile{path: dstPath, data: code})
	}

	if *check {
		return checkFiles(files)
	}

	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, f.path)
	}
	return nil
}

func collectInputs(inFlag string, positional []string) ([]string, string, error) {
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

type schemaSet struct {
	targets []loadedSchema
	all     []loadedSchema
}

func loadSchemasWithImports(paths []string) (*schemaSet, error) {
	byPath := make(map[string]loadedSchema)
	var targets []loadedSchema
	var queue []string

	for _, p := range paths {
		s, err := wire.LoadFile(p)
		if err != nil {
			return nil, err
		}
		ls := loadedSchema{path: p, name: filepath.Base(p), schema: s}
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
		ls := loadedSchema{path: abs, name: filepath.Base(abs), schema: s}
		byPath[abs] = ls
		for _, imp := range s.Imports {
			queue = append(queue, filepath.Join(filepath.Dir(abs), filepath.FromSlash(imp)))
		}
	}

	all := make([]loadedSchema, 0, len(byPath))
	for _, ls := range byPath {
		all = append(all, ls)
	}
	slices.SortFunc(all, func(a, b loadedSchema) int {
		return strings.Compare(a.path, b.path)
	})
	return &schemaSet{targets: targets, all: all}, nil
}

func outputFileName(schemaBaseName string) string {
	stem := strings.TrimSuffix(schemaBaseName, ".wire.yaml")
	if stem == schemaBaseName {
		stem = strings.TrimSuffix(schemaBaseName, filepath.Ext(schemaBaseName))
	}
	return stem + "_wire.go"
}

func checkFiles(files []generatedFile) error {
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
		return fmt.Errorf("generated wire codecs are stale (run go generate ./pkg/online/netplay/wire or gamewire):\n%s", strings.Join(problems, "\n"))
	}
	return nil
}

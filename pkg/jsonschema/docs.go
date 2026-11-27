package jsonschema

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PackageDocs reads the doc comments of the Go package in dir (its non-test .go files) into the map Options.Docs takes: each type's comment under its name, and each struct field's under "Type.Field".
//
// A field with no comment of its own takes the comment of the nearest commented field before it in the same struct when that comment names it, as in "X and Y are the world coordinates", so a pair documented together gets the same description. A comment's lines are joined into paragraphs separated by a blank line.
func PackageDocs(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: read package %s: %w", dir, err)
	}
	fset := token.NewFileSet()
	docs := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("jsonschema: parse %s: %w", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts := spec.(*ast.TypeSpec)
				doc := ts.Doc
				if doc == nil && len(gen.Specs) == 1 {
					doc = gen.Doc
				}
				if text := commentText(doc); text != "" {
					docs[ts.Name.Name] = text
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					addFieldDocs(docs, ts.Name.Name, st)
				}
			}
		}
	}
	return docs, nil
}

func addFieldDocs(docs map[string]string, typeName string, st *ast.StructType) {
	last := ""
	for _, field := range st.Fields.List {
		text := commentText(field.Doc)
		if text == "" {
			text = commentText(field.Comment)
		}
		if text != "" {
			last = text
		}
		for _, name := range field.Names {
			switch {
			case text != "":
				docs[typeName+"."+name.Name] = text
			case last != "" && namesField(last, name.Name):
				docs[typeName+"."+name.Name] = last
			}
		}
	}
}

// namesField reports whether doc mentions the field name as a whole word.
func namesField(doc, name string) bool {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	return re.MatchString(doc)
}

// commentText returns a comment's text with the lines of each paragraph joined by spaces and paragraphs separated by a blank line.
func commentText(c *ast.CommentGroup) string {
	if c == nil {
		return ""
	}
	var paras []string
	var cur []string
	for line := range strings.SplitSeq(c.Text(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(cur) > 0 {
				paras = append(paras, strings.Join(cur, " "))
				cur = nil
			}
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		paras = append(paras, strings.Join(cur, " "))
	}
	return strings.Join(paras, "\n\n")
}

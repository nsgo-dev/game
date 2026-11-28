package editormcp_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor/editormcp"
)

// TestEditorToolReferenceMatchesServer keeps both translations' editor tool tables in step with the tools the server registers.
func TestEditorToolReferenceMatchesServer(t *testing.T) {
	f := openFixture(t)
	if err := f.ed.SetMCPToolEnabled(editormcp.ToolGameDeleteFile, true); err != nil {
		t.Fatal(err)
	}
	s := editormcp.New(f.ed, editormcp.Options{})
	t.Cleanup(func() { _ = s.Close() })
	cs := connect(t, s, nil)

	var registered []string
	schemas := map[string]map[string]json.RawMessage{}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		registered = append(registered, tool.Name)
		data, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatal(err)
		}
		schemas[tool.Name] = schema.Properties
	}

	for _, language := range []string{"en", "zh"} {
		t.Run(language, func(t *testing.T) {
			data, err := os.ReadFile("../../../docs/" + language + "/mcp.md")
			if err != nil {
				t.Fatal(err)
			}
			rows, arguments := editorToolReferenceRows(string(data))
			for _, name := range registered {
				if rows[name] != 1 {
					t.Errorf("%s has %d table rows, want exactly 1", name, rows[name])
					continue
				}
				var missing []string
				for property := range schemas[name] {
					if !strings.Contains(arguments[name], "`"+property+"`") {
						missing = append(missing, property)
					}
				}
				slices.Sort(missing)
				if len(missing) > 0 {
					t.Errorf("%s omits schema arguments %v", name, missing)
				}
			}
			for name, count := range rows {
				if count > 1 {
					t.Errorf("%s has %d table rows, want exactly 1", name, count)
				}
			}
		})
	}
}

func editorToolReferenceRows(markdown string) (map[string]int, map[string]string) {
	rows := map[string]int{}
	arguments := map[string]string{}
	inTable := false
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "| Tool | Action |") || strings.HasPrefix(line, "| 工具 | 动作 |") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			continue
		}
		name := strings.TrimSpace(cells[1])
		if len(name) >= 2 && strings.HasPrefix(name, "`") && strings.HasSuffix(name, "`") {
			name = strings.Trim(name, "`")
			rows[name]++
			arguments[name] = cells[3]
		}
	}
	return rows, arguments
}

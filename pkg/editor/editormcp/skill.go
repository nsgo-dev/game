package editormcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/skills"
)

func skillGet(ctx context.Context, c *Call) (*Result, error) {
	var args struct {
		File    string `json:"file"`
		Section string `json:"section"`
	}
	if err := c.Decode(&args); err != nil {
		return nil, fmt.Errorf("skill_get: %w", err)
	}
	file := strings.TrimPrefix(strings.TrimSpace(args.File), "nsgo-game/")
	if file == "" || strings.Contains(file, "..") || strings.HasPrefix(file, "/") {
		return nil, fmt.Errorf("skill_get: invalid file %q", args.File)
	}
	data, err := skills.FS.ReadFile("nsgo-game/" + file)
	if err != nil {
		return nil, fmt.Errorf("skill_get: unknown file %q", args.File)
	}
	section := strings.TrimSpace(args.Section)
	if section == "" {
		return nil, fmt.Errorf("skill_get: section is required")
	}
	lines := strings.Split(string(data), "\n")
	start := -1
	level := 0
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") {
			l := len(trim) - len(strings.TrimLeft(trim, "#"))
			if strings.TrimSpace(strings.TrimLeft(trim, "#")) == section {
				start, level = i, l
				break
			}
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("skill_get: unknown section %q in %s", section, file)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trim, "#") && len(trim)-len(strings.TrimLeft(trim, "#")) <= level {
			end = i
			break
		}
	}
	return &Result{Text: strings.TrimSpace(strings.Join(lines[start:end], "\n")), Structured: map[string]any{"file": file, "section": section}}, nil
}

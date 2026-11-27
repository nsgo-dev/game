package editor

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

const modelContextPromptSeparator = "\n\n---\n\n"

// AgentModelInput is the single canonical rendering of an AgentRequest for every model runner
// (Claude Code CLI, Codex CLI, and the built-in Anthropic Messages API runner).
//
// AlwaysContext carries project-wide Always chips (brief.yaml, AGENTS.md) so runners with a
// system-prompt channel can send them there without duplicating them across turns, while
// TurnContext carries the per-turn context (compacted/seed summary, explicitly attached chips,
// viewport header, and any caller-supplied AssembledContext remainder). Context holds the full
// combined context block for runners without a separate system-prompt channel.
type AgentModelInput struct {
	AlwaysContext       string
	TurnContext         string
	Context             string
	Prompt              string
	TurnPrompt          string
	FullPrompt          string
	Images              []AgentReferenceImage
	HasCompactedSummary bool
}

// RenderAgentModelInput renders req into the exact text blocks and image attachments delivered
// to an external CLI runner or the built-in model runner.
func RenderAgentModelInput(req AgentRequest) AgentModelInput {
	prompt := strings.TrimSpace(req.Prompt)

	var alwaysBlocks []string
	var nonAlwaysBlocks []string
	for _, a := range req.Attachments {
		if a.Chip.Always || a.Chip.Kind == ChipKindAlways {
			if formatted := strings.TrimSpace(a.Formatted()); formatted != "" {
				alwaysBlocks = append(alwaysBlocks, formatted)
			}
			continue
		}
		if a.Chip.Kind == ChipKindViewportImage {
			if h := strings.TrimSpace(a.Header); h != "" {
				nonAlwaysBlocks = append(nonAlwaysBlocks, h)
			}
			continue
		}
		if formatted := strings.TrimSpace(a.Formatted()); formatted != "" {
			nonAlwaysBlocks = append(nonAlwaysBlocks, formatted)
		}
	}

	rawCtx := strings.TrimSpace(req.AssembledContext)
	summarySeed := strings.TrimSpace(req.SummarySeed)

	var alwaysCtx, turnCtx, fullCtx string
	if rawCtx != "" {
		alwaysCtx = joinContextBlocksBounded(alwaysBlocks, maxAssembledContextBytes)
		truncated := strings.Contains(rawCtx, "… (truncated to context budget)")

		rem := rawCtx
		var missingAlways []string
		for _, blk := range alwaysBlocks {
			next, removed := removeContextBlock(rem, blk)
			if removed {
				rem = next
			} else if !truncated {
				missingAlways = append(missingAlways, blk)
			}
		}

		var missingNonAlways []string
		if !truncated {
			for _, blk := range nonAlwaysBlocks {
				if !containsContextBlock(rem, blk) {
					missingNonAlways = append(missingNonAlways, blk)
				}
			}
		}

		if len(missingNonAlways) > 0 {
			turnBlocks := make([]string, 0, 1+len(missingNonAlways))
			if rem != "" {
				turnBlocks = append(turnBlocks, rem)
			}
			turnBlocks = append(turnBlocks, missingNonAlways...)
			turnCtx = joinContextBlocksBounded(turnBlocks, maxAssembledContextBytes)
		} else {
			turnCtx = rem
		}

		if len(missingAlways) > 0 || len(missingNonAlways) > 0 {
			fullBlocks := make([]string, 0, 1+len(missingAlways)+len(missingNonAlways))
			fullBlocks = append(fullBlocks, rawCtx)
			fullBlocks = append(fullBlocks, missingAlways...)
			fullBlocks = append(fullBlocks, missingNonAlways...)
			fullCtx = joinContextBlocksBounded(fullBlocks, maxAssembledContextBytes)
		} else {
			fullCtx = rawCtx
		}
	} else {
		var turnBlocks []string
		if summarySeed != "" {
			turnBlocks = append(turnBlocks, "# Thread summary\n"+summarySeed)
		}
		turnBlocks = append(turnBlocks, nonAlwaysBlocks...)

		alwaysCtx = joinContextBlocksBounded(alwaysBlocks, maxAssembledContextBytes)
		turnCtx = joinContextBlocksBounded(turnBlocks, maxAssembledContextBytes)

		var allBlocks []string
		if summarySeed != "" {
			allBlocks = append(allBlocks, "# Thread summary\n"+summarySeed)
		}
		allBlocks = append(allBlocks, alwaysBlocks...)
		allBlocks = append(allBlocks, nonAlwaysBlocks...)
		fullCtx = joinContextBlocksBounded(allBlocks, maxAssembledContextBytes)
	}

	hasSummary := summarySeed != "" ||
		strings.Contains(turnCtx, "# Compacted earlier requests") ||
		strings.Contains(turnCtx, "# Thread summary")

	images := collectModelInputImages(req)

	return AgentModelInput{
		AlwaysContext:       alwaysCtx,
		TurnContext:         turnCtx,
		Context:             fullCtx,
		Prompt:              prompt,
		TurnPrompt:          joinModelContextAndPrompt(turnCtx, prompt),
		FullPrompt:          joinModelContextAndPrompt(fullCtx, prompt),
		Images:              images,
		HasCompactedSummary: hasSummary,
	}
}

func joinModelContextAndPrompt(contextBlock, prompt string) string {
	switch {
	case contextBlock != "" && prompt != "":
		return contextBlock + modelContextPromptSeparator + prompt
	case contextBlock != "":
		return contextBlock
	default:
		return prompt
	}
}

func collectModelInputImages(req AgentRequest) []AgentReferenceImage {
	var out []AgentReferenceImage
	seenPaths := make(map[string]bool)

	for _, img := range req.Images {
		cp := img.clone()
		if clean := cleanBriefRefPath(cp.Path); clean != "" {
			seenPaths[clean] = true
		}
		out = append(out, cp)
	}

	// Viewport PNG from req.ViewportImage or a ChipKindViewportImage attachment.
	vpData := req.ViewportImage
	vpW := req.ViewportWidth
	vpH := req.ViewportHeight
	vpHeader := ""
	for _, a := range req.Attachments {
		if a.Chip.Kind != ChipKindViewportImage {
			continue
		}
		if len(vpData) == 0 && len(a.ImagePNG) > 0 {
			vpData = a.ImagePNG
		}
		if vpW == 0 {
			vpW = a.ImageW
		}
		if vpH == 0 {
			vpH = a.ImageH
		}
		if vpHeader == "" && a.Header != "" {
			vpHeader = strings.TrimSpace(strings.TrimPrefix(a.Header, "#"))
		}
	}
	if len(vpData) > 0 {
		note := vpHeader
		if note == "" {
			if vpW > 0 && vpH > 0 {
				note = fmt.Sprintf("Viewport image · %d × %d", vpW, vpH)
			} else {
				note = "Viewport image"
			}
		}
		out = append(out, AgentReferenceImage{
			ID:       viewportImageChipID,
			Name:     "viewport.png",
			Format:   AgentImagePNG,
			MIMEType: "image/png",
			Data:     slices.Clone(vpData),
			Width:    vpW,
			Height:   vpH,
			Note:     note,
		})
	}

	// Attached image-asset chips (e.g. @hero.png) carrying decoded PNG bytes.
	for _, a := range req.Attachments {
		if a.Chip.Kind != ChipKindAsset || len(a.ImagePNG) == 0 {
			continue
		}
		cleanRel := cleanBriefRefPath(a.Chip.Target)
		if cleanRel != "" && seenPaths[cleanRel] {
			continue
		}
		if cleanRel != "" {
			seenPaths[cleanRel] = true
		}
		imgPath := ""
		if req.ProjectRoot != "" && cleanRel != "" {
			imgPath = cleanRel
		}
		name := strings.TrimSpace(a.Chip.Name)
		if name == "" && cleanRel != "" {
			name = path.Base(cleanRel)
		}
		if name == "" {
			name = "asset.png"
		}
		note := strings.TrimSpace(strings.TrimPrefix(a.Header, "#"))
		out = append(out, AgentReferenceImage{
			ID:       a.Chip.ID,
			Name:     name,
			Path:     imgPath,
			Format:   AgentImagePNG,
			MIMEType: "image/png",
			Data:     slices.Clone(a.ImagePNG),
			Width:    a.ImageW,
			Height:   a.ImageH,
			Note:     note,
		})
	}

	return out
}

func containsContextBlock(ctx, blk string) bool {
	if ctx == "" || blk == "" {
		return false
	}
	_, found := findContextBlockRange(ctx, blk)
	return found
}

func removeContextBlock(ctx, blk string) (string, bool) {
	start, ok := findContextBlockRange(ctx, blk)
	if !ok {
		return ctx, false
	}
	end := start + len(blk)
	before := ctx[:start]
	after := ctx[end:]
	switch {
	case before == "" && after == "":
		return "", true
	case before == "":
		return strings.TrimPrefix(after, "\n\n"), true
	case after == "":
		return strings.TrimSuffix(before, "\n\n"), true
	default:
		return strings.TrimSuffix(before, "\n\n") + "\n\n" + strings.TrimPrefix(after, "\n\n"), true
	}
}

func findContextBlockRange(ctx, blk string) (int, bool) {
	if ctx == "" || blk == "" {
		return 0, false
	}
	offset := 0
	for offset <= len(ctx)-len(blk) {
		idx := strings.Index(ctx[offset:], blk)
		if idx < 0 {
			return 0, false
		}
		start := offset + idx
		end := start + len(blk)
		leftOK := start == 0 || (start >= 2 && ctx[start-2:start] == "\n\n")
		rightOK := end == len(ctx) || (end+2 <= len(ctx) && ctx[end:end+2] == "\n\n")
		if leftOK && rightOK {
			return start, true
		}
		offset = start + 1
	}
	return 0, false
}

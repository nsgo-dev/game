package refs

import (
	"path"
	"slices"
	"strings"
)

// IsAssetPath reports whether p, a slash-separated project path, names a game
// asset file (image/sprite, tileset, audio, font, or 3D model) rather than a
// scene, script, project manifest, or import/source sidecar.
func IsAssetPath(p string) bool {
	if p == "" {
		return false
	}
	lower := strings.ToLower(p)
	if strings.HasSuffix(lower, ".import.yaml") || strings.HasSuffix(lower, ".import.yml") ||
		strings.HasSuffix(lower, ".source.yaml") || strings.HasSuffix(lower, ".source.yml") {
		return false
	}
	if strings.HasSuffix(lower, ".tileset.yaml") || strings.HasSuffix(lower, ".tileset.yml") {
		return true
	}
	switch strings.ToLower(path.Ext(lower)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".aseprite", ".ase",
		".tsj", ".tsx",
		".wav", ".ogg", ".mp3", ".flac", ".m4a", ".aac", ".opus",
		".ttf", ".otf", ".woff", ".woff2",
		".gltf", ".glb":
		return true
	default:
		return false
	}
}

// Paths returns every file path Scan found in the directory, in sorted path order.
func (p *Project) Paths() []string {
	if p == nil || len(p.paths) == 0 {
		return nil
	}
	return slices.Clone(p.paths)
}

func dirMatchesPath(dirTarget, cleanPath string) bool {
	if dirTarget == "." {
		return cleanPath != "" && cleanPath != "."
	}
	return strings.HasPrefix(cleanPath, dirTarget+"/")
}

// UsedBy returns every non-output reference in p that targets target, in the
// order Scan recorded them. A directory reference (Ref.Dir) matches every file
// inside that directory.
func (p *Project) UsedBy(target string) []Ref {
	if p == nil || target == "" {
		return nil
	}
	clean := path.Clean(strings.ReplaceAll(target, "\\", "/"))
	var out []Ref
	for _, r := range p.Refs {
		if r.Output || r.Target == "" {
			continue
		}
		if r.Target == clean || (r.Dir && dirMatchesPath(r.Target, clean)) {
			out = append(out, r)
		}
	}
	return out
}

// MissingAssets returns the sorted, deduplicated slash-separated paths of asset
// files (IsAssetPath) referenced by non-output file references in p that do not
// exist on disk.
func (p *Project) MissingAssets() []string {
	if p == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, r := range p.Refs {
		if r.Output || r.Dir || r.Target == "" || p.Found(r) {
			continue
		}
		if !IsAssetPath(r.Target) || seen[r.Target] {
			continue
		}
		seen[r.Target] = true
		out = append(out, r.Target)
	}
	slices.Sort(out)
	return out
}

// UnreferencedAssets returns the sorted slash-separated paths of asset files
// (IsAssetPath) present in p that are neither generated output targets
// (Ref.Output) nor referenced by any file in p (UsedBy).
func (p *Project) UnreferencedAssets() []string {
	if p == nil {
		return nil
	}
	outputFiles := make(map[string]bool)
	var outputDirs []string
	outputRoot := false
	usedFiles := make(map[string]bool)
	var usedDirs []string
	usedRoot := false
	for _, r := range p.Refs {
		if r.Target == "" {
			continue
		}
		if r.Output {
			if r.Dir {
				if r.Target == "." {
					outputRoot = true
				} else {
					outputDirs = append(outputDirs, r.Target+"/")
				}
			} else {
				outputFiles[r.Target] = true
			}
			continue
		}
		if r.Dir {
			if r.Target == "." {
				usedRoot = true
			} else {
				usedDirs = append(usedDirs, r.Target+"/")
			}
		} else {
			usedFiles[r.Target] = true
		}
	}
	if outputRoot || usedRoot {
		return nil
	}
	var out []string
	for _, name := range p.paths {
		if !IsAssetPath(name) || outputFiles[name] || usedFiles[name] {
			continue
		}
		skip := false
		for _, prefix := range outputDirs {
			if strings.HasPrefix(name, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, prefix := range usedDirs {
			if strings.HasPrefix(name, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

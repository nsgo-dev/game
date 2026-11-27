package refs

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"slices"
	"strings"
)

// IsAssetPath reports whether p, a slash-separated project path, unambiguously
// names a game asset file (image/sprite, tileset, audio, font, or 3D model) by
// its path alone, rather than a scene, script, project manifest, or
// import/source sidecar.
//
// Because .tsx is shared by Tiled XML tilesets and TypeScript JSX mod scripts,
// IsAssetPath returns false for .tsx paths; use (*Project).IsAsset to classify
// .tsx files using their contents, map references, and mod.yaml manifests.
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
		".tsj",
		".wav", ".ogg", ".mp3", ".flac", ".m4a", ".aac", ".opus",
		".ttf", ".otf", ".woff", ".woff2",
		".gltf", ".glb":
		return true
	default:
		return false
	}
}

// IsAsset reports whether name, a slash-separated project path, names a game
// asset file in p. It matches IsAssetPath, and additionally classifies a .tsx
// file as a Tiled XML tileset when it is not named as a mod's entry in a
// mod.yaml manifest and either parses as Tiled XML (<tileset>) or is referenced
// as an external tileset from a map. When p is nil, IsAsset falls back to
// IsAssetPath.
func (p *Project) IsAsset(name string) bool {
	if name == "" {
		return false
	}
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if p == nil {
		return IsAssetPath(clean)
	}
	if p.modEntries[clean] {
		return false
	}
	if IsAssetPath(clean) {
		return true
	}
	if strings.EqualFold(path.Ext(clean), ".tsx") {
		return p.xmlTSX[clean] || p.mapTSX[clean]
	}
	return false
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// isTiledTSX reports whether data is a well-formed XML document whose root
// element is <tileset>, as Tiled writes external XML tilesets (.tsx).
func isTiledTSX(data []byte) bool {
	data = bytes.TrimPrefix(data, utf8BOM)
	dec := xml.NewDecoder(bytes.NewReader(data))
	found := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return found
		}
		if err != nil {
			return false
		}
		switch t := tok.(type) {
		case xml.ProcInst, xml.Comment, xml.Directive:
			continue
		case xml.CharData:
			if len(bytes.TrimSpace(t)) != 0 {
				return false
			}
		case xml.StartElement:
			if found || t.Name.Space != "" || t.Name.Local != "tileset" {
				return false
			}
			found = true
			if err := dec.Skip(); err != nil {
				return false
			}
		default:
			return false
		}
	}
}

// parseTMXTilesetSources returns the source attribute of every top-level
// <tileset> child of a Tiled XML <map> document.
func parseTMXTilesetSources(data []byte) []string {
	data = bytes.TrimPrefix(data, utf8BOM)
	dec := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	inMap := false
	var sources []string
	for {
		tok, err := dec.Token()
		if err != nil {
			return sources
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				if t.Name.Space != "" || t.Name.Local != "map" {
					return nil
				}
				inMap = true
			} else if inMap && depth == 2 && t.Name.Space == "" && t.Name.Local == "tileset" {
				for _, a := range t.Attr {
					if a.Name.Space == "" && a.Name.Local == "source" && a.Value != "" {
						sources = append(sources, a.Value)
					}
				}
			}
		case xml.EndElement:
			depth--
		}
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
// files (Project.IsAsset) referenced by non-output file references in p that do
// not exist on disk.
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
		if !p.IsAsset(r.Target) || seen[r.Target] {
			continue
		}
		seen[r.Target] = true
		out = append(out, r.Target)
	}
	slices.Sort(out)
	return out
}

// UnreferencedAssets returns the sorted slash-separated paths of asset files
// (Project.IsAsset) present in p that are neither generated output targets
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
		if !p.IsAsset(name) || outputFiles[name] || usedFiles[name] {
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

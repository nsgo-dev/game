package gltf

import (
	"strconv"
	"strings"
)

// ParseLODNodeName inspects a glTF node or mesh name for the case-insensitive
// _LOD<N> suffix convention (such as "Tree_LOD0", "Tree_LOD1", "Tree_lod2"),
// tolerating Blender's optional trailing ".001" duplicate numbering.
//
// When name carries a recognized suffix with a non-empty base name and a
// non-negative integer level, ParseLODNodeName returns (baseName, level, true).
// Otherwise it returns ("", 0, false).
func ParseLODNodeName(name string) (baseName string, level int, ok bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", 0, false
	}

	// Strip optional Blender duplicate numbering (".001", ".002", ...).
	candidate := trimmed
	if dot := strings.LastIndexByte(trimmed, '.'); dot > 0 && dot+1 < len(trimmed) {
		allDigits := true
		for i := dot + 1; i < len(trimmed); i++ {
			if trimmed[i] < '0' || trimmed[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			candidate = trimmed[:dot]
		}
	}

	// Find the last "_lod" (case-insensitive) followed exclusively by ASCII digits.
	lower := strings.ToLower(candidate)
	idx := strings.LastIndex(lower, "_lod")
	if idx <= 0 {
		return "", 0, false
	}
	digits := candidate[idx+4:]
	if len(digits) == 0 {
		return "", 0, false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return "", 0, false
		}
	}
	lv, err := strconv.Atoi(digits)
	if err != nil || lv < 0 {
		return "", 0, false
	}
	return candidate[:idx], lv, true
}

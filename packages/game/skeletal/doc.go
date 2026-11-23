// Package skeletal loads, saves and animates 2D skeletal rigs drawn through sprite.Batch.
//
// # Format decision
//
// Rigs use an open, versioned YAML format (.skel.yaml, version 1) that Load reads and Encode writes. Choosing an open schema over Spine's export avoids Spine's proprietary runtime license, matches NSGo's screen coordinates directly, and is simple to export from DragonBones, Blender or custom scripts. Like every file a game's author edits, a rig is plain text: one bone, slot, attachment or keyframe per line, so it can be edited by hand and reviewed as a diff. JSON is YAML flow syntax, so a version-1 rig written as JSON loads too.
//
// Load is strict: it rejects unknown fields, duplicate keys, trailing documents, unsupported versions and non-finite numbers, as well as every structural error listed on Load.
//
// # Coordinates and units
//
// Coordinates match package sprite and core.Drawing: +X points right, +Y points down, and rotations are in radians, clockwise on the screen. Keyframe times and animation durations are written in seconds, at most MaxSeconds, and stored as time.Duration in Go.
//
// # Schema (version 1)
//
// A rig file is a YAML mapping with:
//
//   - version (int, required): must be 1.
//   - name (string, optional): rig name.
//   - bones (sequence): bone hierarchy in bind pose. Each bone has name (required, unique), optional parent (another bone's name), local offset x and y, local rotation in radians, local scaleX and scaleY (default 1), and optional length for tooling. Cycles and missing parents are rejected.
//   - slots (sequence): draw-ordered list, back to front. Each slot has name (required, unique), bone (required, an existing bone's name), optional attachment (default atlas frame name), and alpha in [0, 1] (default 1).
//   - skins (mapping): maps skin name (default "default") to a mapping of slot name to an attachment {frame, x, y, rotation, scaleX, scaleY, originX, originY}. scaleX and scaleY default to 1. Slots missing from the active skin fall back to "default". An empty frame hides the slot.
//   - animations (mapping): maps animation name to {duration, loop, bones, slots}. duration is in seconds (derived from the last keyframe when omitted). Each bone timeline has optional translate [{time, x, y, curve, bezier}], rotate [{time, angle, curve, bezier}] and scale [{time, x, y, curve, bezier}], applied relative to bind pose (translate and rotate add; scale multiplies). Each slot timeline has optional attachment [{time, frame}] and alpha [{time, value, curve, bezier}]. curve is "linear" (default), "stepped", or "bezier" with bezier: [cx1, cy1, cx2, cy2] where cx1, cx2 lie in [0, 1]; bezier is rejected on other curves.
//
// # Canonical form
//
// Encode writes one canonical form: keys in schema order, skins, attachments, animations and timelines sorted by name, each bone, slot, attachment and keyframe as a one-line flow mapping, and fields equal to their defaults left out, except that keyframes always write their time and values and animations their duration. Loading a canonical file and encoding it gives the same bytes back, and loading what Encode wrote gives the same rig. Comments and anchors are not kept.
//
// # Sample
//
//	version: 1
//	name: runner
//	bones:
//	  - {name: root}
//	  - {name: torso, parent: root, y: -18, length: 16}
//	  - {name: head, parent: torso, y: -16, length: 12}
//	slots:
//	  - {name: torso, bone: torso}
//	  - {name: head, bone: head}
//	skins:
//	  default:
//	    head: {frame: head, originX: 6, originY: 12}
//	    torso: {frame: torso, originX: 6, originY: 16}
//	animations:
//	  idle:
//	    duration: 1
//	    loop: true
//	    bones:
//	      torso:
//	        translate:
//	          - {time: 0, x: 0, y: 0, curve: bezier, bezier: [0.25, 0.1, 0.25, 1]}
//	          - {time: 0.5, x: 0, y: -2}
//	          - {time: 1, x: 0, y: 0}
package skeletal

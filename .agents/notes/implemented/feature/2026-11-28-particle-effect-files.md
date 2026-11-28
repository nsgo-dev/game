# Agent Note: Particle effect files

Status: implemented

## Problem

A particle file describes one emitter and a scene object carries at most one, so an effect made of a flash, sparks and smoke with different delays is split across several objects and started together from Go, and variations such as a tint per element are made by rebuilding a `particles.Config` in code (#1730). Designers cannot tune a whole effect as one text asset.

## Decision

`pkg/particles/effect.go` adds the version 1 effect file: `particles.Effect` lists emitters, each exactly one of an inline `config` or a `file` path to an emitter definition, with `offsetX`, `offsetY`, `delay`, `duration` and `minQuality`, and declares typed parameters (`float`, `int`, `color`, `vec2`). `UnmarshalEffectYAML` and `MarshalEffectYAML` follow the emitter format in `pkg/particles/format.go` instead of encoding `Config` directly: the inline config goes through `configDoc` (`configToDoc` and `configFromDoc`), so it is the emitter file schema without its `version`, with durations as strings and quoted colors, and a file round-trips byte for byte. Loading refuses unknown fields, null values, a second document, a version in an inline config, both or neither of `file` and `config`, a path `fs.ValidPath` refuses, a negative `delay` or `duration`, a `minQuality` other than the lowercase names of `quality.Level`, and a parameter whose value is missing, not finite, or written under another type's key; errors are `*loaderr.Error` values with the field path. `Effect.SetParam` takes the Go type of the parameter (`float64` or `float32`, `int`, `core.Color`, `[2]float32`) and leaves the parameter unchanged on an error.

Each parameter keeps its value under the key of its type (`float: 1.5`, `color: "#ff8800ff"`) rather than a single `default:` key, so the decoder checks the value's YAML type with the same field types as the emitter format (`docInt`, `docColor`) and a value of the wrong type is a load error rather than a runtime surprise.

## Alternatives considered

Embedding `*Config` with YAML tags in the effect struct, as the first version of this change did, was rejected: `Config` has no YAML schema of its own, so the shape (unexported fields) was dropped on save, durations were written as nanosecond integers and the space as a number, and an inline config did not match the emitter files designers already write. Keeping one emitter per scene object was rejected because timing and authoring drift across objects. Untyped parameter maps were rejected because a misspelled or mistyped value would fail only during play.

## Consequences

The effect file is a format and parameter store only. Nothing plays an `Effect` yet, so `delay`, `duration`, `offsetX`, `offsetY` and `minQuality` have no runtime effect; `UnmarshalEffectYAML` does not load file-backed emitters; emitter fields cannot yet be written as `{param: name, scale: k}`; and the proposal's `kind: effect` field, particle events, culling, scene components that set parameters, a JSON Schema and `game check` coverage remain open in #1730.

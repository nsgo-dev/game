# Agent Note: Input glyph sets

Status: implemented

## Problem

PromptTable labels were hard-coded per action and device, so rebinding an action could leave HUD prompts stale.

## Decision

`pkg/input` defines strict version 1 `GlyphSet` YAML maps from physical binding names to Prompt values. Encode and parse are deterministic and reject unknown fields, giving games a data source to resolve prompts from actual bindings while retaining PromptTable overrides.

## Alternatives considered

Adding more hard-coded action names was rejected because projects use custom actions and rebindings. Shipping binary icon metadata was rejected because input configuration is text and should be reviewable.

## Consequences

Games can load localized, device-specific glyph labels from text and combine them with PromptTable fallback behavior. Screen prompt nodes and automatic binding resolution remain follow-up work.

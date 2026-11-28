# Agent Note: Shared template manifests

Status: implemented

## Problem

Template identity and capabilities were duplicated in Go, editor heuristics, and website TypeScript, so summaries drifted and agents could not query structured choices.

## Decision

Each template ships a strict version 1 `template.yaml`, parsed by `pkg/tooling/templatemeta`. The embedded game CLI registry reads manifests for summaries, while the editor Start Screen receives title, dimension, genre, orientation, inputs, and player bounds. `game new -list -json` and `templates_list` expose the same metadata.

## Alternatives considered

Keeping the Go summary as the source of truth would leave the website and agent without structured fields. A website-only manifest would make offline `game new` and the editor disagree, so the manifest lives next to every template and is embedded with it.

## Consequences

Every template now carries one additional canonical text file and its embedded copy must stay byte-identical. Website generation remains a follow-up, while CLI, editor, and MCP consumers already share validated metadata and filters.

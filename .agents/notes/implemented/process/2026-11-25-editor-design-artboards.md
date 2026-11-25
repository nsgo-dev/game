# Agent Note: The editor is built from a design kept in the repository

Status: implemented

## Problem

The editor grew one narrow issue at a time. Each issue's acceptance was an API and its tests, so the agents that built them spent their effort on library functions and wired a few buttons on top; nobody looked at the result as a user would, and the editor that came out had no layout, no visual language and no shape of its own. The direction has since been set: AI first, with the agent editing through the editor's MCP server and people reviewing, and few manual tools, each finished. Without a shared picture of that, each pull request would again invent its own layout and colors.

## Decision

- The design lives in `docs/design/editor/`: one `.dc.html` artboard per screen or state and `canvas.json` laying them out, readable as HTML in review and rendered on the design canvas. `Spec.dc.html` holds the tokens, contrast table, line styles, focus rules, keyboard scopes and shortcuts.
- Every editor issue names the artboards it implements. An editor pull request is accepted only with real nsgotest screenshots on macOS beside those artboards, using only the Spec's tokens, and after a person has looked at them.
- A change to how the editor should look or behave changes the artboard in the same pull request as the code, or first in its own.

## Alternatives considered

- **Design only in an external tool**: contributors and agents could not read it, and issues could not link to it.
- **Describe the design in prose in each issue**: the issues drifted apart, and prose does not fix spacing, color and state the way an artboard does.

## Consequences

- Editor work now has a visual acceptance bar that tests alone cannot meet; a pull request without screenshots beside its artboard is not done.
- The artboards must be kept current: an editor change that departs from them updates them, or the design and the code drift apart again.

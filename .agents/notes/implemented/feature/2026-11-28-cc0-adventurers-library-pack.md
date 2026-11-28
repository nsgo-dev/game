# Agent Note: CC0 Adventurers library pack

Status: implemented

## Problem

The shared library had only a 2D tile pack, so the 3D templates had no rigged character that an agent or developer could install and inspect, and no written list of clip names and durations to wire animations against (#1964).

## Decision

The library ships `cc0-adventurers` with two placeholder text glTF models, `mage.gltf` and `knight.gltf`: one triangle skinned to one joint each, carrying the twelve clip names the KayKit Adventurers models use (`Attack`, `AttackHeavy`, `Block`, `Death`, `Defeat`, `Dodge`, `Hit`, `Idle`, `Jump`, `Run`, `Victory`, `Walk`). Their import sidecars credit the repository's contributors under CC0-1.0, not KayKit, because the files are the repository's own fixtures; the notice says what will replace them. `tools/clipinventory` reads the glTF JSON and writes the pack's README (meshes, skins, joint count, and each clip's duration, the end of its longest sampler input), and a test in the tools module, which CI runs, fails when the README is stale. A test in `pkg/tooling/library` installs the pack into a project and checks that `project.Check`, `assetcheck` and `credits.Collect` accept it and credit both models.

## Alternatives considered

Shipping the real KayKit models now was left for later: the scene renderer does not draw skinned models yet (#1382), so nothing could show them, and the vendor files are larger binary buffers that the placeholders let the pack's layout and tests settle without. Crediting the placeholders to KayKit, as a first draft did, was rejected because it attributes files to an author who did not make them. A locomotion graph file was dropped from the pack: the only `*.states.yaml` format today is the sprite state machine in `pkg/animclip`, whose states reference clip files rather than glTF clip names, so `game check` refused the file; the 3D animation graph format is #1405. Replacing the procedural rig in the `action3d` template was out of scope.

## Consequences

`game library install cc0-adventurers` installs a pack that passes `game check` and `game credits`. Replacing the placeholders with the KayKit models means changing the two glTF files and their sidecars' credits, regenerating the README with `tools/clipinventory`, and updating the digest in `library/index.yaml`. The README does not list triangle counts yet, and the pack has no animation graph until #1405 defines one.

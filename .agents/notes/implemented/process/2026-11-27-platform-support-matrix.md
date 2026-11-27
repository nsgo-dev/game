# Agent Note: Per-feature platform support matrix

Status: implemented

## Problem

Top-level docs (`README.md`, `docs/en/index.md`, `docs/zh/index.md`, `docs/en/shipping.md`, `docs/zh/shipping.md`) described NSGo Game as targeting "six platforms" without distinguishing buildability from runtime feature parity or CI test execution. Developers targeting mobile in-app purchases, web cloud saves, or experimental targets (HarmonyOS, tvOS, watchOS, mini programs) discovered unsupported features only after building or running their game.

## Decision

`docs/en/platforms.md` and `docs/zh/platforms.md` maintain the canonical 20-feature × 10-target platform support matrix, verified directly against NSGo `v0.3.4` (`core/env.go`, backend `driver.Environment` builders, and `docs/architecture/`) and this repository's `//go:build` constraints and `.github/workflows/` matrices.

- Every cell uses one of four explicit states: `Supported`, `Partial (N)` (with a matching footnote), `Not available` (always linking its upstream `nsgo-dev/nsgo` or `nsgo-dev/game` tracking issue), or `Out of scope`.
- A dedicated `## How each row is decided` (`## 各行的判定依据`) table maps each feature row to its deciding `core.Env.Can...`/`Supports...` check, build tag, or workflow file so reviewers can keep cells current whenever NSGo or this repository adds platform support.
- A dedicated `## Experimental targets` (`#experimental-targets`) section documents HarmonyOS, tvOS, watchOS, and mini programs so downstream CLI and editor warnings (#1309) can link a stable anchor.
- `README.md`, `docs/en/index.md`, `docs/zh/index.md`, `docs/en/shipping.md`, and `docs/zh/shipping.md` describe the primary six targets as "six build targets" and link directly to `platforms.md` instead of implying full feature parity.
- `apps/website/src/content/groups.ts` (`SIDEBAR_GROUPS`) and `apps/website/plugins/content.ts` (`DOC_ORDER`) register `platforms` in the website sidebar and `llms.txt` / `llms-full.txt` output right before `shipping`.

## Alternatives considered

- **Keep target notes scattered inside per-package guides**: each package guide (`liveops.md`, `achievements.md`, `netplay.md`, `shipping.md`) already mentions some platform limits, which avoids maintaining one central table. Rejected because developers evaluating the engine cannot see cross-cutting gaps (such as missing Android IAP, no remote push, or limited CI coverage) in one place, and downstream warnings (#1309) need a single authoritative target page.
- **Generate the matrix automatically from AST/build tags**: avoids manual drift for build-tagged packages. Rejected because most platform gaps live inside NSGo `v0.3.4`'s runtime capability bits (`driver.Environment` in `internal/platform/*`) or unimplemented upstream features (remote push, Steam Deck verification) that static analysis of `nsgo-dev/game` alone cannot infer. Instead, the `## How each row is decided` table cites the exact runtime guard or build tag for manual review.

## Consequences

- **Benefits**: Sets honest expectations before developers commit to a target, gives every gap a clickable upstream tracking issue, exposes real CI test coverage alongside `nsgo check` vetting, and provides stable anchors (`#feature-matrix`, `#experimental-targets`, `#upstream-gaps`) for docs and tooling.
- **Costs**: The matrix is manually maintained and must be updated alongside NSGo version bumps or CI workflow changes that close listed gaps.

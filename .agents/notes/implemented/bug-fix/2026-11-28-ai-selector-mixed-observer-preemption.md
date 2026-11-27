# Agent Note: Selector only skips higher-priority branches governed solely by BlackboardCondition observers

Status: implemented

## Problem

`Selector` previously set `hasObserver = true` on any `Sequence`, `Selector`, or `Guard` branch that contained a `BlackboardCondition` anywhere in its subtree, and `Selector.Tick` skipped ticking those higher-priority siblings while a lower-priority branch was running, relying exclusively on `bbWantsPreempt()` (#1585). Because `sequenceNode.bbWantsPreempt()` only inspected `BlackboardCondition` children and ignored plain `Condition`, `Guard`, `WithAttackToken`, and other non-blackboard nodes, mixing a perception `Condition` or `Guard` with a `BlackboardCondition` (even an entry-only `AbortNone` condition) silently disabled ordinary `Selector` preemption.

This partly narrows the observer-shortcut contract introduced in [YAML behavior trees and typed blackboard with observer aborts in game/ai](../feature/2026-11-25-ai-bt-yaml-blackboard.md).

## Decision

Restrict the `Selector` observer shortcut (`btObserver.hasBBObserver()`) to branches whose gating is governed **solely** by `BlackboardCondition` nodes, while keeping all classification precomputed at construction time so `Tick` stays zero-allocation:

- **Separate self-abort from lower-abort governance on `Sequence` (`pkg/ai/bt.go`, `pkg/ai/bt_blackboard.go`)**:
  - `sequenceNode` precomputes `hasSelfAbort` (true if any child is a `BlackboardCondition` with `AbortSelf` or `AbortBoth`, optionally wrapped in `Named`) separately from `observerOnly` (true only when the sequence begins with a non-empty prefix of observer-governed children and contains no non-blackboard gating node and no trailing `BlackboardCondition` after the prefix).
  - `sequenceNode.Tick` checks `hasSelfAbort`, so `AbortSelf`/`AbortBoth` leaves inside mixed sequences continue to self-abort later running siblings.
  - `sequenceNode.hasBBObserver()` returns `observerOnly`, so any sequence mixing `Condition`, `Guard`, `WithAttackToken`, or leading non-observer nodes with `BlackboardCondition` falls back to ordinary per-tick `Selector` evaluation.
- **Keep `Guard` and mixed `BlackboardCondition` / nested `Selector` branches out of the shortcut (`pkg/ai/bt.go`, `pkg/ai/bt_blackboard.go`)**:
  - `guardNode` no longer implements `btObserver` because its predicate is not tracked by any blackboard.
  - `blackboardConditionNode` precomputes `childHasNonBBGate = nodeHasNonBBGate(child)` (detecting `Condition`, `Guard`, `WithAttackToken`, and composites/decorators containing them, including custom `ChildNodes` with cycle protection) and reports `hasBBObserver() == !childHasNonBBGate`.
  - `selectorNode` distinguishes `hasObserver` (any child is observer-governed, used by its own `Tick`) from `allObserver` (non-empty and every non-nil child is observer-governed, returned by `hasBBObserver()` to an enclosing `Selector`).
- **Preserve pure blackboard-only key-driven behavior**:
  - Branches made purely of `BlackboardCondition` nodes (for example `BlackboardCondition(..., AbortNone/AbortSelf/AbortLower/AbortBoth, action)` or `Sequence(BlackboardCondition(...), action)`) remain `hasBBObserver() == true`, keeping `AbortNone`/`AbortSelf` non-preempting on lower siblings and `AbortLower`/`AbortBoth` key-revision-driven.
  - When an outer `AbortNone`/`AbortSelf` `BlackboardCondition` already evaluated true and wraps an observer-governed child (such as an inner `AbortLower` `BlackboardCondition`), `bbWantsPreempt()` still delegates to the child if the outer condition still holds.

## Alternatives considered

- **Making `blackboardConditionNode.hasBBObserver()` return `false` for `AbortNone` unconditionally**:
  - Would have let `Sequence(Condition(seen), BlackboardCondition(..., AbortNone, attack))` preempt, but would have broken the documented and tested contract on pure blackboard branches (`Selector(BlackboardCondition(bb, "seen", ..., AbortNone, chase), patrol)`), where `AbortNone` must not abort a running lower-priority sibling on key change — turning `AbortNone` into `AbortLower` inside a `Selector` — and would still have left `AbortSelf` and `AbortLower` mixed sequences broken when `seen` changed.
- **Polling plain `Condition` / `Guard` predicates inside `bbWantsPreempt()`**:
  - Would duplicate `Sequence`/`Guard` evaluation logic inside the observer pass, risk side-effect divergence and double-evaluating predicates on the same tick, and still miss custom gating nodes or actions before a `BlackboardCondition`. Falling back to `Selector`'s standard `ch.Tick(dt)` reuses the single authoritative execution path.

## Consequences

- **Benefits**:
  - Mixing `Condition`, `Guard`, or `WithAttackToken` with `BlackboardCondition` (`AbortNone`, `AbortSelf`, `AbortLower`, or `AbortBoth`) in either order or nested inside a `Selector` preempts running lower-priority siblings reliably in both Go-built and YAML-built trees.
  - Pure blackboard-only branches retain their exact key-driven observer abort behavior and zero-allocation fast path.
  - All classification happens once at node construction; `Tick(dt)` remains `0 allocs/op`.
- **Costs and known limits**:
  - Mixed higher-priority branches are ticked on every frame while a lower-priority branch runs (matching a `Selector` without blackboard observers); authors who want key-driven skipping on a branch should keep its gating purely on `BlackboardCondition` nodes.
  - `sequenceNode` and `selectorNode` each store three additional `bool` flags computed at construction time.

## Verification

- Added `TestSelectorMixedBlackboardAndPlainPreemption` in `pkg/ai/bt_yaml_test.go` covering `Sequence(Condition, BlackboardCondition)` and `Sequence(BlackboardCondition, Condition)` across all four abort modes (including `AbortSelf`/`AbortBoth` self-abort preservation), `Guard` outside and inside `BlackboardCondition`, nested `Selector` and `WithAttackToken`, pure blackboard-only key-driven preservation and sibling isolation, YAML-built trees, and `0 allocs/op` on `Tick`.
- Verified with `gofmt -l pkg/ai/`, `go tool nsgo test ./pkg/ai/...`, `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/ai`, `(cd tools && go run ./changelog check)`, and `bun run verify-notes`.

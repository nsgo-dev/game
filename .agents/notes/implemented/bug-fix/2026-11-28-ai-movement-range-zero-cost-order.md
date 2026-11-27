# Agent Note: Range queries preserve cost-then-index ordering across zero-cost edges

Status: implemented

## Problem

`AStar.MovementRange`, `AStar.MovementRangeInto`, `AStar.MovementRangeWithCost`, `ReachableCells`, and `GraphSearch.Range` / `RangeInto` document returning reachable cells or nodes in deterministic ascending-`Cost` then row-major / tie-break order while accepting finite non-negative edge costs including `0` (#1588).

Previously, both `MovementRangeWithCost` (`pkg/ai/topology.go`) and `GraphSearch.RangeInto` (`pkg/ai/graph.go`) relied solely on min-heap pop order to establish the output slice order. That invariant holds only when every node of cost `c` is already in the heap before the first cost-`c` node is popped. When a zero-cost edge (or floating-point absorption `curG + delta == curG`) lets an already-popped cost-`c` node discover another cost-`c` node with a smaller row-major index or `NodeOrder`/`Less` rank, the smaller node was appended after the larger one, violating the documented ordering.

## Decision

Detect pop-order inversions during the Dijkstra loop and sort the output slice in place only when an inversion occurs:

- **Grid movement ranges (`pkg/ai/topology.go`)**:
  - Track the previously emitted `(prevCost, prevID)` in `MovementRangeWithCost` (shared by `MovementRange`, `MovementRangeInto`, and `ReachableCells`). If a newly popped cell satisfies `curG < prevCost || (curG == prevCost && cur.id < prevID)`, mark `needsSort = true`.
  - After the search loop finishes, if `needsSort` is set, sort `out` in place with `slices.SortFunc(out, compareReachableCell)` using a top-level comparator on `(Cost, Cell.Row, Cell.Col)` (equivalent to `(Cost, row-major index)` for in-bounds cells and free of closure allocations).
- **Generic graph ranges (`pkg/ai/graph.go`)**:
  - Track `prevID` in `GraphSearch.RangeInto` and mark `needsSort = true` whenever `s.lessID(curID, prevID, order)` holds for an emitted node.
  - When `needsSort` is set, sort `s.reached` in place by `(g, order.compare, discoveryID)` and refresh `s.nodes[id].outIdx = int32(i)` for every reached node before populating `dst.nodes`, `dst.parents`, and `dst.index` (updating [generic graph search, hex tilemap adapter, and waypoint graphs](../feature/2026-11-25-ai-generic-graph-hex-waypoint-search.md)). Because Dijkstra closes every node before relaxing its outgoing edges, parent links remain an acyclic tree rooted at `start`, so `GraphRange.Parent` and `GraphRange.PathInto` work identically even when a child precedes its parent in `dst.nodes`.

## Alternatives considered

- **Always calling `slices.SortFunc` unconditionally after every range query** — Simpler by a few lines and still linear on already-sorted slices, but adds a full verification pass over `out` on the common strictly-positive-cost path where pop order is already strictly sorted. Checking adjacent popped elements during the loop costs one scalar comparison per closed node and avoids calling the sorter completely when no inversion occurred.
- **Re-ordering within the Dijkstra loop (for example buffering each equal-cost frontier before emitting)** — Would emit directly in final order without a post-sort, but requires an extra frontier slice, careful interaction with `MaxExpansions`, and separate handling of float equality boundaries, while offering no asymptotic benefit over an in-place post-sort.
- **Tracking a `sawZeroEdge` flag on neighbor relaxation instead of checking adjacent popped nodes** — Misses zero-increment transitions caused by floating-point absorption (`curG + step == curG` for tiny positive weights) and still triggers sorting when zero-cost edges only reach higher-indexed cells that were already in order.

## Consequences

- **Benefits**:
  - `MovementRange`, `MovementRangeInto`, `MovementRangeWithCost`, `ReachableCells`, and `GraphSearch.Range` / `RangeInto` always satisfy their documented `(Cost, index / tie-break)` ordering, including on zero-cost edges, zero-cost chains, and zero-cost cycles, without infinite loops.
  - Positive-cost queries incur no sorting pass; zero-cost queries sort in place with `0 allocs/op` on `MovementRangeInto` and `RangeInto`.
- **Costs and known limits**:
  - In `GraphRange.Nodes()`, a node reached via a zero-cost edge may now appear at a smaller slice index than its parent; `GraphRange.Parent`, `Path`, and `PathInto` handle arbitrary valid parent indices and must never assume `parents[i] < i`.

## Verification

- `gofmt -l pkg/ai/`
- `go tool nsgo test ./pkg/ai/...`
- `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/ai`
- `(cd tools && go run ./changelog check)`
- `bun run verify-notes`

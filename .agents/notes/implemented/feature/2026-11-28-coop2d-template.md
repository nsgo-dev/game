# Agent Note: Co-op 2D template

Status: implemented

## Problem

The repository had online transport and server primitives but no starter project showing room membership, authoritative fixed ticks, reconnect, or cooperative life-cycle rules.

## Decision

`template/coop2d` ships a small deterministic room server model with versioned `coop.yaml` tuning. Room codes and invite links are pure helpers, membership state survives a leave for the configured grace period, and the fixed tick owns revive and solo respawn transitions. The client and dedicated-server entry points stay theme-neutral so games can replace rendering while retaining the tested session chassis.

## Alternatives considered

Adding networking directly to an existing single-player template would make its tests and lifecycle harder to understand. A server-only example would leave `game new` without a client project, so the template carries both entry points.

## Consequences

The template is a deterministic in-process chassis and does not provision hosting or a third-party matchmaking service. Games still connect the `Server` model to their chosen netplay transport, while the core join, leave, rejoin, revive, and respawn rules remain covered by headless tests.

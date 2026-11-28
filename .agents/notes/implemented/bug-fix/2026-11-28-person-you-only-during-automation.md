# Agent Note: Person You-only actions during automation

Status: implemented

## Problem

`AgentActorActive` is a process-wide depth counter held for an automation's entire background run. You-only editor actions checked that counter, so a person's click was rejected whenever an unrelated automation was running.

## Decision

Person-facing You-only checks use the per-call `AgentMutating` marker set by `RunAsAgent`. The process-wide actor marker remains available for reporting active automation work and for the task-source guard that prevents an automation from keeping its own task. Regression coverage keeps a proposed deletion confirm available to the person while an actor is active and keeps the same call refused inside `RunAsAgent`.

## Alternatives considered

Keeping `AgentActorActive` on all checks preserves the old direct-agent guard, but it also rejects concurrent person actions. Adding goroutine identity tracking would distinguish callers but would introduce runtime-specific bookkeeping for a boundary already represented by `RunAsAgent`.

## Consequences

Person settings, review decisions, automation controls, rename and publish actions, and other You-only operations can proceed from the editor's person call while an automation runs. Agent calls must enter `RunAsAgent` so the per-call marker is set. Existing automation tests now exercise that explicit agent boundary.

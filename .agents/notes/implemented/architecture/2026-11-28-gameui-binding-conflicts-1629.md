# Agent Note: game UI binding conflict policies

Status: implemented

## Problem

Rebinding one action could silently leave another action using the same device binding.

## Decision

Add grouped conflict resolution with swap, clear and refuse policies. Actions in separate groups may share bindings by design.

## Consequences

Settings UI integrations can choose a predictable conflict policy and report the conflicting action. Controller-family labels and DeviceTracker integration remain follow-up work.

## Alternatives considered

Always refusing conflicts was rejected because menus and gameplay often intentionally share inputs. Always clearing the old binding was rejected because swap preserves the user's previous action mapping.

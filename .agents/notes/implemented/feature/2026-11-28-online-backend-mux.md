# Agent Note: Compose online services and game RPCs

Status: implemented

## Problem

Games had to wrap each online service handler separately and had no standard in-process route for compiled server-side RPCs (#1694).

## Decision

`backend.Backend` mounts existing service handlers and registers authenticated JSON POST RPCs. The application supplies authentication and receives a typed Caller, keeping token verification policy outside the mux.

## Alternatives considered

- Parse and rewrite each service's JSON in the backend: rejected because it duplicates service contracts and risks inconsistent validation.
- Load hooks dynamically from configuration: rejected because server rules must be compiled and auditable.

## Consequences

One process can expose enabled services and game RPCs behind one handler. Service-specific before/after hooks and combined YAML configuration remain extension points for later service integrations.

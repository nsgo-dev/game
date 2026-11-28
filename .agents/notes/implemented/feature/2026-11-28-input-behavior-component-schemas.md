# Agent Note: Input-driven behavior component schemas

Status: implemented

## Problem

The repository had controller libraries, but scenes had no shared component schemas for declaring input-driven movement, aiming, dragging, visibility, or path-agent behavior.

## Decision

`sceneformat` exposes schema constructors for eight named behavior components and a helper to register all of them. Fields are typed through the existing component registry, with defaults and enums validated at scene load. Runtime stepping remains in the behavior follow-up work.

## Alternatives considered

Defining the fields independently in each template would duplicate validation and prevent the editor and agent tools from discovering a common vocabulary. Accepting arbitrary component maps would defer mistakes until runtime. Both were rejected.

## Consequences

Scene files can declare the stable behavior vocabulary and receive typed validation immediately. Controller execution, events, snapshots, and template migration remain follow-up work.

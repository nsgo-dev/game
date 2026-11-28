# Agent Note: render3d frame capture core

Status: implemented

## Problem

Render statistics exposed totals but no one-frame pass and draw ordering data for debugging.

## Decision

Add an explicitly armed `FrameCapture` that records named passes and draw metadata, then disarms when `Finish` returns a snapshot. Unarmed captures are inert.

## Consequences

Profiler and MCP integrations can expose one frame without adding steady-state work. GPU target readback, automatic render pass instrumentation and editor UI remain follow-up work.

## Alternatives considered

Always-on recording was rejected because it adds allocations and synchronization to every frame. Capturing only aggregate stats was rejected because it cannot explain draw order or pass ownership.

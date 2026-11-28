# Agent Note: Ask-in-place draft and context visibility

Status: implemented

## Problem

The anchored `⌘L` composer discarded typed instructions when `Esc` closed it, and its card did not show that a viewport PNG would accompany the request.

## Decision

The composer stores a non-empty draft with its target IDs and scene path when closed. Reopening the same target restores it, while another target starts empty. Submission clears the stored draft. A context summary is built from the same target and privacy state used by submission, and the turn omits viewport bytes when `AgentPrivacy().ViewportImages` is disabled.

## Alternatives considered

A global draft would restore text into an unrelated selection, so the draft is keyed by target IDs and scene path. Leaving the context implicit would preserve the old UI but make privacy-relevant image attachment surprising, so the card displays the privacy-filtered summary.

## Consequences

Accidental dismissals preserve work and the card makes outbound context visible. The general Agent-panel composer remains outside this slice, as does editing which context items are attached.

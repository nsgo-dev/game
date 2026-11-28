# Agent Note: Template gallery media metadata

Status: implemented

## Problem

Template cards on the editor Start Screen and the website showcase had no field for a cover image or a hover clip, so the media that #1962 plans to capture from each template's deterministic run had nowhere to be declared or shown.

## Decision

`editor.ProjectTemplate` gains an optional `Cover` URI. When it is set, the Start Screen draws the image over the card's 56 pt swatch (absolutely positioned, `core.Cover`), so the card keeps its height and the dimension chip; when it is empty the card is unchanged. The website `ShowcaseEntry` gains optional `cover` and `preview` fields. A card with a `cover` shows a lazy-loaded 16:9 image; a `preview` is a muted `<video preload="none">` that the card plays on `mouseenter` and focus and pauses on `mouseleave` and blur, so the clip is fetched only when a reader reaches the card.

Nothing sets these fields yet. The game CLI registry declares no cover, and no showcase entry has one, until the capture plan (#1933) and the template manifest (#1955) produce real media.

## Alternatives considered

A shared placeholder cover for every template and showcase entry was tried first and dropped: the same picture on every card tells the reader nothing, the website-relative path it used (`gallery/templates/cover.svg`) does not resolve in the editor, and a placeholder would hide which templates still lack a real cover. An editor-side `Preview` field was dropped too, because the Start Screen has no clip playback and an unread field would only suggest one. Embedding screenshots in the Go package was rejected because it grows the editor binary and bypasses the capture plan.

## Consequences

Existing templates and showcase entries render exactly as before. The remaining work of #1962 (capture plans with a `gallery` shot, committed covers, the CI drift job, and the gallery rules for size and content) still depends on #1933 and #1955; this change only provides the fields and the rendering those will feed.

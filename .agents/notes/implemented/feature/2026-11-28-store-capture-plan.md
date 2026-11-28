# Agent Note: Store capture plans

Status: implemented

## Problem

Store screenshots and covers were captured ad hoc: nothing recorded which scene, input recording and frame made each image or which sizes each store needs, so the set could not be rebuilt after an art pass and a wrongly sized or missing image was found only when a store refused it (#1933).

## Decision

`pkg/tooling/storecapture` owns the plan format, `store/capture.yaml`: `version: 1`, unknown fields refused, a single YAML document, and a decode and encode round trip that keeps the bytes. Each shot names a scene, an optional `.replay.yaml` recording, at most one of a fixed-step `frame` or `time`, an `output` name and its `sizes`, each a named preset or an explicit width and height, never both. A shot's image at a size is `<output>-<width>x<height>.png` under the media directory, so every file name is derived from the plan and two images writing the same file are refused. Scene, recording and output paths are slash-separated and must stay inside the project or the media directory.

`game capture` loads the plan, keeps only the shot `-shot` names, and runs `storecapture.Check`, which reports every missing scene or recording and every image that is missing, not a PNG or not its target size, joined into one error, so one run lists everything to fix.

The presets are store sizes that are actually accepted: `app_store_iphone` 1290x2796, `app_store_ipad` 2064x2752, `google_play` 1920x1080, `google_play_feature` 1024x500, `itch_cover` 630x500 and `minigame_share` 1280x720. A landscape App Store screenshot is an explicit size rather than another preset.

## Alternatives considered

- Rendering each shot now from a hash of its metadata (the first version of this change) was dropped: it wrote solid gradients under store file names that could be uploaded by mistake, and none of the game's pixels. Real rendering needs the fixed-step movie mode of #1438, which does not exist yet.
- A `game capture` that refuses to run until rendering exists was rejected: checking a hand-captured or `game_screenshot`-captured set against the plan already catches the wrong-size and missing-image failures the plan exists for.
- Keeping captures as untracked files avoids a format but cannot be rebuilt or reviewed.

## Consequences

The plan is reviewable and its images are checked before publishing. Rendering the shots through movie mode, the editor's Build and Release action and the release readiness warning are not part of this change; when rendering lands, `game capture` gains it on the same plan and `Check` stays the verification step after it.

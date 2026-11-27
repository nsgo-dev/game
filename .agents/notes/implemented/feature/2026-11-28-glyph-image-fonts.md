# Agent Note: Bitmap fonts baked from a folder of glyph images

Status: implemented

## Problem

A `fonts` target in `game.yaml` could only bake a TrueType or OpenType file into an AngelCode BMFont (`pkg/tooling/assetbuild/font.go`). Hand-drawn glyph images (score digits, damage numbers, a combo counter, "+" and "×") had no pipeline: a game placed one sprite per digit in Go or wrote the `.fnt` by hand. The packing and BMFont writing already existed; only the glyph source was missing.

## Decision

A font target's `in` may name a folder of PNG glyph images instead of a `.ttf` or `.otf` file, detected by the extension alone (`project.FontTarget.GlyphFolder`), so validation stays filesystem-free. The output is the same BMFont text and PNG pages the TTF path writes, so `text.LoadBMFont` and everything after it are unchanged.

- Each image is one glyph: a file whose base name's stem is one character, such as `7.png`, draws it, and the target's `glyphs` map names every other file to its character, such as `plus.png`: "+". Unmapped names, duplicate characters, `glyphs` entries for files the folder does not hold or whose stem is one character, non-PNG files in the folder (import sidecars excepted), and `glyphs` values that are not exactly one character are refused.
- `size` and `chars` are refused for a folder input and `glyphs`, `spacing` and `align` for a font file, so each form's settings are unambiguous. A folder's own sidecar (`score.import.yaml`) may set `padding` only; `size` and `chars` there are refused, and a sidecar beside an image inside the folder is refused unless it holds only a credit, as one in an atlas's folder is.
- Metrics come from the image, untrimmed: `lineHeight` and `base` are the tallest image's height, the advance is the image's width plus `spacing`, and `align` (top, center, bottom; bottom when omitted) sets each glyph's vertical offset in the line. Hand-drawn images carry no kerning, so the `.fnt` has none. The characters are written sorted by code point, as the TTF path writes them, and the target's key covers every glyph image and the mapping, so editing one image rebakes the font.

## Alternatives considered

### A new target list beside `fonts`

A separate `glyphFonts:` list would have kept `FontTarget` untouched. It was rejected because the output, page naming, cache and validation rules are identical to a font target's; two lists writing the same format would duplicate the pager logic in `project.Validate` for no separation a user can see.

### Trimming transparent borders like an atlas

Packing with `pack.Options.Trim` would make a glyph's advance its ink width, forgiving sloppy crops. It was rejected because untrimmed images are WYSIWYG: the advance is exactly the image's width plus `spacing`, transparent borders act as authored bearings, and a fully transparent image stays a legal blank glyph with an advance, needing no special case.

### Deriving `lineHeight` from a `size` setting

A `size` for folder fonts could have set the line height independently of the images. It was rejected because the issue fixes `lineHeight` and `base` at the tallest glyph, and a separate size would invite glyphs scaled or padded away from their authored pixels.

## Consequences

- **Benefits**: number fonts for HUDs and juice (score tickers, damage popups) build from art folders with no Go placement code and no hand-written `.fnt`; `game assets` rebuilds the font exactly when a glyph image or the mapping changes; `game check` reports a missing folder or mapped file with the target.
- **Costs**: `FontTarget` carries three folder-only fields and a `GlyphFolder` predicate that every consumer of `in` must respect (`assetbuild`, `checkSidecars`, docs); a folder named with a `.ttf` or `.otf` extension cannot be a glyph folder, and a `.woff` file is now refused as "not a directory" at build time rather than as a wrong extension at parse time.

## Verification

`pkg/project` validation tests cover every new refusal and the accepted folder form; `pkg/tooling/assetbuild` tests bake a folder of digit PNGs and check `text.LoadBMFont` loads it, that "1234" lays out with the expected advances and offsets per `align`, that two builds write identical bytes, that editing one image or the sidecar rebakes the font, and that each refusal fires; `go tool nsgo check` and `go tool nsgo test` pass.

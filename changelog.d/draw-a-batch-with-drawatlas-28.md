### Changed

- `game/sprite`: `Batch.Draw` draws consecutive sprites that are only moved, rotated and scaled evenly with one NSGo `DrawAtlas` instead of one `DrawImageFrom` each, drawing the same picture (mirrored, unevenly scaled, blended and masked sprites keep their own draw); recording 5,000 sprites takes 0.59 ms instead of 0.93 ms with 3 allocations instead of 5,000. `game/gametest` adds `ImageDraws` and `AssertSameImages` to check drawn images however they were recorded, and `RenderOps` renders `DrawAtlas` (#28).

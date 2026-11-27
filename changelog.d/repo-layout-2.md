### Changed

- `tooling/apiref`, the Go doc parser behind `tools/gameapi`, moves to `internal/apiref` and is no longer a public package: `github.com/nsgo-dev/game/pkg/tooling/apiref` can no longer be imported from outside this repository, and the package leaves the stability table and the API reference. The File Formats page's entry for `api.json` is now `internal/apiref`.
- The QR code encoder the editor's Web build card draws moves from `internal/qrcode` to `pkg/editor/internal/qrcode`, and the generator of the placeholder pack's sample glTF models moves from `internal/samplegltf` to `pkg/placeholder/internal/samplegltf`, next to their only users; run `go tool nsgo test ./pkg/placeholder/internal/samplegltf -update` to regenerate the models.

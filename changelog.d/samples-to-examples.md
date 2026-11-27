### Changed

- The dedicated-server and visual-novel samples move from `pkg/server/cmd/dedicatedserver` and `pkg/vn/cmd/vnsample` to their own modules under `examples/`: build the server with `go build ./examples/dedicatedserver` and play the scene with `go tool nsgo dev ./examples/vnsample`. They are no longer packages of the `github.com/nsgo-dev/game` module.

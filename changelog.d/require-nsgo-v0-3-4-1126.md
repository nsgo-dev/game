### Changed

- The game layer, every project template and the sample games require NSGo v0.3.4, whose `nsgo` runs `nsgo game` (added in v0.3.3) and whose GPU draws depth-tested 3D into a window's own depth buffer again (nsgo-dev/nsgo#2395), so `nsgo game new` then `nsgo game dev` work in a new project with no extra step; the docs no longer ask for an `nsgo` from main and show how to install `nsgo` with Homebrew, the install script or `go install` (#1126).

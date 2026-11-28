### Added

- The editor has **Help > Check for Updates...**, also in the Command Palette, which checks for a newer released editor and offers to install it: in Sparkle's window on macOS, WinSparkle's on Windows and an alert that installs the signed package on Linux.

### Fixed

- Released editors find newer releases: every release built the editor with build number 1, which the updater compares, so no release looked newer than the installed one and installed copies never offered an update. The release workflow now derives the build number from the version (`X.Y.Z` is `X*1000000+Y*10000+Z*100+99`, a prerelease `X.Y.Z-<name>.N` ends in `N` instead), so each release's is larger than the last; copies of 0.1.0 to 0.3.0 update to the next release on their own.

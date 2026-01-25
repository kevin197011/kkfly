# Change: Add automated GitHub Releases and installer

## Why
We need a consistent and automated way to publish platform-specific binaries and provide a one-command installer that downloads the correct artifact for the user's platform.

## What Changes
- Add a GitHub Actions workflow that builds and publishes a GitHub Release on tag pushes.
- Add GoReleaser configuration to standardize cross-platform builds and checksums.
- Add an `install.sh` installer that downloads the correct binary for the current platform from the release assets and installs it.

## Impact
- Affected specs: new capability `release-automation`
- Affected code: `.github/workflows/release.yml`, `.goreleaser.yaml`, `install.sh`, documentation


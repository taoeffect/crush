# Current Design

## Release planning source

- `.agents/notes/NPM_RELEASE_PLAN.md` contains the desired npm release strategy for the `taoeffect/crush` fork and `@taoeffects/crush` package.
- The current plan avoids GoReleaser Pro and distributes a small npm wrapper that downloads platform-specific release archives from GitHub Releases during `postinstall`.
- The plan currently has one naming issue: it says the npm account must publish under the `@taoeffect` scope, but the package name is `@taoeffects/crush`, so the scope is `@taoeffects`.

## Existing repository state

- There is currently no `npm/` directory and no npm package source files.
- There is currently no npm package generation script under `scripts/`.
- Existing scripts are:
  - `scripts/run-labeler.sh`
  - `scripts/check_log_capitalization.sh`
- The project module remains `github.com/charmbracelet/crush` in `go.mod`; release build flags should set `github.com/charmbracelet/crush/internal/version.Version`.
- `go.mod` declares Go `1.26.3`.

## Existing GitHub workflows

- `.github/workflows/release.yml` is upstream-oriented:
  - Trigger: pushed tags matching `v*.*.*`.
  - Job uses Charm-owned reusable workflow `charmbracelet/meta/.github/workflows/goreleaser.yml@main`.
  - Requires many Charm-owned secrets including `GORELEASER_KEY`, Docker, Fury, AUR, and macOS signing/notary secrets.
  - It should not run for the fork's npm release tags.
- `.github/workflows/build.yml` runs on pushes to `main` and pull requests; the fork's default branch is expected to be `taoeffect`, so release-related workflows should not assume only `main`.
- New fork-specific release workflow should be added as `.github/workflows/release-taoeffect.yml` and use GitHub-hosted runners.

## Intended npm package design

- Template files should live in `npm/`:
  - `package.json`
  - `install.js`
  - `run-crush.js`
  - `lib.js`
- Published package should contain:
  - `package.json`
  - `install.js`
  - `run-crush.js`
  - `lib.js`
  - `README.md`
  - `LICENSE.md`
- `package.json` should use:
  - `name`: `@taoeffects/crush`
  - `type`: `module`
  - `version`: template `0.0.0`, replaced during package generation
  - `bin.crush`: `run-crush.js`
  - dependencies: `proxy-agent` and `tar`
- Package should include generated `crush.archives` metadata keyed by npm platform keys:
  - `linux-x64`
  - `linux-arm64`
  - `darwin-x64`
  - `darwin-arm64`

## Intended release archive design

- Initial supported platforms:
  - Linux x64: `GOOS=linux`, `GOARCH=amd64`, archive suffix `Linux_x86_64`, npm key `linux-x64`
  - Linux arm64: `GOOS=linux`, `GOARCH=arm64`, archive suffix `Linux_arm64`, npm key `linux-arm64`
  - macOS Intel: `GOOS=darwin`, `GOARCH=amd64`, archive suffix `Darwin_x86_64`, npm key `darwin-x64`
  - macOS Apple Silicon: `GOOS=darwin`, `GOARCH=arm64`, archive suffix `Darwin_arm64`, npm key `darwin-arm64`
- Archives should be named:
  - `crush_${VERSION}_Linux_x86_64.tar.gz`
  - `crush_${VERSION}_Linux_arm64.tar.gz`
  - `crush_${VERSION}_Darwin_x86_64.tar.gz`
  - `crush_${VERSION}_Darwin_arm64.tar.gz`
- Each archive should contain a wrapper directory with the same basename and include:
  - `crush`
  - `README.md`
  - `LICENSE.md`
- `dist/checksums.txt` should contain SHA-256 sums for all archives.

## Intended installer behavior

- `install.js` should:
  1. Map `process.platform` and `process.arch` to one of the supported npm platform keys.
  2. Read generated archive metadata from `package.json`.
  3. Download the matching archive from the GitHub Release URL in metadata.
  4. Verify its SHA-256 digest.
  5. Extract the wrapped `crush` binary into `vendor/crush`.
  6. Make the binary executable on Unix-like systems.
  7. Fail clearly on unsupported platforms, missing metadata, download failures, checksum mismatches, or missing extracted binary.
- `run-crush.js` should resolve `vendor/crush/crush`, spawn it with forwarded arguments and stdio, and exit with the same code or signal.

## Intended package generation behavior

- Add `scripts/generate-npm-package.mjs`.
- The script should:
  1. Accept or read `VERSION`, `TAG`, `DIST_DIR`, and output package directory.
  2. Read `dist/checksums.txt`.
  3. Copy npm template files to a temporary release package directory.
  4. Copy `README.md` and `LICENSE.md`.
  5. Replace `package.json` version.
  6. Add archive metadata for the four supported platforms.
  7. Run or support `npm pack --dry-run` as a sanity check in workflow.

## Intended trusted publishing flow

- First manual publication is required by the user before npm trusted publishing can be configured for this package.
- The repo can prepare everything needed to generate a package and run `npm publish --access public` manually.
- After the first manual publication, user configures npm trusted publishing for package `@taoeffects/crush`, repository `taoeffect/crush`, workflow `.github/workflows/release-taoeffect.yml`, and tag refs like `v*.*.*` if npm offers tag restrictions.
- Trusted publishing workflow needs `permissions: id-token: write` and must not require `NPM_TOKEN`.

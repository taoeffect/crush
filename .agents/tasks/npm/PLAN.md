# Plan

## Objective

Prepare the repository so the `taoeffect/crush` fork can publish `@taoeffects/crush` to npm using a small wrapper package and GitHub Release assets, with an initial manual npm publish path followed by npm trusted publishing.

## Scope

This task covers repository changes only. External account/repository actions remain user-owned:

- Enabling GitHub Actions on `taoeffect/crush`.
- Owning or creating the `@taoeffects` npm scope/package.
- Performing the first manual npm publish using npm credentials.
- Configuring npm trusted publishing after the package exists.
- Pushing release tags and confirming published package behavior on npm.

## Implementation plan

### 1. Correct and document npm scope/trusted-publishing requirements

- Update `.agents/notes/NPM_RELEASE_PLAN.md` to consistently refer to the `@taoeffects` scope for `@taoeffects/crush`.
- Add concise manual-first publishing notes if needed:
  - Build GitHub Release assets first.
  - Generate the npm package locally or in CI.
  - Publish the generated package with `npm publish --access public`.
  - Then configure trusted publishing for subsequent releases.

### 2. Add npm wrapper package templates

Create `npm/` with:

- `package.json`
- `install.js`
- `run-crush.js`
- `lib.js`

Key requirements:

- Package name: `@taoeffects/crush`.
- Use ESM (`"type": "module"`).
- Expose `bin.crush` via `run-crush.js`.
- Include only expected published files.
- Support platform keys `linux-x64`, `linux-arm64`, `darwin-x64`, `darwin-arm64`.
- Download release archives from metadata generated into `package.json`.
- Verify SHA-256 checksums before extraction.
- Extract only the expected wrapped binary path.
- Avoid unsafe path traversal during archive extraction.
- Produce clear failures for unsupported platforms, missing metadata, download errors, checksum mismatches, and missing binaries.

### 3. Add npm package generation script

Create `scripts/generate-npm-package.mjs` to:

- Accept command-line arguments and/or env vars for:
  - version
  - tag
  - dist directory
  - output package directory
  - repository, defaulting to `taoeffect/crush`
- Read `checksums.txt` from the dist directory.
- Copy npm template files, `README.md`, and `LICENSE.md` into the output package directory.
- Set `package.json.version` to the release version without leading `v`.
- Generate `package.json.crush.repo` and `package.json.crush.archives` metadata.
- Validate that every expected archive exists in the checksum file.
- Fail clearly on malformed input.

### 4. Add fork-specific release workflow

Create `.github/workflows/release-taoeffect.yml` that:

- Triggers on semantic version tags `v*.*.*` and `workflow_dispatch`.
- Uses GitHub-hosted runners.
- Sets permissions:
  - `contents: write`
  - `id-token: write`
- Runs tests or at least `go test ./...` before release jobs.
- Cross-compiles four Linux/macOS release archives on Ubuntu with:
  - `CGO_ENABLED=0`
  - `GOEXPERIMENT=greenteagc`
  - `-trimpath`
  - `-ldflags "-s -w -X github.com/charmbracelet/crush/internal/version.Version=${VERSION}"`
- Packages archives with wrapper directories containing `crush`, `README.md`, and `LICENSE.md`.
- Generates `dist/checksums.txt`.
- Creates a GitHub Release and uploads archives plus checksums.
- Generates the npm package into `.release/npm-package`.
- Runs `npm pack --dry-run` in the generated package.
- Publishes to npm with `npm publish --access public` using trusted publishing after the first manual package publish has been completed.

### 5. Prevent upstream release workflow conflicts

Adjust `.github/workflows/release.yml` so fork tags do not run the Charm-owned GoReleaser workflow. Preferred surgical approach:

- Keep the file present for upstream compatibility.
- Add a repository guard such as `if: github.repository == 'charmbracelet/crush'` to the GoReleaser job.

This lets `taoeffect/crush` use `release-taoeffect.yml` without triggering upstream release infrastructure.

### 6. Verification

Run targeted checks after implementation:

- Node package generation against a synthetic local `dist/checksums.txt` and placeholder archives.
- `npm pack --dry-run` on the generated package.
- `node --check` or equivalent syntax validation for JavaScript files.
- Relevant workflow/script shell checks where practical.
- Go formatting/build/test if Go files are touched; otherwise skip broad Go tests unless workflow changes need confidence.

## Notes

- Do not publish, tag, commit, or push as part of this task unless explicitly requested.
- Do not store npm tokens or credentials in the repository.
- The first manual npm publish remains a user action because it requires npm account access.
- Trusted publishing can only be activated after the package exists on npm.

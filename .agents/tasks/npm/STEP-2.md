# Add npm wrapper package templates

Status: COMPLETED

## Sub tasks

1. [x] Create `npm/package.json` template
2. [x] Create shared npm package helper module `npm/lib.js`
3. [x] Create secure download/verify/extract installer `npm/install.js`
4. [x] Create binary runner `npm/run-crush.js`
5. [x] Run syntax checks for npm template JavaScript files
6. [x] Mark TODO 2 complete after verification

## NOTES

Starting from task design in `.agents/tasks/npm/CURRENT_DESIGN.md`. No existing `npm/` files were present.

Completed changes:

- Added `npm/package.json` for `@taoeffects/crush` with ESM, `postinstall`, `bin.crush`, package metadata, dependencies, and publish file allow-list.
- Added `npm/lib.js` with platform mapping, package metadata loading, checksum helpers, vendor path handling, executable chmod, and archive path validation.
- Added `npm/install.js` to download the metadata-selected archive through proxy-aware fetch, verify SHA-256, extract only the expected wrapped binary, chmod it, and fail clearly on errors.
- Added `npm/run-crush.js` to execute the installed binary with forwarded args/stdio and propagate exit status or signal.
- Verified syntax with `node --check` for all three JavaScript files.

# Correct npm scope and manual-first trusted publishing notes in the release plan

Status: COMPLETED

## Sub tasks

1. [x] Review existing release-plan sections that mention npm scope, trusted publishing, and publish order
2. [x] Correct the npm scope from `@taoeffect` to `@taoeffects`
3. [x] Document the manual-first publish requirement before trusted publishing
4. [x] Mark TODO 1 complete after verification

## NOTES

Started from `.agents/notes/NPM_RELEASE_PLAN.md` and task context in `.agents/tasks/npm/`.

Completed changes:

- Corrected npm scope references from `@taoeffect` to `@taoeffects`.
- Added the requirement that `@taoeffects/crush` must be manually published once before npm trusted publishing can be configured.
- Updated Phase 3 ordering so first manual publish comes before trusted publishing or token-based CI publishing.
- Verified no remaining literal ``@taoeffect` `` scope references in `.agents/notes/NPM_RELEASE_PLAN.md`.

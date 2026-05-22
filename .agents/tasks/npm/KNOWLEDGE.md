# Knowledge

- Task name: `npm`.
- User wants repository task files for implementing npm release support for `@taoeffects/crush` with a manual first publish, then npm trusted publishing.
- Do not commit, push, tag, publish to npm, or configure external services unless explicitly asked.
- Package scope is `@taoeffects`, not `@taoeffect`.
- Trusted publishing requires the package to exist first; user must do the first manual `npm publish --access public` with their npm account.
- Existing upstream `.github/workflows/release.yml` uses Charm-owned GoReleaser infrastructure and should not run on fork tags.

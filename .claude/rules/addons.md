---
paths:
  - "internal/addon/**/*.go"
  - "cmd/root.go"
---

When adding a new addon:

1. Implement the `internal.Addon` interface and subscribe to events via `gookit/event` — no direct calls between addons.
2. Register it in `addonRegistry` in `cmd/root.go` (name → constructor).
3. Decide: mandatory (add to `mandatoryAddons`) or configurable (add to the appropriate `addons.normal`/`addons.security` defaults in `.drupdater.yaml` and document in CLAUDE.md).
4. Unknown names in the active addon list abort the run — make sure the name in `addonRegistry` matches exactly what users put in `.drupdater.yaml`.

---
paths:
  - "internal/addon/**/*.go"
  - "cmd/root.go"
---

When adding a new addon:

1. Implement the `internal.Addon` interface and subscribe to events via `gookit/event` — no direct calls between addons.
2. Register it in `addonRegistry` in `cmd/root.go` (name → constructor).
3. Decide: mandatory (add to `mandatoryAddons`) or configurable (add to `defaultNormalAddons` in `internal/configfile.go` if it should run by default).
4. Unknown names in the active addon list abort the run — make sure the name in `addonRegistry` matches exactly what users put in `.drupdater.yaml`.
5. Add `ReportKey()`/`ReportData()` in `internal/addon/report.go` — **not** in the addon's own file. Return `nil` when there is nothing to report, and sort anything unordered so reports stay byte-stable.
6. Document it: a new page at `docs/reference/addons/<name>.md`, a row in the catalogue table in `docs/reference/addons/index.md`, an entry in `mkdocs.yml`'s nav, and the addon list in `docs/reference/configuration.md`.

The full procedure, including event/priority guidance and a checklist, is in `docs/contributing/writing-an-addon.md`.

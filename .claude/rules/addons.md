---
paths:
  - "internal/addon/**/*.go"
  - "cmd/addons.go"
---

When adding a new addon:

1. Implement the `workflow.Addon` interface and return its hooks from `Hooks()`, each at a stage — no direct calls between addons.
2. Declare, in its own file, an interface per tool it calls with only the methods it calls (e.g. `AuditComposer`), and add it to `addonComposer`/`addonDrush` in `cmd/addons.go`. Never share one tool interface between addons.
3. Register it in the `factories` of `builtinAddons()` in `cmd/addons.go` (name → constructor).
4. Decide: mandatory (add to its `mandatory` list) or configurable (add to `defaultNormalAddons` in `internal/config/file.go` if it should run by default).
5. Unknown names in the active addon list abort the run — make sure the name in `builtinAddons()` matches exactly what users put in `.drupdater.yaml`.
6. Add `ReportKey()`/`ReportData()` in `internal/addon/report.go` — **not** in the addon's own file. Return `nil` when there is nothing to report, and sort anything unordered so reports stay byte-stable. Then add the addon to `reportingAddons()` in `internal/addon/report_golden_test.go` and regenerate with `go test ./internal/addon -update`, so its section is pinned like every other one.
7. Document it: a new page at `docs/reference/addons/<name>.md`, a row in the catalogue table in `docs/reference/addons/index.md`, an entry in `mkdocs.yml`'s nav, and the addon list in `docs/reference/configuration.md`.

The full procedure, including event/stage guidance and a checklist, is in `docs/contributing/writing-an-addon.md`.

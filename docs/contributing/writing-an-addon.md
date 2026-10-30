# Writing an addon

An addon is a unit of work that hooks into a [workflow
event](../explanation/addon-architecture.md) and does one job when it fires. Adding a
capability to Drupdater almost always means adding an addon rather than touching the
workflow.

## The rules

**Addons never call each other.** If your addon needs another one to have run first,
express that as the stage of its hook, not as a reference. If it needs to influence what another
addon does, and the event is `pre-composer-update`, return an [update
plan](../explanation/addon-architecture.md#steering-the-update-through-proposals) from the
hook. Your addon proposes. It never edits what another addon reads.

**Addons swallow their own failures** where the work is optional. A transient failure in
translation updates should not throw away an otherwise complete dependency update. This is
also exactly why your addon must report to the run report — see step 4.

## 1. Implement the interface

Create `internal/addon/your_addon.go`. An addon hands the workflow its hooks and renders a
description section:

```go
package addon

import (
	"context"

	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal/workflow"
)

// YourAddonComposer is the part of composer YourAddon uses.
type YourAddonComposer interface {
	IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error)
}

type YourAddon struct {
	logger   *zap.Logger
	composer YourAddonComposer
}

func NewYourAddon(logger *zap.Logger, composer YourAddonComposer) *YourAddon {
	return &YourAddon{logger: logger, composer: composer}
}

func (a *YourAddon) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostCodeUpdate: workflow.On(workflow.Change, a.postCodeUpdate),
	}
}

func (a *YourAddon) postCodeUpdate(evt *workflow.PostCodeUpdateEvent) error {
	// Change files under evt.Path(), then name them; false means nothing changed.
	_, err := evt.Commit("Describe the change", "web/modules/custom")
	return err
}
```

The `workflow.Addon` interface is the whole contract. An addon renders nothing itself: what
it found reaches the pull request through its [report section](#4-report). An addon that changes files commits
them by naming them to the event, never through git itself — see [Committing
changes](../explanation/addon-architecture.md#committing-changes).

Declare the interface for each tool your addon calls in its own file, listing only the methods
it calls, as above. Its constructor then shows what the addon can do to the project, and its
tests mock exactly that. Then add the interface to `addonComposer` or `addonDrush` in
`cmd/addons.go`: those are the sum of every addon's own interface, and what `*composer.CLI` and
`*drush.CLI` have to satisfy. Run `make mock` to generate the mock.

### Choosing an event and stage

| Event | Use for |
|---|---|
| `pre-composer-update` | Influencing what the update touches: build the hook with `workflow.Propose` and return a plan |
| `post-composer-update` | Reacting to the new lock file and installed code, before it is committed |
| `post-code-update` | Changing code — Rector, PHPCBF, and similar |
| `pre-site-update` | Inspecting a site before its update hooks run |
| `post-site-update` | Per-site work after hooks, before configuration export |

Pick the stage from what the hook does: `Prepare` if later hooks on the event read what it
writes, `Change` if it edits the project, `Format` if it restyles what `Change` wrote, and
`Inspect` if it only observes, so it records the settled state. Read
[Stages](../explanation/addon-architecture.md#stages) before picking one, and add your
addon to `TestAddonHookStages`.

### Concurrency

`pre-site-update` and `post-site-update` fire **once per site, from a goroutine per site**.
`pre-site-update` hooks run concurrently. `post-site-update` hooks run one site at a time,
in `--sites` order, because they may write into the shared working tree, but still from
different goroutines. Keep what either kind of hook finds in a `workflow.PerSite[T]` field: the hook calls `Set(evt.Site(), value)` for its
own site, and `ReportData` reads `All()`, a copy, and merges the sites — deduplicating, say,
as `unsupported_modules` does. The lock lives inside `PerSite`, so there is none to forget.

An addon with a per-site hook may not have a map, slice or mutex field at all:
`TestPerSiteHooksShareStateOnlyThroughPerSite` fails if it does. Add the addon to
`internal/addon/concurrency_test.go` as well, which fires its hook from many sites at once.

## 2. Register it

In `cmd/addons.go`, add it to the `factories` of `builtinAddons()`:

```go
"your_addon": func(d addonDeps) workflow.Addon { return addon.NewYourAddon(d.logger) },
```

The registry key is exactly what users write in `.drupdater.yaml`. An unknown name aborts
a run, so a mismatch here is a user-visible bug.

## 3. Decide mandatory or configurable

**Mandatory** — add the name to the `mandatory` list of `builtinAddons()`. Reserve this for addons that are
required for the update to succeed at all, or that produce the changelog a reviewer needs.
It is a high bar: a mandatory addon cannot be turned off by any project.

**Configurable** — the default. If it should run in a normal update by default, add it to
`defaultNormalAddons` in `internal/config/file.go`.

Think carefully before adding anything to the **security** defaults. That list is empty on
purpose: a security update should be a minimal, focused fix.

Then update the [configuration reference](../reference/configuration.md) and add a page
under [addon reference](../reference/addons/index.md).

## 4. Report

Implement two methods in `internal/addon/report.go` — **not** in your addon's own file:

```go
func (a *YourAddon) ReportKey() string { return "your_addon" }

func (a *YourAddon) ReportData() any {
	if len(a.results) == 0 {
		return nil // omitted from the report entirely
	}
	return a.results
}
```

They live together in one file because collectively they *are* the report's `addons`
schema, which is a published contract. Scattered across ten files, it is very easy to
rename a field without noticing a consumer depends on it.

Three requirements:

- **The key must match the registry name**, so a report reads the way the project is
  configured.
- **Return `nil` when there is nothing to report** — an omitted key is better than an
  empty one.
- **Report even when the work was "only" a code change.** Because addons swallow their own
  failures, one that silently broke looks exactly like one with nothing to do. This is not
  hypothetical: it [has happened](../explanation/why-a-run-report.md).
- **Sort anything unordered.** Two runs over unchanged input should produce byte-identical
  output so reports diff cleanly.

If your addon records a skip, record **why** — see
[`translations_updater`](../reference/addons/translations-updater.md) for the pattern.

## 5. Add a description section

The pull request description is rendered from the report by `internal/mergerequest`, so an
addon appears in it through its report section. To give it one:

1. Add `internal/mergerequest/templates/your_addon.go.tmpl`.
2. In `internal/mergerequest/mergerequest.go`, read your section into `sections` — declaring
   only the fields the template shows, by their JSON names — and add it to
   `sectionsInOrder`, where the order of the description is decided.
3. Add your addon to `reportingAddons()` in `internal/addon/report_golden_test.go`, and
   regenerate both goldens: `go test ./internal/addon ./internal/mergerequest -update`.

The renderer reads the report through its JSON, like any consumer, so it can only show what
your section publishes. Follow the conventions in [Development
setup](development.md#description-templates). The section's golden file in
`internal/mergerequest/testdata/` is embedded straight into the [addon
reference](../reference/addons/index.md), so a good one becomes the published example.

## 6. Test it

Changed packages need **≥ 90% coverage**. Existing addon tests show the pattern: construct
the addon with mocks, call its hook with an event, assert on both the side effects and
`ReportData()`.

When your addon depends on another one's hook running first, test the two through a
`workflow.NewPipeline` rather than calling the hooks yourself: `workflow.Fire(p, event)` (or
`workflow.Settle` for pre-composer-update) runs them in the order a real run would, so the test
fails if a stage changes.

Then verify end to end against a real project:

```bash
go run . --working-dir /path/to/a/drupal/checkout --dry-run --verbose --report ./report.json
jq '.addons.your_addon' report.json
```

Check the rendered description too — `jq -r '.merge_request_description' report.json`.
Template output is easy to get subtly wrong, and `--dry-run` still renders it.

## Checklist

- [ ] Implements the addon interface, each hook at a justified stage, listed in `TestAddonHookStages`
- [ ] Its own interface per tool, with only the methods it calls, added to `addonComposer` or `addonDrush`
- [ ] Registered in `builtinAddons()` under the name users will write
- [ ] Mandatory or configurable decided, and defaults updated if needed
- [ ] `ReportKey`/`ReportData` in `internal/addon/report.go`, returning `nil` when empty
- [ ] Output sorted for deterministic reports
- [ ] Description section, if any, rendered from the report section and pinned by a golden
- [ ] Per-site findings kept in a `workflow.PerSite`, if it hooks into a site event
- [ ] Template and golden file, if it appears in the request
- [ ] Tests, package at ≥ 90% coverage
- [ ] Documented: a page under `docs/reference/addons/`, listed in the catalogue table, and
      added to the [configuration reference](../reference/configuration.md)

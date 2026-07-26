# Writing an addon

An addon is a unit of work that subscribes to a [workflow
event](../explanation/addon-architecture.md) and does one job when it fires. Adding a
capability to Drupdater almost always means adding an addon rather than touching the
workflow.

## The rules

**Addons never call each other.** If your addon needs another one to have run first,
express that as event priority, not as a reference. If it needs to influence what another
addon does, do it through the mutable event payload.

**Addons swallow their own failures** where the work is optional. A transient failure in
translation updates should not throw away an otherwise complete dependency update. This is
also exactly why your addon must report to the run report — see step 4.

## 1. Implement the interface

Create `internal/addon/your_addon.go`. An addon needs to subscribe to events and render a
description section:

```go
package addon

import (
	"github.com/gookit/event"
	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
)

type YourAddon struct {
	internal.BasicAddon
	logger *zap.Logger
	// injected dependencies — a Composer or Drush wrapper, etc.
}

func NewYourAddon(logger *zap.Logger) *YourAddon {
	return &YourAddon{logger: logger}
}

func (a *YourAddon) SubscribedEvents() map[string]any {
	return map[string]any{
		"post-code-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(a.OnPostCodeUpdate),
		},
	}
}

func (a *YourAddon) OnPostCodeUpdate(e event.Event) error {
	evt := e.(*services.PostCodeUpdateEvent)
	// evt.Context(), evt.Path(), evt.Worktree()
	return nil
}

func (a *YourAddon) RenderTemplate() (string, error) {
	// Return "" when there is nothing to say — no empty sections in the request.
	return "", nil
}
```

Embedding `BasicAddon` provides `Render(name, data)` for template rendering.

### Choosing an event and priority

| Event | Use for |
|---|---|
| `pre-composer-update` | Influencing what the update touches (mutable payload) |
| `post-composer-update` | Reacting to the new lock file, before it is committed |
| `post-code-update` | Changing code — Rector, PHPCBF, and similar |
| `pre-site-update` | Inspecting a site before its update hooks run |
| `post-site-update` | Per-site work after hooks, before configuration export |
| `pre-merge-request-create` | Changing the request title (mutable payload) |

Priority is only meaningful relative to the other subscribers on the same event, and it is
load-bearing on two of them. Read [why priority
matters](../explanation/addon-architecture.md#why-priority-matters) before picking one.

Use the lowest priority for an addon that only observes, so it records the settled state.

### Concurrency

`pre-site-update` and `post-site-update` fire **once per site, concurrently**. Any state
your addon accumulates across sites must be mutex-guarded, and any map you hand to the
report must be a copy — see the existing per-site addons for the pattern.

## 2. Register it

In `cmd/root.go`, add it to `addonRegistry`:

```go
"your_addon": func(d addonDeps) internal.Addon { return addon.NewYourAddon(d.logger) },
```

The registry key is exactly what users write in `.drupdater.yaml`. An unknown name aborts
a run, so a mismatch here is a user-visible bug.

## 3. Decide mandatory or configurable

**Mandatory** — add the name to `mandatoryAddons`. Reserve this for addons that are
required for the update to succeed at all, or that produce the changelog a reviewer needs.
It is a high bar: a mandatory addon cannot be turned off by any project.

**Configurable** — the default. If it should run in a normal update by default, add it to
`defaultNormalAddons` in `internal/configfile.go`.

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

## 5. Add a description template

If your addon should appear in the pull request, add
`internal/addon/templates/your_addon.go.tmpl` and render it from `RenderTemplate`.

Follow the conventions in [Development
setup](development.md#description-templates), and add a golden file in
`internal/addon/testdata/` — those files are embedded straight into the [addon
reference](../reference/addons/index.md), so a good one becomes the published example.

## 6. Test it

Changed packages need **≥ 90% coverage**. Existing addon tests show the pattern: construct
the addon with mocks, fire the event, assert on both the side effects and `ReportData()`.

Then verify end to end against a real project:

```bash
go run . --working-dir /path/to/a/drupal/checkout --dry-run --verbose --report ./report.json
jq '.addons.your_addon' report.json
```

Check the rendered description too — `RenderTemplate` output is easy to get subtly wrong,
and `--dry-run` still generates it.

## Checklist

- [ ] Implements the addon interface, subscribes at a justified priority
- [ ] Registered in `addonRegistry` under the name users will write
- [ ] Mandatory or configurable decided, and defaults updated if needed
- [ ] `ReportKey`/`ReportData` in `internal/addon/report.go`, returning `nil` when empty
- [ ] Output sorted for deterministic reports
- [ ] Per-site state mutex-guarded, if it subscribes to a site event
- [ ] Template and golden file, if it appears in the request
- [ ] Tests, package at ≥ 90% coverage
- [ ] Documented: a page under `docs/reference/addons/`, listed in the catalogue table, and
      added to the [configuration reference](../reference/configuration.md)

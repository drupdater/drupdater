# Run report

`--report <path>` writes a JSON document describing the run.

It is written on **every** outcome — including a run that failed halfway and a `--dry-run`
that never opened a pull request — so a failing repository leaves behind something better
than a log to read. See [Why a run report](../explanation/why-a-run-report.md) for the
reasoning, and [Consume the run report](../how-to/consume-the-run-report.md) for
assertions you can build on it.

```bash
drupdater --dry-run --report ./drupdater-report.json
```

## Example

```json
{
  "schema_version": 1,
  "drupdater_version": "v0.3.6",
  "started_at": "2026-07-25T02:00:00Z",
  "finished_at": "2026-07-25T02:14:31Z",
  "duration_seconds": 871.4,
  "status": "success",
  "mode": "security",
  "dry_run": false,
  "repository": "https://github.com/org/site.git",
  "base_branch": "main",
  "update_branch": "update-3f81a2c",
  "merge_request": {
    "url": "https://github.com/org/site/pull/42",
    "auto_merge": { "enabled": true }
  },
  "sites": ["default"],
  "packages": [
    { "action": "Upgrade", "package": "drupal/core", "from": "10.1.8", "to": "10.2.0" }
  ],
  "phases": [
    { "name": "composer install", "started_at": "…", "duration_seconds": 63.2, "ok": true },
    { "name": "baseline site install", "started_at": "…", "duration_seconds": 121.9, "ok": true }
  ],
  "addons": {
    "composer_audit": { "fixed": [], "remaining": [] },
    "update_hooks": { "default": {} }
  }
}
```

## Fields

| Field | Type | Notes |
|---|---|---|
| `schema_version` | int | Currently `1`. See [schema stability](#schema-stability) |
| `drupdater_version` | string | The build version. **`"dev"` in the published Docker images** — see [version information](cli/index.md#version-information) |
| `started_at` | timestamp | RFC 3339 |
| `finished_at` | timestamp | RFC 3339 |
| `duration_seconds` | float | Wall-clock duration of the whole run |
| `status` | string | `success`, `no_changes` or `failed` — see below |
| `failed_phase` | string | Present only on failure: which phase returned the error |
| `error` | string | Present only on failure: the error message |
| `mode` | string | `normal` or `security` |
| `dry_run` | bool | Whether `--dry-run` was passed |
| `repository` | string | The repository URL, with any embedded credentials stripped |
| `base_branch` | string | The branch the request targets |
| `update_branch` | string | The branch created, omitted if the run never got that far |
| `merge_request` | object or `null` | `null` when none was created — a dry run, or a failure |
| `sites` | list of strings | The configured sites |
| `packages` | list of objects | Every dependency change |
| `phases` | list of objects | Every phase with its duration and outcome |
| `addons` | object | One section per addon that had something to report |

### `status`

| Value | Meaning |
|---|---|
| `success` | Every phase the run attempted completed. A `--dry-run` that stopped before pushing is a success. |
| `no_changes` | The run worked and found nothing to update. Reported separately so an up-to-date site is not mistaken for a broken one. |
| `failed` | A phase returned an error. `failed_phase` and `error` say which and why. |

`no_changes` covers every "nothing to do" path: `composer update` produced no changes, the
update branch already exists, or a `--security` run found no advisories. All exit `0`.

When a run resolves to `no_changes`, the phase that raised the abort **stays in the
`phases` list** with `ok: false`, while the top-level `failed_phase` and `error` are
cleared. You can see where it stopped without the run being reported as a failure.

### `merge_request`

```json
{
  "url": "https://github.com/org/site/pull/42",
  "auto_merge": { "enabled": false, "error": "auto-merge is not enabled for this repository" }
}
```

`auto_merge` is present **only** when the active run type requested it, so "never
requested" is distinguishable from "requested and failed". A failure sets `enabled: false`
with an `error` while the run itself still reports `success` — which is exactly why the
field exists rather than only a log line. See [Enable
auto-merge](../how-to/enable-auto-merge.md).

### `packages`

```json
{ "action": "Upgrade", "package": "drupal/core", "from": "10.1.8", "to": "10.2.0" }
```

`action` is one of `Install`, `Upgrade`, `Downgrade` or `Remove`. `from` is absent on an
install; `to` is absent on a removal.

### `phases`

```json
{ "name": "composer install", "started_at": "…", "duration_seconds": 63.2, "ok": true }
```

Adds `error` when `ok` is `false`. The phase names, in order:

| Phase | What it covers |
|---|---|
| `acquire working copy` | Opening the checkout, or cloning |
| `preflight` | Git history depth and PHP platform requirements |
| `composer install` | Installing the current dependency tree |
| `baseline site install` | Installing each site at the old code |
| `update shared code` | `composer update`, addon events, commits, branch creation |
| `site update` | Update hooks and configuration export per site |
| `publish` | Push and open the request — absent under `--dry-run` |

Recording durations here makes the cost of a run measurable without separate
instrumentation: the phase distribution shows whether a run is dominated by `composer
install`, by site installs, or by Rector.

Only the **first** failure sets the top-level `status`, `failed_phase` and `error`.

### `addons`

One section per addon that has something to report, keyed by the same names used in
[`.drupdater.yaml`](configuration.md), so a report reads the way the project is
configured.

| Key | Shape |
|---|---|
| [`composer_audit`](addons/composer-audit.md) | `{ fixed: [...], remaining: [...] }` |
| [`update_hooks`](addons/update-hooks.md) | `{ <site>: { <hook>: {...} } }` |
| [`unsupported_modules`](addons/unsupported-modules.md) | `[ {...} ]`, sorted by name |
| [`composer_patches`](addons/composer-patches.md) | `{ removed: [...], updated: [...], conflicts: [...] }` |
| [`code_beautifier`](addons/code-beautifier.md) | `{ files: [...], fixable: <int> }` |
| [`deprecations_remover`](addons/deprecations-remover.md) | `[ { file, applied_rectors } ]` |
| [`translations_updater`](addons/translations-updater.md) | `{ <site>: { path, updated, skipped } }` |

Addons with nothing to say are **omitted** rather than present and empty.
[`composer_diff`](addons/composer-diff.md) and
[`composer_normalizer`](addons/composer-normalizer.md) never appear at all — see those
pages for why.

## Check report

`--report` applies to [`drupdater check`](cli/check.md) too, where it writes a different
document. A preflight has no phases, packages or branch, so it gets its own shape rather
than a run report with most fields empty.

```bash
drupdater check --report ./preflight.json
```

```json
{
  "schema_version": 1,
  "drupdater_version": "v0.3.6",
  "checked_at": "2026-07-25T02:00:00Z",
  "ok": false,
  "results": [
    { "name": "git history complete (not a shallow clone)", "ok": true },
    {
      "name": "site \"default\": settings.php",
      "ok": false,
      "detail": "not found at web/sites/default/settings.php"
    }
  ]
}
```

`ok` is `false` if any individual result is. `detail` is present only on a failure. See
[Preflight checks](preflight-checks.md) for the full list of check names.

## Credentials

Credentials never appear in the report. The repository URL is stripped of any embedded
userinfo, and the finished document passes through the same redactor that filters the
logs — applied to the whole serialised JSON rather than to individual fields, so a
credential arriving through an unexpected field (an error string quoting an authenticated
URL, say) is caught too.

## Writing behaviour

The report is written to a temporary file in the destination directory and then atomically
renamed, so a poller never observes a partial document. Parent directories are created as
needed.

**A write failure is logged and swallowed.** If the path is unwritable, the run still
succeeds or fails on its own merits — the report describes the run, it is not the run.

## Schema stability

`schema_version` is part of the contract:

- New fields may be added **without** bumping it. **Consumers must ignore unknown
  fields.**
- Removing or renaming a field increments it.

!!! note "Pre-1.0"

    While Drupdater is pre-1.0, the schema may still gain fields between minor versions.
    It will not silently rename or drop them.

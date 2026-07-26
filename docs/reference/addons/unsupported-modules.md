# `unsupported_modules`

Reports installed modules that have reached end of life — no supported release, no
upgrade path, no further fixes.

| | |
|---|---|
| Runs | Configurable — in the `normal` defaults, not in `security` |
| Events | `pre-site-update` (Normal) |
| Report key | `unsupported_modules` |
| Pull request section | "⚠️ Unsupported modules" |

## What it does

Queries Drupal's own update status for each site, via a helper script shipped in the
Docker image, and collects modules whose status is `NOT_SUPPORTED`.

Results are deduplicated by module name across sites and sorted, so a multi-site project
does not list the same module several times.

This addon is **best-effort**: errors are logged and swallowed rather than failing the
run. A transient problem reaching Drupal.org's update service should not lose an
otherwise complete update.

## Why it exists

An unsupported module is a different problem from a vulnerable one. There is no update to
apply, so [`composer_audit`](composer-audit.md) will never flag it and no amount of
running Drupdater will fix it. It needs a human decision: replace the module, adopt it,
or accept the risk.

Surfacing it in the routine update pull request is the cheapest possible way to keep that
decision from being forgotten for a year.

## Report section

```json
{
  "addons": {
    "unsupported_modules": [
      { "name": "module_a", "installed_version": "1.0.0", "recommended_version": "None" },
      { "name": "module_b", "installed_version": "2.3.1", "recommended_version": "3.0.0" }
    ]
  }
}
```

Sorted by name, so two runs over an unchanged site produce byte-identical output and
reports can be diffed directly.

A `recommended_version` of `None` means there is no supported release at all. A concrete
version means a supported release exists but the installed major is unsupported — an
upgrade path exists, it just is not one Drupdater can take automatically.

Omitted when every installed module is supported.

## Pull request section

--8<-- "internal/addon/testdata/unsupported_modules.md"

## A note on silent failure

This addon was once broken for months without any run going red — the reason the report
records it even when it finds nothing, and the reason to assert on its report key rather
than on the run's `status`. See [Why a run
report](../../explanation/why-a-run-report.md).

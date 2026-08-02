# `unsupported_modules`

Reports everything in the project that is no longer maintained — modules with no supported
release, and packages their maintainers have abandoned — as one list.

| | |
|---|---|
| Runs | **Always.** Mandatory in both modes, not something you put in `.drupdater.yaml` |
| Events | `pre-site-update` (Normal), `pre-merge-request-create` (BelowNormal) |
| Report key | `unsupported_modules` |
| Pull request section | "⚠️ Unsupported modules and abandoned packages" |

## What it does

### Unsupported modules

Queries Drupal's own update status for each site, via a helper script shipped in the
Docker image, and collects modules whose status is `NOT_SUPPORTED`.

Results are deduplicated by module name across sites, so a multi-site project does not
list the same module several times.

### Abandoned packages

On `pre-merge-request-create`, [`composer_audit`](composer-audit.md) puts the packages
Composer reported as abandoned on the event, and this addon renders them in the same table.
Priority is what makes that work: `composer_audit` publishes at Normal, this addon reads at
BelowNormal.

They come from Packagist rather than Drupal.org and cover the non-Drupal libraries this addon
cannot see — but they say the same thing to a reviewer, so they belong in the same list rather
than in two sections that differ only by which addon found them. `drupal/*` packages are
filtered out on the `composer_audit` side, so no module is listed twice.

Every row is sorted by name, so two runs over an unchanged project produce byte-identical
output.

The Drupal.org half is **best-effort**: errors are logged and swallowed rather than
failing the run. A transient problem reaching Drupal.org's update service should not lose
an otherwise complete update.

## Why it exists

Something unmaintained is a different problem from something vulnerable. There is no update
to apply, so [`composer_audit`](composer-audit.md) will never raise an advisory for it and no
amount of running Drupdater will fix it. It needs a human decision: replace it, adopt it, or
accept the risk.

Surfacing that in the routine update pull request is the cheapest possible way to keep the
decision from being forgotten for a year.

It covers both halves of the project because either alone is misleading. Drupal.org knows
about `drupal/*` and nothing else; Packagist's abandonment flag covers the ordinary PHP
libraries a Drupal site is equally full of. A list that silently omitted one of them would
read like a complete answer.

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

The report keeps the two sources apart even though the pull request merges them: the
abandoned packages are under [`composer_audit`](composer-audit.md#report-section), next to the
advisories from the same audit. A report is read by machine and organised by producer; a pull
request is read by a person and organised by topic.

A `recommended_version` of `None` means there is no supported release at all. A concrete
version means a supported release exists but the installed major is unsupported — an
upgrade path exists, it just is not one Drupdater can take automatically.

Omitted when every installed module is supported. An abandoned package does not add this
section — it appears under `composer_audit` — but it does add a row to the pull request
table.

## Pull request section

--8<-- "internal/addon/testdata/unsupported_modules.md"

## A note on silent failure

This addon was once broken for months without any run going red — the reason the report
records it even when it finds nothing, and the reason to assert on its report key rather
than on the run's `status`. See [Why a run
report](../../explanation/why-a-run-report.md).

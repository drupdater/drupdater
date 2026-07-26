# `composer_audit`

Drives a security run: decides which packages the update is allowed to touch, verifies
afterwards which advisories were actually resolved, and retitles the pull request.

| | |
|---|---|
| Runs | **Automatically whenever `--security` is passed.** Not listed by `drupdater addons` and not something you put in `.drupdater.yaml` |
| Events | `pre-composer-update` (**Max**), `post-code-update` (BelowNormal), `pre-merge-request-create` (Normal) |
| Report key | `composer_audit` |
| Pull request section | "🛡️ Security Report" |

## What it does

### Before the update — deciding the scope

Runs `composer audit` and collects the packages with known advisories. It then sets, on
the `pre-composer-update` event:

- **`PackagesToUpdate`** — only the affected packages. When `drupal/core` is affected,
  `drupal/core-recommended` and `drupal/core-composer-scaffold` are added too, since core
  cannot move without them.
- **`MinimalChanges: true`** — Composer is asked to disturb as little else as possible.

It subscribes at the **highest** priority on this event precisely because it decides the
scope that every other addon then works within.

!!! info "No advisories means no run"

    If the audit finds nothing, the addon aborts the run with `No security advisories
    found`. This is a **success**: the process exits `0` and the [run
    report](../run-report.md) records `status: "no_changes"`. A nightly security job on a
    healthy site does nothing and stays green.

### After the code update — verifying the fix

Re-runs the audit against the updated tree and computes which advisories were resolved,
matching on CVE, then advisory ID, then package plus title. The result is the `fixed` and
`remaining` split.

It runs at a **below-normal** priority on `post-code-update`, so it sees the final code —
after Rector and PHPCBF have finished.

### Before the pull request — retitling

Rewrites the request title from the default monthly form to:

```text
2026-07-26: Drupal Security Updates
```

Dated to the day rather than the month, because security updates are expected to arrive
several times in one month and each needs to be distinguishable.

## Why it exists

It is what makes a security run different from a normal one. Without it, `--security`
would just be a normal update with a different pull request title.

## Report section

```json
{
  "addons": {
    "composer_audit": {
      "fixed": [
        {
          "advisoryId": "PKSA-1234-5678-9012",
          "packageName": "drupal/core",
          "affectedVersions": ">=10.1.0,<10.1.8",
          "title": "Drupal core - Moderately critical - Cross Site Scripting",
          "cve": "CVE-2026-12345",
          "severity": "moderate",
          "link": "https://www.drupal.org/sa-core-2026-001",
          "reportedAt": "2026-07-01T00:00:00+00:00"
        }
      ],
      "remaining": []
    }
  }
}
```

`remaining` lists advisories the update could not resolve — usually because the fix
requires a major version bump, or because a [patch conflict](composer-patches.md) held the
package back.

## Pull request section

--8<-- "internal/addon/testdata/composer_audit.md"

## Running it outside security mode

`composer_audit` is in the addon registry, so it is technically a valid name in
`.drupdater.yaml`. It is deliberately excluded from the [`drupdater
addons`](../cli/addons.md) listing because enabling it on a normal run is rarely what you
want: it would restrict a routine update to only the vulnerable packages, which is the
job `--security` already does properly.

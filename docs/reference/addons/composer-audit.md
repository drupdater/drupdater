# `composer_audit`

Runs `composer audit` and reports what it found. On a `--security` run it also drives the
update: it decides which packages may be touched, and retitles the pull request.

| | |
|---|---|
| Runs | **Always.** Mandatory in both modes, not something you put in `.drupdater.yaml` |
| Events | `pre-composer-update` (**Max**), `post-code-update` (BelowNormal), `pre-merge-request-create` (Normal) |
| Report key | `composer_audit` |
| Pull request section | "🛡️ Security Report", plus the abandoned packages in [`unsupported_modules`](unsupported-modules.md)' section |

## Normal run versus security run

The audit itself runs either way. What changes is how much authority it has:

| | Normal run | `--security` |
|---|---|---|
| Audits before and after the update | ✅ | ✅ |
| Reports fixed and remaining advisories | ✅ | ✅ |
| Contributes abandoned packages | ✅ | ✅ |
| Narrows the update to the vulnerable packages | — | ✅ |
| Aborts when there is nothing to fix | — | ✅ |
| Relabels the pull request | — | ✅ |

A normal run therefore gets a security report for free — a monthly maintenance update that
happens to close a CVE says so — without the audit dictating what that update does.

## What it does

### Before the update — deciding the scope

**`--security` only.** Runs `composer audit` and collects the packages with known
advisories. It then sets, on the `pre-composer-update` event:

- **`PackagesToUpdate`** — only the affected packages. When `drupal/core` is affected,
  `drupal/core-recommended` and `drupal/core-composer-scaffold` are added too, since core
  cannot move without them.
- **`MinimalChanges: true`** — Composer is asked to disturb as little else as possible.

It subscribes at the **highest** priority on this event precisely because it decides the
scope that every other addon then works within.

On a normal run this step records the audit and stops there: the scope of a maintenance
update is the user's to decide.

!!! info "No advisories means no run"

    On a `--security` run, if the audit finds nothing the addon aborts with `No security
    advisories found`. This is a **success**: the process exits `0` and the [run
    report](../run-report.md) records `status: "no_changes"`. A nightly security job on a
    healthy site does nothing and stays green.

### After the code update — verifying the fix

Re-runs the audit against the updated tree and computes which advisories were resolved,
matching on CVE, then advisory ID, then package plus title. The result is the `fixed` and
`remaining` split.

It runs at a **below-normal** priority on `post-code-update`, so it sees the final code —
after Rector and PHPCBF have finished.

### After the code update — abandoned packages

`composer audit` reports more than advisories: its JSON output also lists the packages whose
maintainers have marked them abandoned, together with the replacement they suggested when
there is one. The same command is already being run, so this costs nothing extra, and it
covers ground nothing else does — [`unsupported_modules`](unsupported-modules.md) reads
Drupal.org, so it sees `drupal/*` and nothing else, while a Drupal project's lock file is full
of ordinary PHP libraries.

The list is taken from the audit **after** the update, like the remaining advisories, so it
describes the code the pull request actually contains.

It is **not rendered in this addon's section.** On `pre-merge-request-create` the list is put
on the event, and [`unsupported_modules`](unsupported-modules.md) — which subscribes at a lower
priority — renders it in the same table as the unsupported modules. An abandoned package and
an unsupported module are the same finding to a reviewer, and which addon found one is not a
reason to put them in different sections. The [run report](../run-report.md) keeps `abandoned`
here, under the audit that produced it.

!!! info "`drupal/*` packages are excluded"

    A module can be both abandoned on Packagist and unsupported on Drupal.org. Those are one
    finding, not two, so `drupal/*` packages are dropped and left to
    [`unsupported_modules`](unsupported-modules.md), which has Drupal.org's own release data
    and can name a recommended version. Only the `drupal/` vendor is filtered — an unrelated
    vendor such as `drupalfinder/` is still reported.

!!! tip "Silencing it"

    drupdater does not pass `--abandoned`, so Composer applies the project's own
    [`audit.abandoned`](https://getcomposer.org/doc/06-config.md#abandoned) setting. Setting
    it to `ignore` in the project's `composer.json` makes Composer report no abandoned
    packages, and drupdater then has none to show:

    ```bash
    composer config audit.abandoned ignore
    ```

    That also silences Composer's own audit failures for abandoned packages, everywhere. There
    is no drupdater-side switch for this — the project's Composer configuration is the single
    place it is decided.

### Before the pull request — retitling

**`--security` only.** Rewrites the request title from the default monthly form to:

```text
2026-07-26: Drupal Security Updates
```

Dated to the day rather than the month, because security updates are expected to arrive
several times in one month and each needs to be distinguishable.

## Why it exists

It is what makes a security run different from a normal one: without it, `--security` would
just be a normal update with a different pull request title.

It runs on normal updates too because the audit answers a second question that has nothing to
do with `--security` — what is the security and maintenance posture of the code this pull
request contains? That answer is worth having on every update, and it is where the abandoned
packages come from.

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
      "remaining": [],
      "abandoned": [
        {
          "packageName": "patchwork/jsqueeze",
          "replacement": ""
        },
        {
          "packageName": "swiftmailer/swiftmailer",
          "replacement": "symfony/mailer"
        }
      ]
    }
  }
}
```

`remaining` lists advisories the update could not resolve — usually because the fix
requires a major version bump, or because a [patch conflict](composer-patches.md) held the
package back.

`abandoned` lists the abandoned packages, sorted by name so two reports of an unchanged
project are byte-identical. `replacement` is the successor the maintainers suggested, and is
`""` when they suggested none.

Omitted entirely when the run found no advisories at all, which on a normal update is the
common case.

## Pull request section

--8<-- "internal/addon/testdata/composer_audit.md"

Rendered only when there is at least one advisory to show, so a routine update on a healthy
project carries no security section rather than one saying there was nothing to report.

The abandoned packages appear in
[`unsupported_modules`' section](unsupported-modules.md#pull-request-section) instead.

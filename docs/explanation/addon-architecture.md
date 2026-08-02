# The addon architecture

Nearly everything Drupdater does beyond `composer update` itself is an addon: a unit that
subscribes to workflow events and does one job when its event fires.

## Why events rather than a pipeline

The obvious design is a list of steps the workflow calls in order. Drupdater uses an event
bus instead, and the difference matters in one specific way: **addons never call each
other.**

`code_beautifier` does not know `deprecations_remover` exists. `composer_patches` does not
know whether `composer_audit` is running. Each subscribes to a moment in the workflow and
reacts to whatever state it finds.

The payoff is that the set of addons is genuinely configurable. A project can run five of
them or none, in either of two run types, and no addon needs a code path for "what if the
other one is disabled" — because none of them ever depended on another being there.

## The events

| Event | Fired | Mutable payload |
|---|---|---|
| `pre-composer-update` | Before `composer update` | `PackagesToUpdate`, `PackagesToKeep`, `MinimalChanges` |
| `post-composer-update` | After the update, before the commit | — |
| `post-code-update` | After `composer.json`/`.lock` are committed | — |
| `pre-site-update` | Before each site's update hooks | — (carries the site name) |
| `post-site-update` | After each site's hooks, before config export | — (carries the site name) |
| `pre-merge-request-create` | While the request is rendered — under `--dry-run` too | `Title` |

All but the last carry the run context, the working directory path and the git worktree,
so an addon can read files and stage changes without being handed a service.

## Steering the update through the payload

Two events carry **mutable** fields, and that is how addons influence the workflow without
calling into it.

On `pre-composer-update`:

- **`PackagesToUpdate`** — restrict the update to a specific set.
  [`composer_audit`](../reference/addons/composer-audit.md) sets this to the packages with
  advisories on a `--security` run, which is what makes it a security run. On a normal run
  it leaves the field alone.
- **`PackagesToKeep`** — pin packages at their current version.
  [`composer_patches`](../reference/addons/composer-patches.md) adds a package here when
  its patch cannot be made to apply against the new version.
- **`MinimalChanges`** — ask Composer to disturb as little else as possible.

On `pre-merge-request-create`:

- **`Title`** is rewritten by `composer_audit` from the monthly maintenance form to a dated
  security form, on a `--security` run only.
- **`AbandonedPackages`** is written by `composer_audit` and read by
  [`unsupported_modules`](../reference/addons/unsupported-modules.md), which renders it in
  the same table as the modules Drupal.org reports as unsupported.

Addons can therefore cooperate — the audit narrows the update and the patch handler pins one
of the packages within it; the audit finds an abandoned package and another addon renders it
— without either one referencing the other. They both just write to the same event.

`AbandonedPackages` is the first payload field used to hand *findings* rather than
*instructions* between addons. The same rule applies: the producer never knows who reads it,
and the consumer works fine when nobody wrote anything.

## Why priority matters

Addons on the same event run in priority order, and for two events that order is load
bearing rather than incidental.

### `pre-composer-update`

`composer_audit` runs at the **highest** priority, because on a security run it decides the
scope everything else works within. `composer_patches` then evaluates patches against an update whose shape
is already fixed. Reversed, the patch handler would analyse an update far larger than the
one that actually runs.

### `post-code-update`

The order is:

1. `deprecations_remover` (above normal)
2. `code_beautifier` (normal)
3. `composer_audit` (below normal)

`deprecations_remover` temporarily installs Rector and then removes it again. That
`composer.json` churn must be committed **before** `code_beautifier` stages anything —
otherwise a dependency change would be swept into a commit labelled "Update coding
styles", which is both wrong and confusing to review.

`composer_audit` runs last on this event because its job here is to verify which
advisories the update actually resolved. It must see the final code, after Rector and
PHPCBF have finished.

### `pre-merge-request-create`

`composer_audit` publishes at **normal** and `unsupported_modules` reads at **below normal**.
Equal priorities would leave the order to the dispatcher, and the abandoned packages would
sometimes be rendered before they were written — an empty table half the time, which is worse
than a wrong one because nothing looks broken.

The two addons that only observe — [`composer_diff`](../reference/addons/composer-diff.md)
and [`update_hooks`](../reference/addons/update-hooks.md) — run at the **lowest** priority
on their events, so they record the settled state.

## Mandatory versus configurable

Six addons always run:

- `composer_allow_plugins` and `composer_patches` are **required for the update to succeed
  at all** — Composer would prompt for plugin approval, or fail on a stale patch.
- `composer_diff` and `update_hooks` **are the changelog** the reviewer needs.
- `composer_audit` and `unsupported_modules` are the project's **"no longer maintained"
  report**, and neither is complete alone: one reads Packagist, the other Drupal.org. They
  render their findings as one list, which only works if both run on every update.

`composer_audit` is also what makes `--security` mean anything; it is handed the flag rather
than being switched on by it, so that a normal run still gets the audit's report without the
audit taking over the update.

The rest are listed per run type in [`.drupdater.yaml`](../reference/configuration.md), and
an unknown name aborts the run rather than being ignored. A typo that silently disabled an
addon would look exactly like an addon with nothing to do.

## Rendering the request description

Each addon can render a markdown section via a Go template, and the workflow concatenates
them in addon order into the request description.

An addon with nothing to say returns an empty string and contributes no section — which is
why a request for a small update is short rather than full of empty headings.

Templates are embedded in the binary, so the rendered output is fixed at build time and
cannot be affected by the project being updated.

## Reporting

Separately from the description, an addon may contribute a section to the [run
report](../reference/run-report.md) by implementing two methods. This is satisfied
structurally, so no addon imports the report package — an addon that does not implement
them is simply absent from the report.

All of these methods live together in one file rather than beside each addon's other code.
Together they *are* the report's `addons` schema, which is a published contract; scattered
across nine files, it is very easy to rename a field without noticing that a consumer
depends on it.

See [Why a run report](why-a-run-report.md).

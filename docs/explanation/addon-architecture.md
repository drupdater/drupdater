# The addon architecture

Nearly everything Drupdater does beyond `composer update` itself is an addon: a unit that
hooks into workflow events and does one job when its event fires.

## Why events rather than a pipeline

The obvious design is a list of steps the workflow calls in order. Drupdater has each addon
hand the workflow its hooks instead, and the difference matters in one specific way:
**addons never call each other.**

`code_beautifier` does not know `deprecations_remover` exists. `composer_patches` does not
know whether `composer_audit` is running. Each hooks into a moment in the workflow and
reacts to whatever state it finds.

The payoff is that the set of addons is genuinely configurable. A project can run five of
them or none, in either of two run types, and no addon needs a code path for "what if the
other one is disabled" — because none of them ever depended on another being there.

## The events

| Event | Fired | Payload |
|---|---|---|
| `pre-composer-update` | Before `composer update`, which only writes the lock — before any site is installed | The update plan, read-only; hooks return proposals |
| `post-composer-update` | After the resolved update is installed into `vendor/`, before the commit | — |
| `post-code-update` | After `composer.json`/`.lock` are committed | — |
| `pre-site-update` | Before each site's update hooks, concurrently across sites | The site name and its facts |
| `post-site-update` | After every site's hooks, before its config export; one site at a time, in `--sites` order | The site name and its facts |

A site's facts — config sync directory, translation path, enabled modules, pending update hooks,
unsupported modules — are read from the site, so an addon asks the event rather than Drush.
`pre-site-update` carries them as they were before the update hooks ran; `post-site-update`
carries them read again afterwards, since a hook can enable a module or move a path, and
without the unsupported modules, which only `pre-site-update` asks drupal.org for. Each call to
`Facts()` returns a copy, so no hook can change what the next one reads.

Each carries the run context, the working directory path, and a way to commit: see
[Committing changes](#committing-changes).

There is no event for the merge request. It is rendered from the [run
report](../reference/run-report.md) once every addon has finished — see [Rendering the
request description](#rendering-the-request-description).

## Steering the update through proposals

One event lets addons influence the workflow without calling into it. A
`pre-composer-update` hook reads the **update plan** from the event and *returns* a
proposal. It never edits the event.

A plan has three parts:

- **`Only`**: restrict the update to these packages. Empty means every package.
  [`composer_audit`](../reference/addons/composer-audit.md) proposes the packages with
  advisories on a `--security` run, which is what makes it a security run. On a normal run
  it proposes nothing.
- **`Keep`**: pin packages at a version.
  [`composer_patches`](../reference/addons/composer-patches.md) proposes a pin when a
  package's patch cannot be made to apply against the new version.
- **`MinimalChanges`**: ask Composer to disturb as little else as possible.

The runner merges the proposals by fixed rules:

| Part | Rule | Disagreement |
|---|---|---|
| `Only` | Restrictions intersect: a proposal can narrow the update, never widen it | Two restrictions with no package in common fail the run |
| `Keep` | Pins accumulate | One package pinned at two versions fails the run |
| `MinimalChanges` | Set if any proposal sets it | — |

These rules don't depend on order: merging A then B gives the same plan as B then A. Each
hook sees the plan as the [earlier stages](#stages) settled it. Hooks that share a stage
see the same plan, so neither sees the other's proposal. The proposals of a stage are
merged before the next stage starts.

Addons can therefore cooperate without either one referencing the other. The audit
narrows the update at `Prepare`. The patch handler, at `Change`, checks its patches against
that narrower update and proposes a pin for one of the packages in it.

Findings never travel this way. An addon that finds something reports it, and anything that
combines two addons' findings does so from the report: see [Rendering the request
description](#rendering-the-request-description).

## Committing changes

Addons never touch git. A hook that changes files returns them as `Commits` in its
`workflow.Result`, and the pipeline commits exactly those paths — files or directories,
deletions included — under each message, in order, before the next hook runs. A later stage
therefore sees an earlier stage's change already committed.

Two rules make that safe without any addon knowing about the others:

- **Paths without a change are not committed.** No commit is made, and the hook's finding
  records which of its commits were, so an addon need not check first whether its tool
  changed anything.
- **A commit never carries someone else's change.** If anything outside the named paths is
  already staged, the commit is refused with an error naming it. A half-staged change from
  one addon cannot end up under another's message, whatever order they ran in.

Changes nobody names stay in the working tree, uncommitted.

## Stages

Each hook names a **stage**, and the stages run in a fixed order on every event:

| Stage | For a hook that |
|---|---|
| `Prepare` | produces what the event's later hooks read |
| `Change` | edits the project |
| `Format` | rewrites what `Change` wrote into the project's style |
| `Inspect` | reads the settled state and changes nothing |

Hooks sharing a stage run in addon order. A stage says *why* a hook runs where it does,
which a number cannot: an ordering dependency between two addons is visible in the stages
they declare, and a new addon picks its stage from what it does rather than from where the
others happen to sit.

| Event | Prepare | Change | Format | Inspect |
|---|---|---|---|---|
| `pre-composer-update` | `composer_audit` | `composer_allow_plugins`, `composer_patches` | | |
| `post-composer-update` | | `composer_allow_plugins` | `composer_normalizer` | `composer_diff` |
| `post-code-update` | | `deprecations_remover` | `code_beautifier` | `composer_audit` |
| `pre-site-update` | | | | `unsupported_modules`, `update_hooks` |
| `post-site-update` | | `translations_updater` | | |

Two of those orders are load bearing rather than incidental.

### `pre-composer-update`

`composer_audit` prepares, because on a security run it decides the scope everything else
works within. `composer_patches` then evaluates patches against an update whose shape is
already fixed: at `Change`, its hook reads the plan with the audit's restriction merged in.
Reversed, the patch handler would analyse an update far larger than the one that actually
runs.

### `post-code-update`

`deprecations_remover` changes the code and `code_beautifier` formats it, so PHPCBF puts
Rector's output right: Rector does not write to Drupal's coding standard. Commit hygiene
does not depend on this order — each addon commits only the paths it names (see
[Committing changes](#committing-changes)).

`composer_audit` inspects last on this event because its job here is to verify which
advisories the update actually resolved. It must see the final code, after Rector and
PHPCBF have finished.

## Mandatory versus configurable

Six addons always run:

- `composer_allow_plugins` and `composer_patches` are **required for the update to succeed
  at all** — Composer would prompt for plugin approval, or fail on a stale patch.
- `composer_diff` and `update_hooks` **are the changelog** the reviewer needs.
- `composer_audit` and `unsupported_modules` are the project's **"no longer maintained"
  report**, and neither is complete alone: one reads Packagist, the other Drupal.org. The
  request description shows their findings as one list, which only works if both run on
  every update.

`composer_audit` is also what makes `--security` mean anything; it is handed the flag rather
than being switched on by it, so that a normal run still gets the audit's report without the
audit taking over the update.

The rest are listed per run type in [`.drupdater.yaml`](../reference/configuration.md), and
an unknown name aborts the run rather than being ignored. A typo that silently disabled an
addon would look exactly like an addon with nothing to do.

## Rendering the request description

Addons do not render anything. An addon that has something to tell a reviewer contributes
a section to the [run report](../reference/run-report.md), and once every addon has
finished, the title and description are rendered from that report by
`internal/mergerequest`.

The renderer reads the report the way any consumer does: through its JSON. So the
description cannot say anything the report does not publish, the two cannot disagree, and
a `--dry-run` report shows exactly what a real run would put in the request — the report
records the rendered title and description too.

It also means findings from two addons can be combined without either knowing about the
other. The end-of-life table joins `unsupported_modules`'s section with the abandoned
packages in `composer_audit`'s; the title follows the report's `mode`.

A section with nothing in it is absent from the report, and so from the description —
which is why a request for a small update is short rather than full of empty headings. The
sections appear in a fixed order: what needs a decision first, the logs last.

Templates are embedded in the binary, so the rendered output is fixed at build time and
cannot be affected by the project being updated. An addon built outside Drupdater brings its
own template by implementing `Describer`; it is rendered against that addon's section as read
back from the report's JSON, so the same rule holds — it can only show what it published. If the description would exceed GitHub's
65,536-byte limit, it is rendered again without the dependency table's links.

## Reporting

An addon contributes its report section by implementing two methods. This is satisfied
structurally, so no addon imports the report package — an addon that does not implement
them is simply absent from the report, and from the request description.

All of these methods live together in one file rather than beside each addon's other code.
Together they *are* the report's `addons` schema, which is a published contract; scattered
across nine files, it is very easy to rename a field without noticing that a consumer
depends on it.

See [Why a run report](why-a-run-report.md).

## Addons outside Drupdater

The addon contract — `Addon`, `Hooks`, the events, stages, hook builders and findings — is
re-exported by the public `sdk` package, so an addon can live in another module and be
compiled into a binary of its own with `sdk.Main`. Drupdater's own `main.go` is
`sdk.Main()` with nothing registered: a custom binary differs only by its registrations.

Re-exported rather than moved, because the runner drives hooks through unexported fields
and methods; aliases give an external addon exactly the types the builtin ones use without
handing it the runner's controls.

An external addon is always configurable — mandatory means "the update or its changelog
needs it", which only Drupdater's own addons can claim. Its name may not shadow another's,
and its report key must be its name, so a report still reads the way the project is
configured. It gets composer and drush only through a generic `Exec`: the typed tool
methods are written for the builtin addons and change with them.

See [Build an addon outside Drupdater](../how-to/build-an-external-addon.md).

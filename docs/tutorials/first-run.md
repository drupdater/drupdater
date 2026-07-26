# Your first run

In this tutorial you will run Drupdater against a Drupal project on your own machine,
watch what it does, and read the report it produces — without changing anything remote and
without needing a token.

By the end you will know what a real run would have pushed, and be ready to [put it in
CI](scheduled-updates.md).

**Time:** about 15 minutes, most of it waiting for Composer.

## What you need

- Docker.
- A Drupal project on GitHub or GitLab that installs from configuration — that is,
  `drush site-install --existing-config` works on it. Your own project is ideal; if you
  do not have one to hand, `https://github.com/drupdater/test-drupal-cms.git` is the
  repository Drupdater's own integration tests run against.

You do **not** need a token for this tutorial.

## Step 1: check the project is ready

Before running an update, ask Drupdater whether the project meets its prerequisites. From
inside your checkout:

```bash
docker run -v "$(pwd)":/app -w /app \
  ghcr.io/drupdater/drupdater-php8.3:latest check
```

You should see something like:

```text
✓ .drupdater.yaml valid (sites: default)
✓ addon names resolve
✓ git history complete (not a shallow clone)
✓ PHP platform requirements satisfied
✓ site "default": settings.php
✓ repository host recognized (GitHub/GitLab)
```

Every line is near-instant — none of these checks install anything.

!!! tip "If a check fails"

    Each one and its fix is covered in [Preflight
    checks](../reference/preflight-checks.md). The two most common: a shallow clone
    (`git fetch --unshallow`) and the wrong PHP image (swap `php8.3` for the version your
    project needs).

`.drupdater.yaml valid` passes even if you have no such file — it is optional, and the
defaults apply. We will add one in the next tutorial.

## Step 2: do a dry run

Now the real thing, with `--dry-run` so nothing is pushed:

```bash
docker run -v "$(pwd)":/app -w /app \
  ghcr.io/drupdater/drupdater-php8.3:latest \
  --dry-run --verbose --report /app/drupdater-report.json
```

Notice there is no token in that command. In checkout mode a dry run neither reads from
nor writes to GitHub or GitLab, so it does not need one — Drupdater does not even
construct a client.

This takes several minutes. Composer installs the current dependency tree, then a Drupal
site is installed from scratch to build a baseline database, then the update runs.

## Step 3: read the log

The log narrates the work as it happens:

```text
INFO  using checkout        {"url": "https://github.com/you/site.git", "branch": "main"}
INFO  running composer install
INFO  updating dependencies
INFO  dependencies updated  {"total": 16, "installed": 2, "upgraded": 14, "downgraded": 0, "removed": 0}
INFO  removing deprecations
INFO  updating coding styles
INFO  updating site         {"site": "default"}
INFO  update hooks found    {"site": "default", "count": 4}
INFO  exporting configuration {"site": "default"}
INFO  update finished
```

The log does not announce the seven phases — those are recorded in the report, with their
durations, which you will read in the next step.

Nothing is pushed and no request is created: that is the `publish` phase, and `--dry-run`
skips it.

If instead you see:

```text
WARN  update aborted        {"error": "no changes detected"}
```

then the project is already fully up to date. That is a success, not a failure — the
process exits `0`. Skip ahead to step 4 and look at the `status` field.

## Step 4: read the report

The interesting artefact is the JSON report:

```bash
jq -r '.status, .mode, .dry_run' drupdater-report.json
```

```text
success
normal
true
```

What would have been updated:

```bash
jq -r '.packages[] | "\(.action) \(.package) \(.from // "-") → \(.to // "-")"' \
  drupdater-report.json
```

```text
Upgrade drupal/core 10.1.8 → 10.2.0
Upgrade drupal/token 1.12.0 → 1.13.0
Install drupal/coder - → 8.3.24
```

Where the time went:

```bash
jq -r '.phases[] | "\(.duration_seconds)s \(.name)"' drupdater-report.json | sort -rn
```

```text
121.9s baseline site install
63.2s composer install
41.7s update shared code
```

Which addons had something to say:

```bash
jq -r '.addons | keys[]' drupdater-report.json
```

```text
code_beautifier
deprecations_remover
translations_updater
unsupported_modules
update_hooks
```

Each of those is a [addon](../reference/addons/index.md) that ran and produced output.
Look at one:

```bash
jq '.addons.update_hooks' drupdater-report.json
```

Those are the database update hooks that would run on deploy — exactly the information a
reviewer needs.

!!! note "`no_changes` is not a failure"

    If `status` is `no_changes`, the run worked and found nothing to update. It exits `0`
    on purpose: a scheduled job should not turn red because a site is current.

## Step 5: look at what was built locally

A dry run still creates the branch and the commits locally — it only skips pushing them:

```bash
git branch --list 'update-*'
git log --oneline main..$(git branch --list 'update-*' | tr -d ' *')
```

```text
a1b2c3d Update translations
e4f5a6b Remove deprecations
c7d8e9f Update coding styles
0a1b2c3 Update composer.json and composer.lock
```

One commit per kind of change, which is what makes the resulting pull request reviewable
commit by commit.

The branch name — `update-3f81a2c` — is derived from a hash of the resulting
`composer.lock`. Run this again over unchanged dependencies and you will get the same name,
and the run will abort with `branch update-3f81a2c already exists, skipping`. That is
what stops duplicate pull requests.

## Step 6: clean up

```bash
git checkout main
git branch -D $(git branch --list 'update-*' | tr -d ' *')
rm drupdater-report.json
```

The SQLite database and private files directory Drupdater created beside your checkout are
already gone — checkout-mode runs remove them.

## What you learned

- `drupdater check` validates a project in about a second, and `--full` proves the site
  installs.
- A checkout-mode `--dry-run` is completely safe and needs no credentials.
- The run works through seven phases, and the report records each with its duration.
- The report distinguishes `success`, `no_changes` and `failed`.
- Update branches are content-addressed, so identical updates cannot duplicate.

## Next

[Scheduled updates in CI](scheduled-updates.md) — turn this into a pull request that
arrives every Monday.

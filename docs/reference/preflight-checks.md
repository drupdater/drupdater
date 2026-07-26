# Preflight checks

The checks [`drupdater check`](cli/check.md) performs, in the order it runs them. Two of
them also run inside a real update, as the `preflight` phase, so a shallow checkout is
caught identically either way.

See [Run preflight checks](../how-to/run-preflight-checks.md) for how to use them.

## Cheap tier

These run by default. None of them install anything, and the only network access is one
optional read-only API call.

### `.drupdater.yaml valid (sites: …)`

Loads and validates the configuration file. The site list is included in the check name,
so the output confirms which sites the rest of the run will operate on.

**On failure** the name is just `.drupdater.yaml valid` and the detail is the loader
error — a strict-decode failure on an unknown key, an unparseable `timeout`, an empty
`sites` list, or the [legacy layout error](../how-to/migrate-config-layout.md).

The remaining config-dependent checks still run, against the defaults, so one bad key does
not hide every other problem in the project.

### `addon names resolve`

Every addon name in **both** run type blocks is checked against the registry, regardless
of which mode is active.

**On failure:** `unknown addon "code_beautifer" (run "drupdater addons" to list valid
names)`.

### `git history complete (not a shallow clone)`

Verifies the working directory is not a shallow clone.

**Why it matters:** the update branch cannot be pushed from a shallow clone — the remote
needs the ancestry. Without this check the run would proceed through `composer install`,
a site install and the whole update before failing at push time with a cryptic `object not
found`.

**On failure:**

```text
shallow checkout detected: fetch full history (set "fetch-depth: 0" in GitHub Actions,
or GIT_DEPTH: "0" in GitLab CI)
```

Or, if the depth could not be determined at all: `could not determine clone depth: …`.

**Also runs in a real update**, as part of the `preflight` phase.

### `PHP platform requirements satisfied`

Runs Composer's platform requirement check: does the running PHP satisfy what
`composer.lock` demands?

**Why it matters:** almost always this means the wrong image was chosen — a project
requiring PHP 8.4 running in `drupdater-php8.3`. The detail is Composer's own output.

Extension requirements are deliberately **not** checked, only the PHP version itself.

**Also runs in a real update**, as part of the `preflight` phase.

### `site "<name>": settings.php`

Run once per configured site. Resolves the project's web root from
`extra.drupal-scaffold.locations.web-root` in `composer.json`, then checks that
`<web-root>/sites/<site>/settings.php` exists.

**Why it matters:** Drupdater appends its test database configuration to an existing
`settings.php`; it does not create one. A missing file means the site was never installed
in this checkout, so the baseline install, update hooks and configuration export all have
nothing to build on.

**On failure:** `not found at web/sites/default/settings.php`, or `could not determine web
root: …`.

### `repository host recognized (GitHub/GitLab)`

Resolves the repository URL — from `--repository-url` or the checkout's `origin` remote —
and confirms it parses into a host and an `owner/repo` path.

**On failure:**

```text
could not determine repository URL (pass --repository-url or run inside a checkout with
an origin remote)
```

Or the URL validation error. See [VCS provider
detection](../explanation/vcs-provider-detection.md).

### `token authenticates`

**Only runs when a token was supplied.** Builds the VCS client and asks the platform who
the token belongs to.

**On failure:** `did not authenticate, or lacks API access`.

Without a token this check is skipped entirely and its absence from the output is normal.

## Full tier

Added by `--full`. These cost most of a real run.

The repository is cloned to a **scratch directory** — never the live working copy — and
everything is removed afterwards, including the clone, each `<site>.sqlite` and
`private/<site>`.

### `clone for full check`

Appears only if cloning fails. The branch defaults to `main` when not otherwise set.

### `composer install`

A real `composer install` against the scratch clone. Catches unreachable private
registries, missing [`COMPOSER_AUTH`](../how-to/use-private-packagist.md), and lock files
that cannot be satisfied.

### `site "<name>": installs from configuration`

A real `drush site-install --existing-config` per site. **This is the definitive check**
— it is exactly what the update's baseline install phase does, and the single most common
reason a real run fails partway through.

## Exit status

Any failing check makes the command exit `1` with `preflight check failed`, so `check` can
gate a pipeline directly. With `--report`, the results are also written as a [check
report](run-report.md#check-report).

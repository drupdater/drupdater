# Troubleshoot a run

## Start here

Two commands answer most questions:

```bash
drupdater check              # is the project set up correctly?
drupdater check --full       # does the site really install from config?
```

And for a run that failed, `--verbose` plus a report:

```bash
drupdater --dry-run --verbose --report ./report.json
jq -r '"\(.failed_phase): \(.error)"' report.json
```

A dry run in checkout mode needs no token and touches nothing remote, so it is always safe
to repeat.

## Common failures

### Push fails with `object not found`

**Cause:** shallow checkout. The update branch cannot be pushed without full history.

**Fix:** `fetch-depth: 0` in GitHub Actions, `GIT_DEPTH: "0"` in GitLab CI. See
[GitHub Actions](github-actions.md) or [GitLab CI](gitlab-ci.md).

This is caught upfront by the `preflight` phase and by `drupdater check`, so you should
see the clearer message instead:

```text
shallow checkout detected: fetch full history (set "fetch-depth: 0" in GitHub Actions,
or GIT_DEPTH: "0" in GitLab CI)
```

### The pull request is created but CI does not run on it (GitHub)

**Cause:** expected behaviour with `GITHUB_TOKEN`. Pull requests opened with it do not
trigger other workflows.

**Fix:** use a personal access token or a GitHub App installation token. See [choosing a
token](github-actions.md#choosing-a-token).

### `composer install` fails on private packages

**Cause:** missing registry credentials.

**Fix:** provide [`COMPOSER_AUTH`](use-private-packagist.md).

### Site install fails

**Cause:** the site does not install from its exported configuration.

**Fix:** confirm `drush site-install --existing-config` works locally first. This is the
single most common reason a real run fails partway through, and `drupdater check --full`
reproduces it in isolation:

```text
✗ site "default" installs from configuration
```

### The run aborts on an unknown addon name

```text
unknown addon "code_beautifer" (run "drupdater addons" to list valid names)
```

**Fix:** typo in `.drupdater.yaml`. Run [`drupdater
addons`](../reference/cli/addons.md). Note that names in **both** run type blocks are
validated, so a typo in `security` fails a normal run too.

### `field addons not found in type internal.fileConfig`

**Cause:** an old-layout `.drupdater.yaml`, or a genuine typo in a key name.

**Fix:** see [Migrate the config layout](migrate-config-layout.md).

### `could not determine the target branch`

```text
could not determine the target branch: the checkout is in detached HEAD and no CI
branch variable (GITHUB_REF_NAME, CI_COMMIT_REF_NAME) is set
```

**Cause:** running against a detached checkout outside a recognised CI environment.

**Fix:** check out a branch, or set `GITHUB_REF_NAME` / `CI_COMMIT_REF_NAME` explicitly.
`--branch` will not help — it only applies with `--clone`.

### `no token provided`

**Cause:** the run will push or clone, and no token was given.

**Fix:** pass it as the argument or set `DRUPDATER_TOKEN`. Note that a checkout-mode
`--dry-run` needs no token at all — if you are only rehearsing, add `--dry-run`. See [when
a token is required](../reference/cli/index.md#when-a-token-is-required).

### Wrong PHP version

```text
✗ PHP platform requirements satisfied
```

**Fix:** pick the image variant matching the project —
`ghcr.io/drupdater/drupdater-php8.4` and so on. See [Docker
images](../reference/docker-images.md).

## Confusing but correct behaviour

### The run exits 0 and did nothing

Check the report's `status`. `no_changes` means the run worked and found nothing to
update — an up-to-date site, a `--security` run with no advisories, or an update branch
that already exists.

```bash
jq -r '.status' report.json
```

This exits `0` deliberately: a scheduled pipeline should not go red because a site was
already current.

### The branch name is the same as last time

Update branch names are content-addressed — derived from a hash of the resulting
`composer.lock`. A rerun over unchanged dependencies produces the same branch name, and
the run aborts with `branch <name> already exists, skipping`. That is the mechanism preventing
duplicate pull requests for identical updates.

### A package was not updated

Check for a patch conflict:

```bash
jq -r '.addons.composer_patches.conflicts[]?' report.json
```

A package whose patch no longer applies is pinned to its old version rather than updated.
See [Enable patch management](enable-patch-management.md) — with a Drupal.org token,
Drupdater can often repair the patch instead.

### An addon produced nothing

Most addons log and swallow their own failures, so a failure looks like "nothing to do".
Check the report:

```bash
jq -r '.addons | keys[]' report.json
```

An enabled addon that is **absent** either had nothing to report or failed silently — run
with `--verbose` to tell which.
[`composer_diff`](../reference/addons/composer-diff.md) and
[`composer_normalizer`](../reference/addons/composer-normalizer.md) are always absent by
design.

### Auto-merge did not happen

Auto-merge is best-effort and does not fail the run. Check:

```bash
jq '.merge_request.auto_merge' report.json
```

See [Enable auto-merge](enable-auto-merge.md).

## Getting help

Open a [discussion](https://github.com/drupdater/drupdater/discussions) or an
[issue](https://github.com/drupdater/drupdater/issues) with what you ran, what you
expected, what happened, and the `--verbose` output. **Redact any tokens** — though
Drupdater's own log redaction should already have done so.

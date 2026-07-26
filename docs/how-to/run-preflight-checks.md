# Run preflight checks

[`drupdater check`](../reference/cli/check.md) validates the things a real run depends on
without running `composer update`, creating a branch or opening a request. Use it before
scheduling a real run, and to gate a pipeline.

## Check a project before scheduling anything

From the checkout:

```bash
docker run -v "$(pwd)":/app -w /app \
  ghcr.io/drupdater/drupdater-php8.3:latest check
```

This runs only the cheap checks — they take about a second. Output:

```text
✓ .drupdater.yaml valid (sites: default)
✓ addon names resolve
✓ git history complete (not a shallow clone)
✓ PHP platform requirements satisfied
✓ site "default": settings.php
✓ repository host recognized (GitHub/GitLab)
```

`check` never modifies your checkout, and it never requires a token.

## Get the definitive answer

The cheap checks catch configuration problems. They cannot tell you whether the site
actually installs from its exported configuration — the single most common reason a real
run fails partway through. `--full` proves it:

```bash
docker run -v "$(pwd)":/app -w /app \
  ghcr.io/drupdater/drupdater-php8.3:latest \
  check --full
```

This clones the repository to a scratch directory, runs a real `composer install` and a
real `drush site-install --existing-config` per site, then removes everything it created.
It costs most of a real run, which is why it is opt-in.

Your live working copy is never touched, even with `--full`.

## Verify a token

Pass a token and one more check runs — whether it authenticates and has API access:

```bash
docker run -v "$(pwd)":/app -w /app -e DRUPDATER_TOKEN \
  ghcr.io/drupdater/drupdater-php8.3:latest check
```

```text
✓ repository host recognized (GitHub/GitLab)
✓ token authenticates
```

Without a token that check is silently skipped, which is the normal case in a pipeline
that has no credentials.

## Gate a pipeline

`check` exits non-zero if anything fails, so it works as a job on its own.

=== "GitHub Actions"

    ```yaml
    jobs:
      drupdater-check:
        runs-on: ubuntu-latest
        container:
          image: ghcr.io/drupdater/drupdater-php8.3:latest
        steps:
          - uses: actions/checkout@v4
            with:
              fetch-depth: 0
          - run: /opt/drupdater/bin check
    ```

=== "GitLab CI"

    ```yaml
    drupdater:check:
      image:
        name: ghcr.io/drupdater/drupdater-php8.3:latest
        entrypoint: [""]
      variables:
        GIT_DEPTH: "0"
      script:
        - /opt/drupdater/bin check
    ```

Running this on every merge request catches the moment someone shallow-clones the
pipeline or renames a site directory — before the next scheduled update run silently
starts failing.

## Get a machine-readable verdict

```bash
drupdater check --report ./preflight.json
```

Which writes a [check report](../reference/run-report.md#check-report):

```json
{
  "schema_version": 1,
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

Print just the failures:

```bash
drupdater check --report ./preflight.json || {
  jq -r '.results[] | select(.ok | not) | "\(.name): \(.detail)"' preflight.json
  exit 1
}
```

## Interpreting a failure

Each check and what its failure means is documented on the [Preflight
checks](../reference/preflight-checks.md) reference page. The most common:

| Failure | Fix |
|---|---|
| `git history complete` | Set `fetch-depth: 0` (GitHub) or `GIT_DEPTH: "0"` (GitLab) |
| `PHP platform requirements satisfied` | Wrong image — pick the `drupdater-php*` variant matching the project |
| `site "…": settings.php` | The site was never installed in this checkout, or `sites` in `.drupdater.yaml` names a directory that does not exist |
| `addon names resolve` | Typo in `.drupdater.yaml` — run [`drupdater addons`](../reference/cli/addons.md) |
| `token authenticates` | Token expired, or lacks API scope |

See also [Troubleshoot a run](troubleshoot.md).

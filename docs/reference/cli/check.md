# `drupdater check`

Validates the things a real run depends on, without running `composer update`, without
creating a branch, and without opening a merge or pull request.

```text
drupdater check [token] [flags]
```

By default only cheap, near-instant checks run: `.drupdater.yaml` and its addon names,
git history, PHP platform requirements, each site's `settings.php`, and — if a token is
given — that it authenticates. Pass `--full` to additionally clone the repository and
prove each site installs from its exported configuration.

Exits non-zero if any check fails, so it can gate a pipeline.

## Flags

`check` accepts every [persistent flag](drupdater.md#flags) of the root command, plus:

| Flag | Type | Default | Description |
|---|---|---|---|
| `--full` | bool | `false` | Additionally clone the repository and verify each site installs from its exported configuration (`drush site-install --existing-config`). Expensive: most of a real run's cost. |

The most useful inherited flags here are `--working-dir`, `--config` and `--report`.

## The token is optional

Unlike a real run, `check` never requires a token. Its absence simply skips the "token
authenticates" check; every other check still runs. That makes `check` safe to run in a
pipeline that has no credentials.

## What it checks

The full ordered list, with the exact check names as they appear in output and in the
`--report` document, is on the [Preflight checks](../preflight-checks.md) page.

In short:

1. `.drupdater.yaml valid (sites: …)`
2. `addon names resolve`
3. `git history complete (not a shallow clone)`
4. `PHP platform requirements satisfied`
5. `site "<name>": settings.php` — once per configured site
6. `repository host recognized (GitHub/GitLab)`
7. `token authenticates` — only when a token was given

With `--full`, three more are appended: `clone for full check`, `composer install`, and
`site "<name>": installs from configuration` per site.

## Safety

`check` never modifies your checkout. The `--full` tier clones to a scratch directory
rather than touching the live working copy, and removes the clone, each `<site>.sqlite`
and `private/<site>` afterwards.

## Output

Results are printed one per line, with failures detailed:

```text
✓ .drupdater.yaml valid (sites: default)
✓ addon names resolve
✓ git history complete (not a shallow clone)
✓ PHP platform requirements satisfied
✗ site "default": settings.php
    not found at web/sites/default/settings.php
✓ repository host recognized (GitHub/GitLab)
```

Failure details pass through the same redactor as the logs, so a message quoting an
authenticated URL will not leak a token.

## Machine-readable output

`--report` applies to `check` too, and writes a different document shape — a
[`Check` report](../run-report.md#check-report) rather than a run report:

```bash
drupdater check --report ./preflight.json
```

## Examples

Validate the current checkout:

```bash
docker run -v "$(pwd)":/app -w /app ghcr.io/drupdater/drupdater-php8.3:latest check
```

The definitive answer, including a real site install:

```bash
docker run -v "$(pwd)":/app -w /app ghcr.io/drupdater/drupdater-php8.3:latest \
  check --full "$DRUPDATER_TOKEN"
```

Gate a pipeline on a machine-readable verdict:

```bash
drupdater check --report ./preflight.json || {
  jq -r '.results[] | select(.ok | not) | "\(.name): \(.detail)"' preflight.json
  exit 1
}
```

See [Run preflight checks](../../how-to/run-preflight-checks.md) for the full workflow.

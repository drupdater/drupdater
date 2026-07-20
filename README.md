# Drupdater

> Automated, reviewable Drupal updates — as a pull/merge request, on every schedule.

[![CI](https://github.com/drupdater/drupdater/actions/workflows/go.yml/badge.svg)](https://github.com/drupdater/drupdater/actions/workflows/go.yml)
[![Docker](https://ghcr-badge.egpl.dev/drupdater/drupdater-php8.3/latest_tag?trim=major&label=docker)](https://github.com/drupdater/drupdater/pkgs/container/drupdater-php8.3)
[![License](https://img.shields.io/github/license/drupdater/drupdater)](LICENSE)
[![Release](https://img.shields.io/github/v/tag/drupdater/drupdater?label=release)](https://github.com/drupdater/drupdater/tags)

**Drupdater** is a standalone CLI (shipped as a Docker image) that keeps Drupal
sites up to date for you. Point it at a checkout in CI; it runs `composer update`,
applies code-quality fixes, exports Drupal config, and opens a **pull request
(GitHub) or merge request (GitLab)** with a detailed, human-reviewable changelog —
security changes flagged.

You review and merge. Drupdater never deploys anything on its own.

**Who it's for:** teams maintaining one or more Drupal sites who want routine and
security updates to arrive as reviewable PRs on a schedule, instead of as a manual
chore.

> [!WARNING]
> **Project status: pre-1.0 (`v0.x`).** Drupdater is in active development and
> used in real pipelines, but the CLI surface and config format may still change
> between minor versions. Pin to a specific image tag in production.

## Contents

- [How It Works](#how-it-works)
- [Quick Start](#quick-start)
- [Prerequisites](#prerequisites)
- [CI/CD Integration](#cicd-integration)
- [Tokens & Permissions](#tokens--permissions)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)
- [Architecture](#architecture)
- [Development](#development)
- [Contributing](#contributing)
- [Security](#security)
- [Support](#support)
- [License](#license)

## How It Works

Drupdater runs against the checkout your CI already provides and works through
four phases in a single working directory:

```mermaid
flowchart LR
    A[Acquire checkout<br/>+ composer install] --> B[Install site<br/>baseline DB]
    B --> C[composer update<br/>+ code fixes<br/>+ commit branch]
    C --> D[Run update hooks<br/>+ export config]
    D --> E[Open PR / MR<br/>with changelog]
```

1. **Acquire** the existing checkout (or `--clone` one for local testing) and run `composer install`.
2. **Install** each Drupal site from config to build a baseline database.
3. **Update shared code** — `composer update`, code-quality fixes, patch management, then commit to a new branch.
4. **Update each site** — run database update hooks and export configuration.

Finally it pushes the branch and opens a PR/MR (skipped with `--dry-run`).

### What it does in each run

- **Dependency updates** — `composer update`, committed.
- **Security-only mode** (`--security`) — updates only packages with known vulnerabilities.
- **Patch management** — drops obsolete patches, verifies remaining ones still apply, and pulls updated patch files from Drupal.org.
- **Code style** — `phpcbf`, auto-generating a `phpcs.xml` baseline if missing.
- **Deprecation removal** — `drupal-rector`.
- **Composer hygiene** — auto-allow-lists new Composer plugins; runs `composer normalize` when `ergebnis/composer-normalize` is present.
- **Translations** — updates interface translations via Drush when `locale_deploy` is enabled.
- **Changelog** — full dependency diff table and pending DB update hooks in the PR/MR description.
- **Multi-site** — updates several sites in one repo under a single PR/MR.
- **GitHub & GitLab** — including self-hosted GitLab.

## Quick Start

Try it locally against any GitHub/GitLab repo. Pick the image matching your site's
PHP version (`php8.3`, `php8.4`, `php8.5`):

```bash
docker run ghcr.io/drupdater/drupdater-php8.3:latest \
  <token> --clone --repository-url https://github.com/you/your-drupal-site.git
```

- `<token>` — a personal access token allowed to push branches and open PRs/MRs.
- `--clone --repository-url` — tells Drupdater to fetch the repo itself. In CI you
  omit these; it uses the checkout already on disk (see below).

Add `--dry-run` to do everything except create the branch and PR/MR.

## Prerequisites

- The site installs from configuration (`drush site-install --existing-config` works).
- The repo is hosted on GitHub or GitLab.
- **Full git history in the checkout** so the update branch can be pushed —
  `fetch-depth: 0` (GitHub Actions) or `GIT_DEPTH: "0"` (GitLab CI). Shallow
  checkouts fail with `object not found`.
- *(Optional)* A [Drupal.org GitLab access token](https://git.drupalcode.org)
  (`DRUPALCODE_ACCESS_TOKEN`) to enable patch management.

## CI/CD Integration

In CI, Drupdater runs against the checkout — no `--clone` needed. The recommended
setup is two scheduled jobs: a **weekly full update** and a **daily security-only
update**.

### GitLab CI

Run via [scheduled pipelines](https://docs.gitlab.com/ee/ci/pipelines/schedules.html).
Add a `DRUPDATER_SCHEDULE` variable per schedule to distinguish them.

```yaml
.drupdater_base:
  image:
    name: ghcr.io/drupdater/drupdater-php8.3:latest
    entrypoint: [""]
  variables:
    GIT_DEPTH: "0"  # full history required to push the update branch

drupdater:weekly:
  extends: .drupdater_base
  script:
    - /opt/drupdater/bin $DRUPDATER_TOKEN
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule" && $DRUPDATER_SCHEDULE == "weekly"

drupdater:security:
  extends: .drupdater_base
  script:
    - /opt/drupdater/bin $DRUPDATER_TOKEN --security
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule" && $DRUPDATER_SCHEDULE == "daily"
```

### GitHub Actions

**Weekly full update** (`.github/workflows/drupdater.yml`):

```yaml
name: Drupdater
on:
  schedule:
    - cron: "0 4 * * 1"   # Mondays 04:00 UTC
  workflow_dispatch:
permissions:
  contents: write
  pull-requests: write
jobs:
  drupdater:
    runs-on: ubuntu-latest
    container:
      image: ghcr.io/drupdater/drupdater-php8.3:latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0   # full history required to push the update branch
      - run: /opt/drupdater/bin ${{ secrets.GITHUB_TOKEN }}
```

For the **daily security update**, copy this into a second workflow file, change
the cron to `"0 4 * * *"`, and append `--security` to the run command.

## Tokens & Permissions

Drupdater needs a token that can **push branches** and **open PRs/MRs**.

| Platform | Token | Notes |
|----------|-------|-------|
| GitHub | `GITHUB_TOKEN` | Enough to push and open a PR. **But** PRs opened with `GITHUB_TOKEN` do not trigger other workflows, so CI won't run on the Drupdater PR. To get CI on the PR, use a [PAT](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens) or [GitHub App token](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app) stored as a secret. |
| GitLab | Project/Group access token or PAT | Needs `write_repository` and `api` scope. |

## Configuration

Configuration is split in two: **CLI flags** (how a run is invoked) and
**`.drupdater.yaml`** (what the project needs, committed to the repo).

### CLI flags

All flags are optional; pass them after the required `<token>`.

| Flag | Default | Description |
|------|---------|-------------|
| `--branch` | `main` | Branch to update / MR target. Only used with `--clone`; in checkout mode the branch comes from the checkout (or the CI branch variable in detached HEAD). |
| `--working-dir` | `.` | Path to the existing checkout to update in place. |
| `--clone` | `false` | Clone instead of using the checkout. Requires `--repository-url`. For local testing. |
| `--repository-url` | _(from `origin`)_ | Repo URL. Required with `--clone`; otherwise read from the `origin` remote. |
| `--security` | `false` | Update only packages with known vulnerabilities. Selects the `addons.security` list. |
| `--dry-run` | `false` | Run everything but skip branch and PR/MR creation. |
| `--verbose` | `false` | Debug-level logging (also logs resolved config). |
| `--config` | _(`<working-dir>/.drupdater.yaml`)_ | Path to the config file. |

### `.drupdater.yaml`

Optional file at the repo root. Missing file or omitted keys fall back to the
defaults below; unknown keys are rejected so typos fail fast.

```yaml
sites: [default]      # Drupal site directories to update
timeout: 30m          # overall run timeout (Go duration; 0 disables)
addons:               # configurable addons per mode (mandatory addons always run)
  normal:
    - code_beautifier        # phpcbf code-style fixes
    - deprecations_remover   # drupal-rector deprecation removal
    - translations_updater   # interface translations
    - composer_normalizer    # normalize composer.json
    - unsupported_modules    # report modules with no supported release
  security: []               # minimal by default — don't interfere with the fix
```

`--security` selects `addons.security`; otherwise `addons.normal` runs. Run
`drupdater addons` to list valid addon names.

### Environment variables

| Variable | Purpose |
|----------|---------|
| `DRUPALCODE_ACCESS_TOKEN` | Drupal.org GitLab PAT. Required for patch management (detecting upstream-committed patches and downloading updated patch files). |
| `COMPOSER_AUTH` | Composer auth JSON for private Packagist/registries. See [Composer docs](https://getcomposer.org/doc/03-cli.md#composer-auth). |

## Troubleshooting

| Symptom | Cause / Fix |
|---------|-------------|
| Push fails with `object not found` | Shallow checkout. Set `fetch-depth: 0` / `GIT_DEPTH: "0"`. |
| PR is created but CI doesn't run on it (GitHub) | Expected with `GITHUB_TOKEN` — use a PAT or App token. See [Tokens & Permissions](#tokens--permissions). |
| `composer install` fails on private packages | Provide `COMPOSER_AUTH` (see below). |
| Site install fails | Confirm `drush site-install --existing-config` works locally first. |
| Run aborts on an unknown addon name | The active addon list in `.drupdater.yaml` has a typo — `drupdater addons` lists valid names. |

<details>
<summary>Private Packagist example</summary>

```bash
docker run \
  -e COMPOSER_AUTH='{"http-basic":{"repo.packagist.com":{"username":"token","password":"<your-token>"}}}' \
  ghcr.io/drupdater/drupdater-php8.3:latest \
  <token> --clone --repository-url <repository_url>
```
</details>

<details>
<summary>Updating multiple sites in one repository</summary>

**1.** Resolve the active site directory from the `SITE_NAME` env var in
`web/sites/sites.php` (or a file it includes):

```php
$site_name = getenv('SITE_NAME');
if (is_string($site_name) && $site_name !== "") {
  $scheme = $request->getScheme();
  $port = $request->getPort();
  $site = $request->getHost();
  if ($site !== '') {
    if (('http' === $scheme && 80 != $port) || ('https' === $scheme && 443 != $port)) {
      $site = $port . '.' . $site;
    }
    if (!isset($sites[$site])) {
      $sites[$site] = $site_name;
    }
  } else {
    $sites[str_replace('/', '.', dirname($script_name))] = $site_name;
  }
}
```

**2.** List each site directory under `sites` in `.drupdater.yaml`:

```yaml
sites: [default, subsite_a, subsite_b]
```

All sites are updated in one branch under a single PR/MR.
</details>

## Architecture

Drupdater is a Go CLI (Cobra) that orchestrates external tools (Composer, Drush,
PHPCBF, Rector) over a linear, event-driven workflow. Functionality is organized
as **addons** subscribed to workflow events. For the full breakdown — workflow
phases, the addon system, and the VCS provider abstraction — see
[`CLAUDE.md`](CLAUDE.md).

## Development

Requires Go 1.26+ and `make`.

```bash
make build   # build the binary
make test    # run all tests
make lint    # vet + staticcheck + golangci-lint
make fmt     # format
make mock    # regenerate mocks (mockery v3)
```

Run a single test:

```bash
go test -v -run TestName ./path/to/package/...
```

## Contributing

Contributions are welcome. Please open an issue to discuss substantial changes
before opening a PR, run `make lint test` before submitting, and keep PRs
focused. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for details.

## Security

Drupdater handles credentials and modifies dependency trees, so we take security
seriously. **Do not file public issues for vulnerabilities.** Report them
privately via [GitHub Security Advisories](https://github.com/drupdater/drupdater/security/advisories/new)
or the contacts in [`SECURITY.md`](SECURITY.md).

## Support

- **Bugs / features:** [GitHub Issues](https://github.com/drupdater/drupdater/issues)
- **Questions / ideas:** [GitHub Discussions](https://github.com/drupdater/drupdater/discussions)

## License

Licensed under the [Apache License 2.0](LICENSE).

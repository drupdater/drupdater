# Drupdater

[![CI](https://github.com/drupdater/drupdater/actions/workflows/go.yml/badge.svg)](https://github.com/drupdater/drupdater/actions/workflows/go.yml)
[![Docker](https://ghcr-badge.egpl.dev/drupdater/drupdater-php8.3/latest_tag?trim=major&label=docker)](https://github.com/drupdater/drupdater/pkgs/container/drupdater-php8.3)
[![License](https://img.shields.io/github/license/drupdater/drupdater)](LICENSE)

Drupdater is a standalone tool for automating Drupal site updates. It runs against your existing checkout (the one CI already provides), updates Composer dependencies, applies code quality fixes, exports Drupal configuration, and opens a pull/merge request on GitHub or GitLab with a detailed changelog — highlighting any security-related changes.

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Usage](#usage)
  - [Command-Line](#command-line)
  - [CI/CD Integration](#cicd-integration)
- [Configuration](#configuration)
  - [Flags](#flags)
  - [Environment Variables](#environment-variables)
- [FAQ](#faq)

## Features

- **Dependency updates** — runs `composer update` and commits the result.
- **Security-only mode** — with `--security`, only vulnerable packages are updated.
- **Patch management** — removes patches for packages that no longer need them, checks whether existing patches still apply, and automatically downloads updated patch files from Drupal.org.
- **Code style fixing** — runs `phpcbf` to fix PHP code style issues; auto-generates a `phpcs.xml` baseline if one is missing.
- **Deprecation removal** — runs `drupal-rector` to remove deprecated API usage.
- **Composer plugin allow-listing** — detects newly required Composer plugins and adds them to `allow-plugins` in `composer.json`.
- **`composer normalize`** — normalises `composer.json` formatting when `ergebnis/composer-normalize` is installed.
- **Translation updates** — updates Drupal translation files via Drush when `locale_deploy` is enabled.
- **Changelog generation** — includes a full dependency diff table and a list of pending database update hooks in the MR/PR description.
- **Multi-site support** — updates multiple Drupal sites in a single repository with one merge request.
- **GitHub and GitLab** — automatically creates a pull request (GitHub) or merge request (GitLab), including self-hosted GitLab instances.

## Prerequisites

- Your Drupal site must be installable from configuration (i.e. `drush site-install --existing-config` works).
- Your repository is hosted on GitHub or GitLab.
- *(Optional)* A [Drupal.org GitLab access token](https://git.drupalcode.org) (`DRUPALCODE_ACCESS_TOKEN`) to enable automated patch management.

## Usage

### Command-Line

By default Drupdater runs against the **existing checkout** in the working directory — which is exactly what CI provides — so it only needs a token. To run it standalone (no checkout on disk), add `--clone --repository-url <url>` and it will clone the repository itself.

Pick the image that matches the PHP version your site requires:

```bash
# PHP 8.3
docker run ghcr.io/drupdater/drupdater-php8.3:latest <token> --clone --repository-url <repository_url>

# PHP 8.4
docker run ghcr.io/drupdater/drupdater-php8.4:latest <token> --clone --repository-url <repository_url>

# PHP 8.5
docker run ghcr.io/drupdater/drupdater-php8.5:latest <token> --clone --repository-url <repository_url>
```

Replace `<repository_url>` with your repository's HTTPS URL and `<token>` with a personal access token that has permission to push branches and create merge/pull requests.

### CI/CD Integration

#### GitLab CI

**Weekly full update** — run via a [scheduled pipeline](https://docs.gitlab.com/ee/ci/pipelines/schedules.html) set to a weekly interval:

```yaml
drupdater:
  image:
    name: ghcr.io/drupdater/drupdater-php8.3:latest
    entrypoint: [""]
  script:
    - /opt/drupdater/bin $DRUPDATER_TOKEN
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule" && $DRUPDATER_SCHEDULE == "weekly"
```

**Daily security-only update** — run via a separate scheduled pipeline set to a daily interval. Add `DRUPDATER_SCHEDULE=daily` as a pipeline schedule variable to distinguish it from the weekly run:

```yaml
drupdater-security:
  image:
    name: ghcr.io/drupdater/drupdater-php8.3:latest
    entrypoint: [""]
  script:
    - /opt/drupdater/bin $DRUPDATER_TOKEN --security
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule" && $DRUPDATER_SCHEDULE == "daily"
```

#### GitHub Actions

**Weekly full update:**

```yaml
name: Drupdater

on:
  schedule:
    - cron: "0 4 * * 1"  # every Monday at 04:00 UTC
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
      - name: Checkout
        uses: actions/checkout@v7
      - name: Run Drupdater
        run: /opt/drupdater/bin ${{ secrets.GITHUB_TOKEN }}
```

**Daily security-only update** — create a separate workflow file (e.g. `.github/workflows/drupdater-security.yml`) that runs every day and passes `--security` to only update packages with known vulnerabilities:

```yaml
name: Drupdater Security

on:
  schedule:
    - cron: "0 4 * * *"  # every day at 04:00 UTC
  workflow_dispatch:

permissions:
  contents: write
  pull-requests: write

jobs:
  drupdater-security:
    runs-on: ubuntu-latest
    container:
      image: ghcr.io/drupdater/drupdater-php8.3:latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Run Drupdater (security only)
        run: /opt/drupdater/bin ${{ secrets.GITHUB_TOKEN }} --security
```

> **Note:** `GITHUB_TOKEN` is sufficient to push a branch and open a pull request. However, GitHub prevents workflows triggered by `GITHUB_TOKEN` from starting other workflows, so CI will not run automatically on the resulting PR. To have CI trigger on the Drupdater PR, use a [personal access token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens) or a [GitHub App token](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app) stored as a repository secret instead.

## Configuration

### Flags

All flags are optional. Pass them after the required `<token>` argument.

| Flag | Default | Description |
|------|---------|-------------|
| `--branch` | `main` | Branch to update and target for the MR. Only used with `--clone`; in checkout mode the branch is taken from the checkout (or the CI branch variable when in detached HEAD). |
| `--working-dir` | `.` | Path to the existing checkout to update in place. |
| `--clone` | `false` | Clone the repository instead of using the existing checkout. Requires `--repository-url`. Intended for local testing. |
| `--repository-url` | _(from `origin`)_ | Repository URL. Required with `--clone`; otherwise derived from the checkout's `origin` remote. |
| `--sites` | `default` | Drupal site directories to update. Repeat the flag for multiple sites: `--sites default --sites subsite`. |
| `--security` | `false` | Only update packages with known security vulnerabilities. |
| `--skip-cbf` | `false` | Skip running `phpcbf` for PHP code style fixes. |
| `--skip-rector` | `false` | Skip running `drupal-rector` for deprecation removal. |
| `--dry-run` | `false` | Run all update steps but skip branch creation and MR/PR creation. |
| `--verbose` | `false` | Enable debug-level structured logging. |

### Environment Variables

| Variable | Description |
|----------|-------------|
| `DRUPALCODE_ACCESS_TOKEN` | Drupal.org GitLab personal access token. Required for patch management: checking whether a patch is already committed upstream and downloading updated patch files automatically. |
| `COMPOSER_AUTH` | Composer authentication JSON for accessing private Packagist repositories or other private registries. See the [Composer documentation](https://getcomposer.org/doc/03-cli.md#composer-auth) for the expected format. |

## FAQ

### How do I use Drupdater with a private Packagist?

Pass your credentials via the `COMPOSER_AUTH` environment variable:

```bash
docker run \
  -e COMPOSER_AUTH='{"http-basic":{"repo.packagist.com":{"username":"token","password":"<your-token>"}}}' \
  ghcr.io/drupdater/drupdater-php8.3:latest \
  <token> --clone --repository-url <repository_url>
```

### How do I update multiple Drupal sites in one repository?

**1. Add the following snippet to your `web/sites/sites.php`** (or a file included by it) to resolve the active site directory from the `SITE_NAME` environment variable:

```php
$site_name = getenv('SITE_NAME');
if (is_string($site_name) && $site_name !== "") {
  $scheme = $request->getScheme();
  $port = $request->getPort();
  $site = $request->getHost();

  if ($site !== '') {
    // Add the port if using a non-standard port for http or https.
    if (('http' === $scheme && 80 != $port) || ('https' === $scheme && 443 != $port)) {
      $site = $port . '.' . $site;
    }
    // Do not override existing entries in the $sites array.
    if (!isset($sites[$site])) {
      $sites[$site] = $site_name;
    }
  }
  else {
    $sites[str_replace('/', '.', dirname($script_name))] = $site_name;
  }
}
```

**2. Pass each site directory name via `--sites`:**

```bash
docker run ghcr.io/drupdater/drupdater-php8.3:latest \
  <repository_url> <token> \
  --sites default --sites subsite_a --sites subsite_b
```

All sites will be updated in a single branch and covered by one merge/pull request.

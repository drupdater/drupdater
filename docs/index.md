# Drupdater

Automated, reviewable Drupal updates — as a pull or merge request, on every schedule.

Drupdater is a standalone CLI, shipped as a Docker image, that keeps Drupal sites up to
date. Point it at a checkout in CI and it runs `composer update`, applies code-quality
fixes, exports Drupal configuration, and opens a **pull request (GitHub)** or **merge
request (GitLab)** with a detailed, human-reviewable changelog, with security changes
flagged.

You review and merge. Drupdater never deploys anything on its own.

!!! warning "Project status: pre-1.0 (`v0.x`)"

    Drupdater is in active development and used in real pipelines, but the CLI surface
    and config format may still change between minor versions. Pin to a specific image
    tag in production.

## Start here

<div class="grid cards" markdown>

-   :material-school: **[Tutorials](tutorials/index.md)**

    ---

    Learning-oriented. Start with [your first run](tutorials/first-run.md) against a
    throwaway repository, then wire up [scheduled updates in
    CI](tutorials/scheduled-updates.md).

-   :material-wrench: **[How-to guides](how-to/index.md)**

    ---

    Task-oriented recipes for a goal you already have: running in [GitHub
    Actions](how-to/github-actions.md) or [GitLab CI](how-to/gitlab-ci.md), [updating
    several sites](how-to/update-multiple-sites.md), [enabling
    auto-merge](how-to/enable-auto-merge.md), [troubleshooting a
    run](how-to/troubleshoot.md).

-   :material-book-open-variant: **[Reference](reference/index.md)**

    ---

    The precise details. Every [CLI flag](reference/cli/drupdater.md), the
    [`.drupdater.yaml` schema](reference/configuration.md), all ten
    [addons](reference/addons/index.md), and the [run report
    schema](reference/run-report.md).

-   :material-lightbulb-on: **[Explanation](explanation/index.md)**

    ---

    Why it is built this way. [How a run works](explanation/how-a-run-works.md), the
    [addon architecture](explanation/addon-architecture.md), [credential
    handling](explanation/credentials-and-redaction.md), and what Drupdater
    [deliberately does not do](explanation/non-goals.md).

</div>

## Quick start

Try it against any GitHub or GitLab repository. Pick the image matching your site's PHP
version:

```bash
docker run ghcr.io/drupdater/drupdater-php8.3:latest \
  <token> --clone --repository-url https://github.com/you/your-drupal-site.git
```

Add `--dry-run` to do everything except push the branch and open the pull request. See
[your first run](tutorials/first-run.md) for a guided walkthrough, or
[prerequisites](how-to/run-preflight-checks.md) to check a project is ready.

## What a run does

- **Dependency updates** — `composer update`, committed.
- **Security-only mode** (`--security`) — updates only packages with known
  vulnerabilities.
- **Patch management** — drops obsolete patches, verifies remaining ones still apply,
  and pulls updated patch files from Drupal.org.
- **Code style** — `phpcbf`, auto-generating a `phpcs.xml` baseline if missing.
- **Deprecation removal** — `drupal-rector`.
- **Composer hygiene** — auto-allow-lists new Composer plugins; runs `composer
  normalize` when `ergebnis/composer-normalize` is present.
- **Translations** — updates interface translations via Drush when `locale_deploy` is
  enabled.
- **Changelog** — full dependency diff table and pending database update hooks in the
  pull/merge request description.
- **Multi-site** — updates several sites in one repository under a single request.
- **GitHub and GitLab** — including self-hosted GitLab.

Each of these is an [addon](reference/addons/index.md), and most can be turned on or off
per run type.

## Support

- **Bugs and features** — [GitHub Issues](https://github.com/drupdater/drupdater/issues)
- **Questions and ideas** —
  [GitHub Discussions](https://github.com/drupdater/drupdater/discussions)
- **Vulnerabilities** — see the [security policy](contributing/security-policy.md); do
  not open a public issue.

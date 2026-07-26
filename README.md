# Drupdater

> Automated, reviewable Drupal updates — as a pull/merge request, on every schedule.

[![CI](https://github.com/drupdater/drupdater/actions/workflows/go.yml/badge.svg)](https://github.com/drupdater/drupdater/actions/workflows/go.yml)
[![Docker](https://ghcr-badge.egpl.dev/drupdater/drupdater-php8.3/latest_tag?trim=major&label=docker)](https://github.com/drupdater/drupdater/pkgs/container/drupdater-php8.3)
[![License](https://img.shields.io/github/license/drupdater/drupdater)](LICENSE)
[![Release](https://img.shields.io/github/v/tag/drupdater/drupdater?label=release)](https://github.com/drupdater/drupdater/tags)

**📖 [Documentation](https://drupdater.github.io/)**

**Drupdater** is a standalone CLI (shipped as a Docker image) that keeps Drupal sites up
to date for you. Point it at a checkout in CI; it runs `composer update`, applies
code-quality fixes, exports Drupal config, and opens a **pull request (GitHub) or merge
request (GitLab)** with a detailed, human-reviewable changelog — security changes flagged.

You review and merge. Drupdater never deploys anything on its own.

**Who it's for:** teams maintaining one or more Drupal sites who want routine and security
updates to arrive as reviewable PRs on a schedule, instead of as a manual chore.

> [!WARNING]
> **Project status: pre-1.0 (`v0.x`).** Drupdater is in active development and used in
> real pipelines, but the CLI surface and config format may still change between minor
> versions. Pin to a specific image tag in production.

## Quick start

Try it locally against any GitHub/GitLab repo. Pick the image matching your site's PHP
version (`php8.2`, `php8.3`, `php8.4`, `php8.5`):

```bash
docker run ghcr.io/drupdater/drupdater-php8.3:latest \
  <token> --clone --repository-url https://github.com/you/your-drupal-site.git
```

Add `--dry-run` to do everything except create the branch and PR/MR. In checkout mode
(no `--clone`), a `--dry-run` run touches the VCS platform for neither reading nor
writing, so it needs no token at all.

Validate a project before scheduling a real run:

```bash
docker run -v "$(pwd)":/app -w /app ghcr.io/drupdater/drupdater-php8.3:latest check
```

## Documentation

Full documentation lives at **[drupdater.github.io](https://drupdater.github.io/)**.

| | |
|---|---|
| [Tutorials](https://drupdater.github.io/tutorials/) | [Your first run](https://drupdater.github.io/tutorials/first-run/) · [Scheduled updates in CI](https://drupdater.github.io/tutorials/scheduled-updates/) |
| [How-to guides](https://drupdater.github.io/how-to/) | [GitHub Actions](https://drupdater.github.io/how-to/github-actions/) · [GitLab CI](https://drupdater.github.io/how-to/gitlab-ci/) · [Multi-site](https://drupdater.github.io/how-to/update-multiple-sites/) · [Auto-merge](https://drupdater.github.io/how-to/enable-auto-merge/) · [Troubleshooting](https://drupdater.github.io/how-to/troubleshoot/) |
| [Reference](https://drupdater.github.io/reference/) | [CLI flags](https://drupdater.github.io/reference/cli/drupdater/) · [`.drupdater.yaml`](https://drupdater.github.io/reference/configuration/) · [Addons](https://drupdater.github.io/reference/addons/) · [Run report](https://drupdater.github.io/reference/run-report/) |
| [Explanation](https://drupdater.github.io/explanation/) | [How a run works](https://drupdater.github.io/explanation/how-a-run-works/) · [Addon architecture](https://drupdater.github.io/explanation/addon-architecture/) · [Credentials](https://drupdater.github.io/explanation/credentials-and-redaction/) |

The documentation sources live in [`docs/`](docs/) in this repository and are published to
[drupdater/drupdater.github.io](https://github.com/drupdater/drupdater.github.io) on every
push to `main`. Preview them locally with `make docs-serve`.

## Contributing

Contributions are welcome. Please open an issue to discuss substantial changes before
opening a PR, run `make lint test` before submitting, and keep PRs focused.

See [`CONTRIBUTING.md`](CONTRIBUTING.md) and the
[contributor documentation](https://drupdater.github.io/contributing/).

## Security

Drupdater handles credentials and modifies dependency trees, so we take security
seriously. **Do not file public issues for vulnerabilities.** Report them privately via
[GitHub Security Advisories](https://github.com/drupdater/drupdater/security/advisories/new)
or the contacts in [`SECURITY.md`](SECURITY.md).

## Support

- **Bugs / features:** [GitHub Issues](https://github.com/drupdater/drupdater/issues)
- **Questions / ideas:** [GitHub Discussions](https://github.com/drupdater/drupdater/discussions)

## License

Licensed under the [Apache License 2.0](LICENSE).

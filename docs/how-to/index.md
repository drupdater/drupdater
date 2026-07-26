# How-to guides

Task-oriented recipes. Each page assumes you already know what you want and covers one
goal from start to finish.

New to Drupdater? Start with the [tutorials](../tutorials/index.md) instead.

## Getting it running

- [Run in GitHub Actions](github-actions.md)
- [Run in GitLab CI](gitlab-ci.md)
- [Run preflight checks](run-preflight-checks.md) — validate a project before scheduling
  a real run, and gate a pipeline on the result

## Configuring a project

- [Update multiple sites in one repository](update-multiple-sites.md)
- [Use a private Composer registry](use-private-packagist.md)
- [Enable patch management](enable-patch-management.md)
- [Enable auto-merge](enable-auto-merge.md)
- [Migrate the config layout](migrate-config-layout.md) — moving to the `run_types` shape

## Operating it

- [Consume the run report](consume-the-run-report.md) — assert on a run from CI
- [Troubleshoot a run](troubleshoot.md)

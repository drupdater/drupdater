# Run in GitHub Actions

The recommended setup is two scheduled workflows: a **weekly full update** and a **daily
security-only update**.

## Before you start

- The site installs from configuration (`drush site-install --existing-config` works).
- You know your project's PHP version, to pick the right
  [image](../reference/docker-images.md).
- Run [`drupdater check`](run-preflight-checks.md) once first — it will tell you if
  anything is missing.

## Weekly full update

Create `.github/workflows/drupdater.yml`:

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

      - run: /opt/drupdater/bin "${{ secrets.DRUPDATER_TOKEN }}"
```

Three things in there are load-bearing:

- **`fetch-depth: 0`** — without full history the update branch cannot be pushed. The run
  fails fast in its `preflight` phase rather than at push time.
- **`/opt/drupdater/bin`** — the binary's path inside the image. The image has an
  `ENTRYPOINT`, but `run:` steps bypass it.
- **`permissions`** — `contents: write` to push the branch, `pull-requests: write` to open
  the request.

## Daily security update

Copy the file to `.github/workflows/drupdater-security.yml` and change two things: the
cron to `"0 4 * * *"`, and add `--security` to the run command.

```yaml
      - run: /opt/drupdater/bin "${{ secrets.DRUPDATER_TOKEN }}" --security
```

Two workflows rather than one with a conditional keeps the schedules independently
adjustable and the run logs separate.

## Choosing a token

This is the decision most worth getting right.

=== "`GITHUB_TOKEN` (built in)"

    ```yaml
      - run: /opt/drupdater/bin "${{ secrets.GITHUB_TOKEN }}"
    ```

    Works out of the box with no setup. Enough to push a branch and open a pull request.

    **But pull requests opened with `GITHUB_TOKEN` do not trigger other workflows.** Your
    test suite will not run on the Drupdater pull request, which defeats most of the
    point — you would be reviewing an update nothing has validated.

=== "PAT or App token (recommended)"

    Store a [personal access
    token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)
    or a [GitHub App installation
    token](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app)
    as a repository secret — `DRUPDATER_TOKEN` above.

    Pull requests opened with it **do** trigger your other workflows, so CI runs on the
    update before you review it.

    A fine-grained PAT needs **Contents: read and write** and **Pull requests: read and
    write** on the repository.

## Passing the token via the environment

Drupdater reads `DRUPDATER_TOKEN` when no argument is given, which keeps the token out of
the process list:

```yaml
      - run: /opt/drupdater/bin
        env:
          DRUPDATER_TOKEN: ${{ secrets.DRUPDATER_TOKEN }}
```

## Optional additions

### Patch management

Add a [Drupal.org token](enable-patch-management.md) so Drupdater can repair patches
instead of pinning packages:

```yaml
        env:
          DRUPDATER_TOKEN: ${{ secrets.DRUPDATER_TOKEN }}
          DRUPALCODE_ACCESS_TOKEN: ${{ secrets.DRUPALCODE_ACCESS_TOKEN }}
```

### Private packages

See [Use a private Composer registry](use-private-packagist.md) for `COMPOSER_AUTH`.

### A run report artifact

```yaml
      - run: /opt/drupdater/bin "${{ secrets.DRUPDATER_TOKEN }}" --report ./report.json

      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: drupdater-report
          path: ./report.json
```

`if: always()` matters — the [report](../reference/run-report.md) is written on failures
too, and that is when you most want it. See [Consume the run
report](consume-the-run-report.md).

## Verify it

Trigger the workflow manually with **Run workflow** rather than waiting for the schedule.
For a first run that touches nothing remote, add `--dry-run`:

```yaml
      - run: /opt/drupdater/bin --dry-run --verbose
```

In checkout mode a dry run needs no token at all — it neither reads from nor writes to
GitHub.

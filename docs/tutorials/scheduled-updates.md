# Scheduled updates in CI

In this tutorial you will take the dry run from [your first run](first-run.md) and turn it
into a pull request that arrives on a schedule: a full update weekly, and a security-only
update daily.

By the end, the project maintains itself and your only job is reviewing.

**Time:** about 30 minutes, plus one pipeline run.

## What you need

- A Drupal project on GitHub or GitLab that passes `drupdater check`.
- Permission to add secrets and workflows to it.

## Step 1: commit a configuration file

Drupdater works with no configuration at all, but being explicit means the next person can
see what the project expects. Create `.drupdater.yaml` at the repository root:

```yaml
sites: [default]
timeout: 30m

run_types:
  normal:
    addons:
      - code_beautifier
      - deprecations_remover
      - translations_updater
      - composer_normalizer
      - unsupported_modules
    auto_merge: false
  security:
    addons: []
    auto_merge: false
```

Those are the defaults written out. Two things are worth adjusting now:

- **`sites`** — list every directory under `web/sites/` you want updated. See [Update
  multiple sites](../how-to/update-multiple-sites.md).
- **`timeout`** — raise it if your first run took more than about 20 minutes. A large
  multi-site project may need `2h`.

Note that `security.addons` is empty. A security update should be a minimal, focused fix,
so code-style and deprecation work stays out of its way.

Verify the file parses:

```bash
docker run -v "$(pwd)":/app -w /app \
  ghcr.io/drupdater/drupdater-php8.3:latest check
```

```text
✓ .drupdater.yaml valid (sites: default)
✓ addon names resolve
```

Commit it.

## Step 2: create a token

Drupdater needs to push a branch and open a request.

=== "GitHub"

    Create a [fine-grained personal access
    token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)
    with, on this repository:

    - **Contents:** read and write
    - **Pull requests:** read and write

    Store it as a repository secret named **`DRUPDATER_TOKEN`**.

    !!! warning "Do not use the built-in `GITHUB_TOKEN`"

        It works — it can push and open a pull request — but pull requests opened with it
        **do not trigger other workflows**. Your test suite would never run on the update,
        leaving you reviewing a change nothing has validated. That defeats the purpose.

=== "GitLab"

    Create a **project access token** with:

    - **Scopes:** `write_repository` and `api`
    - **Role:** Developer

    Store it as a **masked, protected** CI/CD variable named **`DRUPDATER_TOKEN`**.

## Step 3: add the weekly job

=== "GitHub Actions"

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
              fetch-depth: 0

          - run: /opt/drupdater/bin
            env:
              DRUPDATER_TOKEN: ${{ secrets.DRUPDATER_TOKEN }}
    ```

    `workflow_dispatch` is there so you can trigger it by hand in the next step rather
    than waiting until Monday.

=== "GitLab CI"

    Add to `.gitlab-ci.yml`:

    ```yaml
    .drupdater_base:
      image:
        name: ghcr.io/drupdater/drupdater-php8.3:latest
        entrypoint: [""]
      variables:
        GIT_DEPTH: "0"

    drupdater:weekly:
      extends: .drupdater_base
      script:
        - /opt/drupdater/bin
      rules:
        - if: $CI_PIPELINE_SOURCE == "schedule" && $DRUPDATER_SCHEDULE == "weekly"
    ```

    Then under **Build → Pipeline schedules**, create a schedule with interval pattern
    `0 4 * * 1` and a variable `DRUPDATER_SCHEDULE` set to `weekly`.

Two settings in there are not optional:

- **Full history** (`fetch-depth: 0` / `GIT_DEPTH: "0"`) — the update branch cannot be
  pushed from a shallow clone.
- **The binary path** `/opt/drupdater/bin` — the image's entrypoint is bypassed when you
  run a script step.

Commit and push.

## Step 4: rehearse it

Before letting it open a real pull request, run it once as a dry run. Temporarily change
the command:

```yaml
      - run: /opt/drupdater/bin --dry-run --verbose
```

Trigger it manually — **Run workflow** on GitHub, or **Play** on the pipeline schedule in
GitLab — and watch the log. You should see the phases from the previous tutorial, ending
without a `publish` phase.

This also proves the checkout is deep enough and the image PHP version is right, which are
the two things that differ between your machine and CI.

Now remove `--dry-run` and push again.

## Step 5: trigger a real run

Trigger the workflow manually once more. This time it will:

1. Push a branch named `update-<hash>`.
2. Open a pull or merge request titled like **July 2026: Drupal Maintenance Updates**.
3. Fill the description with the dependency table, pending update hooks, and any patch or
   unsupported-module findings.

Open it. That description is the whole point of the tool — it is a review, not a diff.

!!! note "If nothing happened"

    Check the log for `update aborted`. `no changes detected` means the project is already
    current; `branch update-… already exists` means an identical update is already open.
    Both exit `0` deliberately.

## Step 6: add the daily security job

Security fixes should not wait for Monday.

=== "GitHub Actions"

    Copy `.github/workflows/drupdater.yml` to
    `.github/workflows/drupdater-security.yml` and change two things — the cron, and add
    `--security`:

    ```yaml
    on:
      schedule:
        - cron: "0 4 * * *"   # every day 04:00 UTC
      workflow_dispatch:
    ```

    ```yaml
          - run: /opt/drupdater/bin --security
            env:
              DRUPDATER_TOKEN: ${{ secrets.DRUPDATER_TOKEN }}
    ```

=== "GitLab CI"

    Add a second job alongside the first:

    ```yaml
    drupdater:security:
      extends: .drupdater_base
      script:
        - /opt/drupdater/bin --security
      rules:
        - if: $CI_PIPELINE_SOURCE == "schedule" && $DRUPDATER_SCHEDULE == "daily"
    ```

    Then create a second pipeline schedule with pattern `0 4 * * *` and
    `DRUPDATER_SCHEDULE` set to `daily`.

A security run differs from a normal one in three ways:

- Only packages with known advisories are updated, and Composer is asked to disturb as
  little else as possible.
- It reads `run_types.security` from `.drupdater.yaml` — the empty addon list.
- The request is titled **2026-07-26: Drupal Security Updates**, dated to the day, because
  several can arrive in a month.

On a healthy site it will do nothing most nights and exit `0`. That is the intended
behaviour, not a misconfiguration.

## Step 7: keep the report

Have CI archive the machine-readable report so a failed run leaves something better than a
log:

=== "GitHub Actions"

    ```yaml
          - run: /opt/drupdater/bin --report ./report.json
            env:
              DRUPDATER_TOKEN: ${{ secrets.DRUPDATER_TOKEN }}

          - uses: actions/upload-artifact@v4
            if: always()
            with:
              name: drupdater-report
              path: ./report.json
    ```

=== "GitLab CI"

    ```yaml
      script:
        - /opt/drupdater/bin --report ./drupdater-report.json
      artifacts:
        when: always
        paths:
          - drupdater-report.json
    ```

`if: always()` / `when: always` matters — the report is written on failures too, and that
is when you want it most.

## Step 8: pin the image

You have been using `:latest`. Drupdater is pre-1.0, so its CLI and config format can
change between minor versions — and `latest` will pick that up without warning.

Replace it with a full version everywhere:

```yaml
image: ghcr.io/drupdater/drupdater-php8.3:v0.12.0
```

Check the [latest release](https://github.com/drupdater/drupdater/releases) for the
current tag, and bump it deliberately.

## What you built

- A `.drupdater.yaml` recording what the project needs.
- A weekly full update and a daily security update, both scheduled.
- A machine-readable report archived from every run.
- A pinned image, so the pipeline does not change under you.

## Next

- [Consume the run report](../how-to/consume-the-run-report.md) — assert that the addons
  you enabled actually ran, rather than trusting the exit code.
- [Enable patch management](../how-to/enable-patch-management.md) — stop packages being
  pinned by stale patches.
- [Enable auto-merge](../how-to/enable-auto-merge.md) — if your pipeline is a good enough
  gate to skip the review.

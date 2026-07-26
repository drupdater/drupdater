# Run in GitLab CI

The recommended setup is two [scheduled
pipelines](https://docs.gitlab.com/ee/ci/pipelines/schedules.html): a **weekly full
update** and a **daily security-only update**, distinguished by a variable set on each
schedule.

## Before you start

- The site installs from configuration (`drush site-install --existing-config` works).
- You know your project's PHP version, to pick the right
  [image](../reference/docker-images.md).
- Run [`drupdater check`](run-preflight-checks.md) once first.

## The pipeline configuration

Add to `.gitlab-ci.yml`:

```yaml
.drupdater_base:
  image:
    name: ghcr.io/drupdater/drupdater-php8.3:v0.12.0
    entrypoint: [""]
  variables:
    GIT_DEPTH: "0"  # full history required to push the update branch

drupdater:weekly:
  extends: .drupdater_base
  script:
    - /opt/drupdater/bin "$DRUPDATER_TOKEN"
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule" && $DRUPDATER_SCHEDULE == "weekly"

drupdater:security:
  extends: .drupdater_base
  script:
    - /opt/drupdater/bin "$DRUPDATER_TOKEN" --security
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule" && $DRUPDATER_SCHEDULE == "daily"
```

Three things in there are load-bearing:

- **`entrypoint: [""]`** — the image's `ENTRYPOINT` is the Drupdater binary itself. GitLab
  needs a shell to run `script:`, so the entrypoint must be cleared and the binary invoked
  explicitly at `/opt/drupdater/bin`.
- **`GIT_DEPTH: "0"`** — GitLab clones shallow by default, and the update branch cannot be
  pushed from a shallow clone. The run fails fast in its `preflight` phase rather than at
  push time.
- **`$CI_PIPELINE_SOURCE == "schedule"`** — keeps the jobs out of ordinary merge request
  and branch pipelines.

## Creating the schedules

Under **Build → Pipeline schedules**, create two:

| Description | Interval pattern | Variable |
|---|---|---|
| Weekly Drupal update | `0 4 * * 1` | `DRUPDATER_SCHEDULE` = `weekly` |
| Daily security update | `0 4 * * *` | `DRUPDATER_SCHEDULE` = `daily` |

The variable is what the `rules:` above match on, so one `.gitlab-ci.yml` serves both.

## The access token

Create a **project or group access token**, or use a personal access token, with:

- **Scopes** — `write_repository` and `api`.
- **Role** — Developer or higher. (Maintainer if you plan to use
  [auto-merge](enable-auto-merge.md) on a protected branch.)

Store it as a **masked, protected** CI/CD variable named `DRUPDATER_TOKEN`.

Because the variable is already named `DRUPDATER_TOKEN`, you can drop it from the command
line entirely — Drupdater reads it from the environment, which keeps it out of the process
list:

```yaml
  script:
    - /opt/drupdater/bin
```

!!! note "Self-hosted GitLab works without configuration"

    GitLab CI sets `GITLAB_CI=true`, which Drupdater treats as authoritative when picking
    a provider. A self-hosted instance whose hostname does not contain "gitlab" resolves
    correctly on that basis alone. See [VCS provider
    detection](../explanation/vcs-provider-detection.md).

## Optional additions

### Patch management

```yaml
.drupdater_base:
  variables:
    GIT_DEPTH: "0"
    DRUPALCODE_ACCESS_TOKEN: $DRUPALCODE_ACCESS_TOKEN
```

See [Enable patch management](enable-patch-management.md).

### Private packages

See [Use a private Composer registry](use-private-packagist.md) for `COMPOSER_AUTH`.

### A run report artifact

```yaml
drupdater:weekly:
  extends: .drupdater_base
  script:
    - /opt/drupdater/bin --report ./drupdater-report.json
  artifacts:
    when: always
    paths:
      - drupdater-report.json
```

`when: always` matters — the [report](../reference/run-report.md) is written on failures
too, and that is when you most want it. See [Consume the run
report](consume-the-run-report.md).

## Verify it

Run the schedule manually from **Build → Pipeline schedules** rather than waiting. For a
first run that touches nothing remote:

```yaml
  script:
    - /opt/drupdater/bin --dry-run --verbose
```

In checkout mode a dry run needs no token at all — it neither reads from nor writes to
GitLab.

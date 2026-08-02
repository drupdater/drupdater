# `drupdater`

The root command. Runs an update against a Drupal checkout and opens a pull or merge
request.

```text
drupdater [token] [flags]
```

Drupal Updater is a tool to update Drupal dependencies and create merge requests.

The access token is read from the first argument, or from the `DRUPDATER_TOKEN`
environment variable when no argument is given. Project settings — sites, timeout, and
which addons run — are read from [`.drupdater.yaml`](../configuration.md) in the working
directory.

## Flags

All flags are optional and all are persistent, so [`drupdater check`](check.md) accepts
them as well.

| Flag | Type | Default | Description |
|---|---|---|---|
| `--branch` | string | `main` | Branch to update and target for the merge request. Only used with `--clone`; in checkout mode it is taken from the checkout, or from the CI branch variable when the checkout is in detached HEAD. |
| `--working-dir` | string | `.` | Path to the existing checkout to update in place. Also where `.drupdater.yaml` is read from unless `--config` overrides it. |
| `--clone` | bool | `false` | Clone the repository instead of using the existing checkout. Requires `--repository-url`. Intended for local testing. |
| `--repository-url` | string | *(from `origin`)* | Repository URL. Required with `--clone`; otherwise derived from the checkout's `origin` remote. |
| `--security` | bool | `false` | Only apply security updates. Selects the `run_types.security` block in `.drupdater.yaml` and lets [`composer_audit`](../addons/composer-audit.md) — which runs either way — narrow the update to the vulnerable packages. |
| `--concurrency` | int | `GOMAXPROCS(0)` | Maximum number of sites to install and update concurrently. The default reflects the container's CPU quota, not just the host's core count. |
| `--dry-run` | bool | `false` | Do not push the update branch or create a merge request. The branch and commits are still created locally. |
| `--report` | string | *(disabled)* | Write a machine-readable [JSON report](../run-report.md) of the run to this path. Written on every outcome, including failures and `--dry-run`. |
| `--verbose` | bool | `false` | Debug-level logging. Also logs the resolved configuration. |
| `--config` | string | *(`<working-dir>/.drupdater.yaml`)* | Path to the config file. |

## Validation before the run starts

Two checks run before anything else, and fail the command immediately:

- `--clone` without `--repository-url` →
  `--repository-url is required with --clone`
- A malformed `--repository-url` → `invalid repository URL: <detail>`

The URL is validated against what the provider factory accepts, which includes SCP-style
git URLs (`git@host:owner/repo.git`) as well as HTTP(S). See [VCS provider
detection](../../explanation/vcs-provider-detection.md).

Once the run proper begins, a `preflight` phase runs two more checks — full git history
and PHP platform requirements — which are the same checks [`drupdater
check`](check.md) performs. See [Preflight checks](../preflight-checks.md).

## Checkout mode versus clone mode

Checkout mode is the default and the one to use in CI:

```bash
drupdater "$DRUPDATER_TOKEN"
```

The repository URL comes from the checkout's `origin` remote, and the target branch from
the checkout's current branch. In CI the checkout is normally in detached HEAD, in which
case the branch is read from `GITHUB_REF_NAME` or `CI_COMMIT_REF_NAME`. If the checkout
is detached and neither variable is set, the run fails with:

```text
could not determine the target branch: the checkout is in detached HEAD and no CI
branch variable (GITHUB_REF_NAME, CI_COMMIT_REF_NAME) is set
```

Clone mode is for local testing:

```bash
drupdater <token> --clone --repository-url https://github.com/you/site.git --branch main
```

It clones into a temporary directory, which is removed when the run ends. `--branch` only
has an effect here.

!!! note "Checkout mode requires full git history"

    The update branch cannot be pushed from a shallow clone. Set `fetch-depth: 0` in
    GitHub Actions or `GIT_DEPTH: "0"` in GitLab CI. This is caught by the `preflight`
    phase rather than at push time.

## Where files are written

Site databases are SQLite files written **beside** the working directory, not inside it:

```text
<working-dir>/../<site>.sqlite
<working-dir>/../private/<site>/
```

Checkout-mode runs remove these afterwards, including the `private/` directory itself if
it is empty — so a project's real private files directory survives. This means the
checkout's parent must be a real, writable directory.

## Examples

Update in place in CI, writing a report:

```bash
drupdater "$DRUPDATER_TOKEN" --report ./drupdater-report.json
```

Security-only update:

```bash
drupdater "$DRUPDATER_TOKEN" --security
```

Full local rehearsal with no token and no remote side effects:

```bash
drupdater --dry-run --verbose --report ./report.json
```

Update a checkout that is not the current directory:

```bash
drupdater "$DRUPDATER_TOKEN" --working-dir /workspace/project
```

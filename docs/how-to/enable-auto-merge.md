# Enable auto-merge

Auto-merge asks the platform to merge Drupdater's request as soon as its pipeline
succeeds. Nothing is merged while checks are failing or pending.

It is **off by default in both run types**.

!!! warning "This removes the review step"

    Drupdater's premise is that a human reviews the update. Enabling auto-merge replaces
    that human with your test suite. Only do it where the pipeline genuinely gates the
    change, and consider enabling it for one run type rather than both.

## 1. Turn it on for a run type

In `.drupdater.yaml`:

```yaml
run_types:
  normal:
    auto_merge: false     # routine bumps still get reviewed
  security:
    auto_merge: true      # security fixes land as soon as CI is green
```

The two are separate on purpose: auto-merging routine dependency bumps is a different
risk decision from auto-merging a security fix, and most teams want one without the
other. Which way round depends on whether you fear an unreviewed change more than a
delayed patch.

## 2. Meet the platform requirements

=== "GitHub"

    - **Enable "Allow auto-merge"** in the repository's **Settings → General → Pull
      Requests**. Without it the API call fails.
    - The token needs **write access to pull requests**.
    - Drupdater picks a merge method the repository actually permits, trying merge commit,
      then squash, then rebase.
    - Whether the branch is deleted afterwards is your repository's **Automatically delete
      head branches** setting — Drupdater does not override it.

    !!! note "GitHub Enterprise is not supported"

        Auto-merge uses GitHub's GraphQL API at a hardcoded path that GitHub Enterprise
        serves elsewhere. See [VCS provider
        detection](../explanation/vcs-provider-detection.md).

=== "GitLab"

    - The token needs the **Developer role or higher**. On a protected target branch it
      may need Maintainer.
    - The source branch **is** deleted on merge.
    - If the project has no pipeline at all, the merge happens immediately — there is
      nothing to wait for.

    Drupdater waits for the merge request's status to settle before requesting auto-merge,
    retrying briefly if GitLab is still preparing it. It deliberately does **not** wait for
    the request to become `mergeable`: "CI must pass" and "CI still running" are exactly
    the states auto-merge exists to handle.

## 3. Verify it

Auto-merge is **best-effort**. If it fails — the feature is disabled, the token lacks the
scope, the platform errors — Drupdater logs a warning and the run still **succeeds**. The
branch is pushed and the request exists; it just waits for you to merge it by hand.

That means a silent failure is possible, so check the [run
report](../reference/run-report.md) rather than the exit code:

```bash
jq '.merge_request.auto_merge' report.json
```

```json
{ "enabled": true }
```

On failure:

```json
{
  "enabled": false,
  "error": "auto-merge is not enabled for this repository"
}
```

The `auto_merge` object is present **only when the active run type asked for it**, so
"never requested" is distinguishable from "requested and failed". If it is absent, check
that `auto_merge: true` is set on the run type you are actually running — a `--security`
run reads `run_types.security`, not `run_types.normal`.

Assert on it in CI:

```bash
jq -e '.merge_request.auto_merge.enabled == true' report.json \
  || echo "auto-merge was requested but did not take effect"
```

## Why a failure does not fail the run

By the time auto-merge is requested, the branch has been pushed and the request created —
the run's actual work is complete and correct. Failing it at that point would report a
successful update as a failure and, on a scheduled job, invite someone to re-run it,
producing nothing but noise.

The outcome is recorded in the report precisely so that a best-effort failure is still
machine-readable rather than buried in a log.

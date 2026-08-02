# Consume the run report

`--report` writes a [JSON document](../reference/run-report.md) describing the run, on
every outcome — success, failure, and `--dry-run` alike. This page covers what to do with
it.

```bash
drupdater "$DRUPDATER_TOKEN" --report ./drupdater-report.json
```

## Why not just check the exit code

Because a run reports `success` even when an addon failed. Most addons log and swallow
their own errors, so that the loss of, say, translation updates does not throw away an
otherwise complete dependency update.

That trade has a cost — an addon that broke would look exactly like one with nothing to
do, and [one once did, for months](../explanation/why-a-run-report.md).

Asserting that the addons you expect to run **actually appear in the report** is what
turns a check from "it did not crash" into "it did what it is for".

## Quick queries

```bash
# Did anything change?
jq -r '.status' report.json                    # success | no_changes | failed

# What changed?
jq -r '.packages[] | "\(.action) \(.package) \(.from // "-") → \(.to // "-")"' report.json

# Where did the time go?
jq -r '.phases[] | "\(.duration_seconds)s \(.name)"' report.json | sort -rn

# What failed, and why?
jq -r 'select(.status == "failed") | "\(.failed_phase): \(.error)"' report.json

# Which addons reported anything?
jq -r '.addons | keys[]' report.json

# What would the merge request say? (works on a --dry-run, which opens none)
jq -r '.merge_request_description' report.json

# Which security advisories are still open?
jq -r '.addons.composer_audit.remaining[]? | "\(.cve // .advisoryId) \(.packageName)"' report.json

# Which packages were held back by a patch conflict?
jq -r '.addons.composer_patches.conflicts[]? | "\(.package) held at \(.fixed_version)"' report.json
```

## Distinguish "nothing to do" from "broken"

`status` has three values, and the middle one matters:

| Value | Meaning |
|---|---|
| `success` | The run did work and completed |
| `no_changes` | The run worked and found nothing to update |
| `failed` | A phase errored — see `failed_phase` and `error` |

Both `success` and `no_changes` exit `0`. A nightly security job on a healthy site
produces `no_changes` every night, and that is the correct outcome, not a problem.

```bash
case "$(jq -r '.status' report.json)" in
  success)    echo "update opened: $(jq -r '.merge_request.url' report.json)" ;;
  no_changes) echo "already up to date" ;;
  failed)     jq -r '"failed in \(.failed_phase): \(.error)"' report.json; exit 1 ;;
esac
```

## Assert on a run in CI

Drupdater's own integration tests use a jq script for this, and it is directly reusable.

It checks two different things. The `$expect` fields are what *your* project expects —
status, a minimum package count, named phases present **and** succeeded, named addons that
reported something, packages that had to move, actions that must not appear. Everything
after them is the report's own internal consistency and is always on: package changes have
a shape, phases are ordered and unique, a `success` has no failed phase, a `--dry-run` has
no merge request, an advisory is not both fixed and remaining, and an addon that reported
data also rendered its section into the description.

```jq title=".github/assert-report.jq"
--8<-- ".github/assert-report.jq"
```

Use it like this:

```bash
EXPECT='{
  "status": ["success", "no_changes"],
  "phases": ["composer install", "baseline site install", "update shared code"],
  "addons": ["update_hooks", "code_beautifier"],
  "packages_match": ["^drupal/core"],
  "forbid_actions": ["Downgrade"],
  "audit_fixed_min": 1
}'

# Optional: assert no credential reached the document
export DRUPDATER_ASSERT_SECRETS="$DRUPDATER_TOKEN"

jq -e --argjson expect "$EXPECT" -f .github/assert-report.jq report.json
```

`packages_match` and `audit_fixed_min` are the two worth adding early. A package count
alone is satisfied by any unrelated transitive bump, and "`composer_audit` reported
something" is satisfied by a security run that closed no advisory at all and only listed
the ones still open.

On failure it prints every problem at once and exits non-zero:

```text
report assertions failed:
  - phase not ok: site update (drush command failed)
  - addon reported nothing: translations_updater -- present in report: update_hooks, composer_diff
```

### Choosing what to expect

Expect the addons you have actually enabled in
[`.drupdater.yaml`](../reference/configuration.md), and remember that a normal run and a
security run produce different sets. Drupdater's own fixtures split them:

| Run type | Addons expected |
|---|---|
| `normal` | `update_hooks`, `unsupported_modules`, `composer_audit`, `code_beautifier`, `deprecations_remover`, `translations_updater` |
| `security` | `composer_audit`, `unsupported_modules`, `update_hooks` |

`composer_audit` and `unsupported_modules` appear in both because both addons run on every
update. Each is still omitted when it has nothing to say — `composer_audit` when the run
found no advisories at all, `unsupported_modules` when every installed module is supported —
so expect them only for a project where you know there is something to find.

Note that [`composer_diff`](../reference/addons/composer-diff.md) and
[`composer_normalizer`](../reference/addons/composer-normalizer.md) never appear in the
report at all — do not assert on them.

### Cross-check the report against `composer.lock`

`packages` is parsed out of composer's console output, not read back from the lock. If you
want a second opinion that does not share that assumption, diff the lock the run committed
against the one it started from and compare:

```bash
git show "origin/main:composer.lock" > /tmp/base-composer.lock

jq -n -e -r \
  --slurpfile before /tmp/base-composer.lock \
  --slurpfile after composer.lock \
  --slurpfile report report.json \
  -f .github/assert-lock-matches-report.jq
```

It fails in both directions: a lock change the report does not mention, and a reported
change the lock does not show — the second being the one a reviewer would act on.

```jq title=".github/assert-lock-matches-report.jq"
--8<-- ".github/assert-lock-matches-report.jq"
```

## Collect the report as a CI artifact

=== "GitHub Actions"

    ```yaml
      - run: /opt/drupdater/bin "${{ secrets.DRUPDATER_TOKEN }}" --report ./report.json

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

`if: always()` / `when: always` is the important part. The report is written on failures
too, and a failed run is when you most want it.

## Track runs over time

Because reports are stable — addons sort their output so two runs over unchanged input
produce byte-identical sections — they diff cleanly:

```bash
diff <(jq -S '.addons.unsupported_modules' old.json) \
     <(jq -S '.addons.unsupported_modules' new.json)
```

Phase durations make the cost of a run measurable without separate instrumentation:

```bash
jq -r '[.started_at, .duration_seconds, .status] | @tsv' report.json >> history.tsv
```

## Schema compatibility

Check `schema_version` and **ignore fields you do not recognise**. New fields are added
without bumping the version; only a removal or rename increments it.

```bash
jq -e '.schema_version == 1' report.json || echo "report contract changed — review the consumer"
```

## Credentials

Reports are safe to archive and to attach to a ticket. The repository URL is stripped of
embedded credentials, and the whole serialised document passes through the same redactor
as the logs. See [Credentials and
redaction](../explanation/credentials-and-redaction.md).

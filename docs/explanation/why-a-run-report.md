# Why a run report

Drupdater writes a machine-readable [JSON report](../reference/run-report.md) describing
each run. This page is about why it exists in the form it does.

## The failure it was built to catch

At one point, the
[`unsupported_modules`](../reference/addons/unsupported-modules.md) addon was silently
broken on Drupal 11. The procedural constants it relied on had been removed from core, so
every query failed.

The addon swallows its own errors by design — a transient problem reaching Drupal.org
should not throw away an otherwise complete update. So every run was green. Every pull
request looked normal. The addon reported nothing, for months, and nothing distinguished
that from "there are no unsupported modules".

Gating on the exit code, or on `status`, would never have caught it. The run genuinely did
succeed. What was missing was any way to ask **did the addon run at all?**

That question is what the report answers.

## Why addons report even when they did nothing visible

It would be reasonable to have an addon report only when it has something to say. Two
addons do exactly that, and are absent from the report entirely:
[`composer_diff`](../reference/addons/composer-diff.md), whose content duplicates the
top-level `packages` field, and
[`composer_normalizer`](../reference/addons/composer-normalizer.md), which only reorders
`composer.json`.

Everything else reports even when its work is "only" a code change, because:

- **The diff tells you what changed, not whether an addon ran.** A commit labelled "Update
  coding styles" proves PHPCBF ran; its *absence* proves nothing at all.
- **Most addons log and swallow their own failures.** An addon that crashed looks exactly
  like an addon with nothing to do.

[`translations_updater`](../reference/addons/translations-updater.md) is the clearest case.
It skips a site when `locale_deploy` is not enabled, or when the translation path does not
resolve. Both are legitimate; both were previously visible only in a log line. A report
that simply omitted the site would make "deliberately skipped" and "silently failed"
indistinguishable — so it records `skipped` with the reason instead.

## Why it is written on every exit path

The report's write is deferred **first**, which in Go means it runs **last** — after every
other piece of cleanup. So it is emitted on success, on failure, on `--dry-run`, on
timeout, and on `SIGTERM`.

A run that fails halfway is precisely the run whose report you want. It records which
phase failed, with what error, and how long each preceding phase took. Reading that is
considerably better than reading several thousand lines of Composer output.

The write is also **atomic** — a temporary file in the destination directory, then a
rename — so a process polling the path never observes a partial document.

And a write failure is **logged and swallowed**. If the path is unwritable, the run still
succeeds or fails on its own merits. The report describes the run; it is not the run.

## Why `no_changes` is its own status

`success` and `failed` are not enough, because "the site was already up to date" is
neither.

Calling it a failure means a nightly security job on a healthy site goes red every night,
which trains everyone to ignore the signal — and then to miss the night it means
something. Calling it plain `success` loses the distinction between "we shipped an update"
and "there was nothing to ship", which is exactly what a dashboard needs to show.

So there are three statuses, and the two "fine" ones both exit `0`.

When a run resolves to `no_changes`, the phase that raised the abort stays in the `phases`
list with `ok: false`, while the top-level `failed_phase` and `error` are cleared. You can
still see where it stopped without the run being reported as broken.

## Why phase durations are in there

Because the alternative is adding instrumentation to answer an obvious question: why does
this take twenty minutes?

The phase distribution answers it directly. A run dominated by `composer install` is a
network or cache problem; one dominated by `baseline site install` is a database or
concurrency problem; one dominated by `update shared code` is usually Rector. None of that
needs a metrics pipeline — it is in a file the run already writes.

## Why auto-merge is recorded there

Enabling auto-merge is best-effort: by the time it is requested, the branch is pushed and
the request exists, so a failure must not fail the run. See [Enable
auto-merge](../how-to/enable-auto-merge.md).

But "does not fail the run" cannot mean "is invisible". The report records the outcome
under `merge_request.auto_merge`, present **only** when the active run type asked for it —
so "never requested" and "requested and failed" are distinguishable, and a silent failure
is machine-readable rather than buried in a log.

## Why the output is deterministic

Addons sort their report data — modules by name, Rector rules per file — so two runs over
unchanged input produce byte-identical sections.

That makes reports diffable directly, without a consumer having to normalise map ordering
first. Comparing this week's unsupported modules to last week's is a `diff`, not a
program.

## The schema contract

`schema_version` is part of the contract:

- New fields may be added **without** bumping it. Consumers must ignore unknown fields.
- Removing or renaming a field increments it.

The report types deliberately **mirror** Drupdater's internal types rather than reusing
them. An internal refactor that renames a field would otherwise silently rename a
published one — the duplication is the point.

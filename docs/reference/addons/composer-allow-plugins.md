# `composer_allow_plugins`

Keeps Composer's plugin allow-list from blocking an unattended update, and reports any
new plugins for you to review.

| | |
|---|---|
| Runs | **Always** — mandatory, cannot be disabled |
| Events | `pre-composer-update` (Normal), `post-composer-update` (Normal) |
| Report key | *(none)* |
| Pull request section | "🔌 New Composer plugins", when new plugins were found |

## Why it exists

Composer refuses to execute a plugin that is not in `config.allow-plugins`, and prompts
interactively when it meets one. An unattended run has nobody to answer that prompt, so
an update that pulls in a new plugin would hang or fail.

## What it does

**Before the update**, it saves the project's current `allow-plugins` configuration and
replaces it with `allow-plugins: true`, permitting everything for the duration of the
update.

**After the update**, it restores the saved configuration, compares the installed plugin
list against it, and appends any newly-introduced plugin with the value `false`. The
project ends up back where it started, plus explicit `false` entries for anything new.

The result is that new plugins are **not** silently trusted. They are recorded, disabled,
and surfaced in the pull request for a human to decide on.

## Pull request section

Rendered only when the update introduced new plugins:

--8<-- "internal/addon/testdata/allowplugins.md"

The commands are copy-pasteable — review each plugin, then run the ones you want to
allow.

## Notes

- If a run fails partway through, the temporary `allow-plugins: true` could in principle
  be left in `composer.json`. It is not: a failing run restores the original checkout,
  discarding that change along with any commits on the throwaway work branch.
- This addon contributes nothing to the [run report](../run-report.md). The new plugins
  are visible in the `composer.json` diff and in the pull request section.

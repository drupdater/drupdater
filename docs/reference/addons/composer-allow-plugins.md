# `composer_allow_plugins`

Keeps Composer's plugin allow-list from blocking an unattended update, and reports any
new plugins for you to review.

| | |
|---|---|
| Runs | **Always** — mandatory, cannot be disabled |
| Events | `pre-composer-update` (Change), `post-composer-update` (Change) |
| Report key | `composer_allow_plugins` |
| Pull request section | "🔌 New Composer plugins", when new plugins were found |

## What it does

**Before the update**, it appends a temporary rule, `"*": true`, after the project's own
`allow-plugins` entries. Composer applies the first rule that matches a plugin, so every
plugin the project already allows or denies — by name or by a pattern such as `drupal/*` —
is still decided by the project. Only a plugin none of them names is let through, for the
duration of the update.

A setting that already decides every plugin is left alone: `allow-plugins: true`,
`allow-plugins: false`, or an object with its own `"*"` rule.

**After the update**, it adds `false` for each installed plugin that no rule of the project
matches, then removes the temporary rule. A plugin here is what Composer itself checks
against `allow-plugins`: a locked package of type `composer-plugin` or
`composer-installer`. Whether a rule matches is decided by Composer's own pattern matching,
so a rule decides exactly the plugins Composer would let it decide. The denials go in first
because Composer refuses to run any command while an installed plugin is undecided. The
project ends up exactly where it started — same rules, same order — plus one explicit `false`
per new plugin. A project that had no `allow-plugins` at all gets none back unless a new
plugin needed an entry, and no empty `config` either.

Both steps run in the project's own Composer, inside the
[Drupdater agent](../docker-images.md#drupdater-agent), and each edit is the one
`composer config allow-plugins.<name>` would make. Which plugins the project's rules already
cover is decided with Composer's own matching — anchored, case-insensitive, `*` for any
characters — so it cannot disagree with Composer about which plugin a rule names.

The result is that new plugins are **not** silently trusted. They are recorded, disabled,
and surfaced in the pull request for a human to decide on.

## Why it exists

Composer refuses to execute a plugin that is not in `config.allow-plugins`, and prompts
interactively when it meets one. An unattended run has nobody to answer that prompt, so
an update that pulls in a new plugin would hang or fail.

## Report data

```json
{
  "composer_allow_plugins": {
    "added": ["php-http/discovery", "tbachert/spi"]
  }
}
```

`added` lists the plugins the update introduced, each now in `allow-plugins` as `false`,
sorted by name. Omitted when the update introduced none.

## Pull request section

Rendered only when the update introduced new plugins:

--8<-- "internal/mergerequest/testdata/composer_allow_plugins.md"

The commands are copy-pasteable — review each plugin, then run the ones you want to
allow.

## Notes

- If a run fails partway through, the temporary `"*": true` rule could in principle be
  left in `composer.json`. It is not: the run edits a worktree or clone of its own, which
  is removed whichever way the run ends, and the checkout itself is never changed.
- The new plugins are also in the [run report](../run-report.md), which the pull request
  section is rendered from.

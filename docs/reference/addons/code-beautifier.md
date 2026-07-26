# `code_beautifier`

Runs PHPCBF over the project's custom code to fix coding-standard violations, generating
a PHPCS configuration first if the project has none.

| | |
|---|---|
| Runs | Configurable — in the `normal` defaults, not in `security` |
| Events | `post-code-update` (Normal) |
| Report key | `code_beautifier` |
| Pull request section | *(none)* |
| Commits | `Add PHPCS config`, `Install drupal/coder`, `Update coding styles` |

## Why it exists

Drupal core version bumps regularly change what the `Drupal` and `DrupalPractice`
standards require. Folding the resulting mechanical fixes into the same pull request as
the update keeps them from accumulating into a separate cleanup task later.

## What it does

### Ensures a PHPCS configuration exists

If the project has neither `phpcs.xml` nor `phpcs.xml.dist`, one is generated from the
project's custom code directories and the installed `drupal/core` major version, using
the `Drupal` and `DrupalPractice` rulesets and the extensions
`php,module,inc,install,test,profile,theme`. It is committed as `Add PHPCS config`.

Two cases stop it short:

- **No custom code directories** — the addon skips entirely. There is nothing to check.
- **A config exists but declares no `<file>` paths** — a warning is logged and the addon
  skips, rather than guessing at a scope the project deliberately left open.

### Ensures `drupal/coder` is installed

If absent, it is required as a dev dependency and committed as `Install drupal/coder`.

### Fixes and commits

Runs `phpcs` to collect violations, then `phpcbf` to fix them. Only files that actually
had errors or warnings are staged, and the `Update coding styles` commit is made **only
if something was staged** — so a run with nothing to fix leaves no empty commit behind.

## Report section

```json
{
  "addons": {
    "code_beautifier": {
      "files": [
        "web/modules/custom/example/src/Controller/ExampleController.php",
        "web/themes/custom/site/site.theme"
      ],
      "fixable": 47
    }
  }
}
```

| Field | Meaning |
|---|---|
| `files` | The files PHPCBF actually fixed and committed |
| `fixable` | How many violations PHPCS considered fixable beforehand |

The two differ when a violation is reported as fixable but PHPCBF cannot fix it in
practice — a gap worth looking at, since those violations will be reported again on every
subsequent run.

Omitted when no files were fixed.

## Ordering

This addon runs at **normal** priority on `post-code-update`, deliberately *after*
[`deprecations_remover`](deprecations-remover.md), which runs above normal. Rector
temporarily installs and removes itself, and that `composer.json` churn must be committed
before PHPCBF stages anything — otherwise it would be swept into the coding-style commit.

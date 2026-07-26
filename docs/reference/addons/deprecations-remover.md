# `deprecations_remover`

Runs [Drupal Rector](https://github.com/palantirnet/drupal-rector) over the project's
custom code to rewrite APIs deprecated by the new Drupal version.

| | |
|---|---|
| Runs | Configurable — in the `normal` defaults, not in `security` |
| Events | `post-code-update` (**AboveNormal**) |
| Report key | `deprecations_remover` |
| Pull request section | *(none)* |
| Commits | `Remove temporary drupal-rector installation`, `Remove deprecations` |

## What it does

1. If `palantirnet/drupal-rector` is not already a project dependency, it is required
   temporarily.
2. Rector runs over the project's custom code directories, using the `scripts/rector.php`
   configuration shipped in the Docker image.
3. The temporary Rector installation is removed again, and any residual `composer.json`
   or `composer.lock` change is committed as `Remove temporary drupal-rector
   installation`.
4. Changed files are staged and committed as `Remove deprecations`.

The temporary install-then-remove is what forces this addon to run **above normal**
priority on `post-code-update`: its `composer.json` churn must be fully committed before
[`code_beautifier`](code-beautifier.md) stages anything at normal priority.

## Why it exists

Each Drupal minor release deprecates APIs that the *next* major removes. Fixing them at
the moment the deprecation lands — in the same pull request as the version bump that
introduced it — is far cheaper than discovering a few hundred of them when the major
upgrade finally comes around.

## Report section

```json
{
  "addons": {
    "deprecations_remover": [
      {
        "file": "web/modules/custom/example/src/Form/ExampleForm.php",
        "applied_rectors": [
          "DrupalRector\\Rector\\Deprecation\\DrupalSetMessageRector"
        ]
      },
      {
        "file": "web/modules/custom/example/example.module",
        "applied_rectors": [
          "DrupalRector\\Rector\\Deprecation\\FileCreateUrlRector"
        ]
      }
    ]
  }
}
```

A list of the files Rector rewrote and the rules that fired on each. **The rule names are
the actionable part**: they say which deprecation was removed, which "the file changed"
does not. Entries are sorted so two runs over unchanged code produce byte-identical
output and reports can be diffed directly.

Omitted when Rector changed nothing.

## Configuration

The Rector rule set comes from `scripts/rector.php` inside the Docker image — Drupal
Rector's own set provider plus the deprecation-helper rule. It is not currently
configurable per project.

Only the project's **custom** code directories are processed, as resolved from
`composer.json`. Contributed and core code is never rewritten.

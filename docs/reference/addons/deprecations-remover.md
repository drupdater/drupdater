# `deprecations_remover`

Runs [Drupal Rector](https://github.com/palantirnet/drupal-rector) over the project's
custom code to rewrite APIs deprecated by the new Drupal version.

| | |
|---|---|
| Runs | Configurable — in the `normal` defaults, not in `security` |
| Events | `post-code-update` (**Change**) |
| Report key | `deprecations_remover` |
| Pull request section | *(none)* |
| Commits | `Remove deprecations` |

## What it does

1. It picks a Rector:
    - A project that depends on `palantirnet/drupal-rector` runs its own, through
      `composer exec`.
    - A project with no Rector at all runs the copy [bundled in the Docker
      image](../docker-images.md#bundled-tools), which loads the project's code through its
      `vendor/autoload.php`, wherever `vendor-dir` points. Nothing is installed into the
      project.
    - A project that requires `rector/rector` but not `palantirnet/drupal-rector` is
      **skipped with a warning**. Two Rector versions cannot load into one process, so the
      bundled one cannot run beside the project's. Add `palantirnet/drupal-rector` to
      `require-dev` to have its own Rector used instead.
2. Rector runs over the project's custom code directories, using the `scripts/rector.php`
   configuration shipped in the Docker image.
3. Changed files are committed as `Remove deprecations`.

It runs at the **Change** stage on `post-code-update`, ahead of
[`code_beautifier`](code-beautifier.md) at Format, so that PHPCBF formats the code Rector wrote.

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
Rector's Drupal 8, 9 and 10 deprecation sets plus the deprecation-helper rule. Each rule
checks the installed `drupal/core` version itself, so none rewrites code for an API the
site's core has not deprecated yet. It is not currently configurable per project.

Only the project's **custom** code directories are processed, as resolved from
`composer.json`. Contributed and core code is never rewritten.

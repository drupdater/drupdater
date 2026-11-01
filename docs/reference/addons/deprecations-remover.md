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
   configuration shipped in the Docker image. Rector imports the classes its rewrites
   name, adding `use` statements.
3. In each file Rector changed, a `@file` docblock Rector's new imports ended up under is
   moved back to the top. In a file without a namespace — a `.module` or `.install` file —
   Rector puts imports straight after `<?php`, and Drupal's coding standard would then have
   PHPCBF add a second, empty `@file` docblock above them.
4. Changed files are committed as `Remove deprecations`.

It runs at the **Change** stage on `post-code-update`, ahead of
[`code_beautifier`](code-beautifier.md) at Format, so that PHPCBF formats the code Rector wrote.

### Service calls

A deprecated method is only found when Rector knows the type of the object it is called on.
For `\Drupal::service('renderer')->renderPlain($build)` that type comes from Drupal's service
container, which Rector learns through
[`mglaman/phpstan-drupal`](https://github.com/mglaman/phpstan-drupal): the bundled Rector
always has it, and a project's own Rector uses the project's copy when there is one.

If phpstan-drupal cannot load the project — a malformed `*.services.yml`, say — Rector still
runs and applies every other fix, and only calls on `\Drupal::service()` results go
unrewritten. The reason is logged at debug level.

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

The Rector rule set comes from `scripts/rector.php` inside the Docker image:

- Drupal Rector's **composer-based sets**, chosen from the installed `drupal/core` version.
  They cover the installed major up to the installed minor, including the renames that
  only become safe from that minor on, and never a later minor's deprecations.
- Its **Drupal 10 set**, because the composer-based sets stop at the major: an API
  deprecated in 10.x still exists on 11 until 12 removes it.
- The deprecation-helper rule, which unwraps `DeprecationHelper::backwardsCompatibleCall()`
  for deprecations older than the installed core. A site runs one core version, so no
  backward-compatibility wrappers are written either.

It is not currently configurable per project.

Only the project's **custom** code directories are processed: `modules/custom`,
`themes/custom` and `profiles/custom` under the web root `composer.json` names, where they
exist. Contributed and core code is never rewritten.

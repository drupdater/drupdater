# `composer_patches`

Maintains the project's `extra.patches` across an update: drops patches that are no
longer needed, replaces ones that have been superseded, and refuses to update a package
whose patch cannot be made to apply.

| | |
|---|---|
| Runs | **Always** — mandatory, cannot be disabled |
| Events | `pre-composer-update` (Normal) |
| Report key | `composer_patches` |
| Pull request section | "🩹 Patch updates" |
| Requires | [`DRUPALCODE_ACCESS_TOKEN`](../environment-variables.md#drupalcode_access_token) for the Drupal.org half |

## What it does

It runs before the real update, using a dry-run `composer update` to see which packages
would change, then works through the project's `extra.patches`:

### Removal

A patch is removed when:

- Its package is no longer installed, or is being removed by this update.
- The patch is already provided by a dependency.
- Its Drupal.org issue has been resolved upstream — status **Fixed**, **Closed (fixed)**
  or **Patch (to be ported)** — *and* a commit for it exists on the target version.

### Update

For a package being upgraded or downgraded, the addon resolves the Drupal.org issue
number from the patch description, rewrites the description into the canonical form
`Issue #12345: [Title](url)`, and tests whether the patch still applies.

If it does not, the addon looks for a newer patch: it fetches the most recent fork merge
request diff for the issue and stores it under

```text
patches/<project>/<issue-id>-<sha>-<title>.diff
```

Where a package carries several patches, they are validated as a set — patches that each
apply alone but conflict with each other are caught.

The test happens in a throwaway Composer project, which is given **the repositories your
project declares** in addition to drupal.org and packagist.org. Without them a package
served only from a private registry — a private fork of a contrib module, an in-house
module, anything behind [Private Packagist](../../how-to/use-private-packagist.md) —
could not be resolved for the test at all.

### Conflict

If no working patch can be found, the package is **pinned to its currently installed
version** rather than updated. The rest of the update proceeds. The pin is reported as a
conflict so you know a package was deliberately held back and why.

A pin means *the patch was tested and rejected*. When the test cannot be carried out at
all — the package could not be downloaded, a registry was unreachable, a credential was
refused — the package is **not** pinned and no conflict is reported; the run logs a
warning naming the package and leaves it to update normally. An unverifiable patch is not
a known-stale one, and pinning on that path would hold a package back on every run while
blaming a conflict that never happened.

Changes to `composer.json` and the `patches/` directory are committed as `Update patches`.

## Why it exists

Patches are the most fragile part of a Drupal dependency update. A patch written against
version 1.2 frequently will not apply to 1.3 — sometimes because the upstream issue was
fixed and the patch is now redundant, sometimes because it needs rerolling. Without
handling, `composer update` simply fails and the whole run is lost.

## Report section

```json
{
  "addons": {
    "composer_patches": {
      "removed": [
        {
          "package": "drupal/example",
          "patch_description": "Issue #3123456: [Fix the thing](https://www.drupal.org/i/3123456)",
          "patch_path": "https://www.drupal.org/files/issues/3123456-12.patch",
          "reason": "Fixed"
        }
      ],
      "updated": [
        {
          "package": "drupal/other",
          "patch_description": "Issue #3200000: [Support Drupal 11](https://www.drupal.org/i/3200000)",
          "previous_patch_path": "https://www.drupal.org/files/issues/3200000-8.patch",
          "new_patch_path": "patches/other/3200000-a1b2c3-support-drupal-11.diff"
        }
      ],
      "conflicts": [
        {
          "package": "drupal/stubborn",
          "patch_description": "Custom behaviour we depend on",
          "patch_path": "patches/stubborn/custom.patch",
          "fixed_version": "2.0",
          "new_version": "3.0"
        }
      ]
    }
  }
}
```

A conflict entry records both versions: `fixed_version` is the version the package was
held at, `new_version` the one it would otherwise have moved to.

Omitted entirely when the run changed no patches.

`conflicts` needs a human. Those packages could not be updated, and will stay behind on
every subsequent run until someone rerolls the patch or the issue is fixed upstream.

## Pull request section

--8<-- "internal/addon/testdata/composer_patches_1.md"

## Without a Drupal.org token

Set [`DRUPALCODE_ACCESS_TOKEN`](../environment-variables.md#drupalcode_access_token) to
enable the Drupal.org half of this addon. Without it, Drupdater cannot read issue status
or download replacement patches, so:

- Patches for removed or uninstalled packages are still dropped.
- Patches whose issues were fixed upstream are **not** detected, and stay in the project.
- A patch that stops applying results in a pin, not a repair.

See [Enable patch management](../../how-to/enable-patch-management.md).

## Limitations

This addon targets the `cweagans/composer-patches` **version 1** `extra.patches` format —
hence the internal name `ComposerPatches1`. Drupal.org is the only patch source it can
fetch replacements from; a patch hosted elsewhere can be removed or pinned but never
updated.

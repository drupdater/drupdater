# `composer_patches`

Maintains the project's `extra.patches` across an update: drops patches that are no
longer needed, replaces ones that have been superseded, and refuses to update a package
whose patch cannot be made to apply.

| | |
|---|---|
| Runs | **Always** — mandatory, cannot be disabled |
| Events | `pre-composer-update` (Change) |
| Report key | `composer_patches` |
| Pull request section | "🩹 Patch updates" |
| Requires | [`DRUPALCODE_ACCESS_TOKEN`](../environment-variables.md#drupalcode_access_token) for the Drupal.org half |

## What it does

It runs before the real update, resolving it once to see which packages would change and
then putting `composer.json` and `composer.lock` back, then works through the project's
`extra.patches`:

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
apply alone but conflict with each other are caught. The set is applied in the order
`composer.json` declares it, which is the order `composer-patches` applies it in, so patches
that only apply one after another are checked that way.

When the addon rewrites `extra.patches`, it also updates `composer.lock`'s `content-hash` to
match, since the hash covers `extra`, and changes nothing else in the lock. `composer.json`
keeps its formatting, except where Composer's own JSON editor cannot write the patches
faithfully: then the whole file is written out again in Composer's format. Every
package and patch keeps its place, and an empty patch list stays an empty object. A rewritten
description takes the place of the one it replaces. Two patches for the same issue keep their
own descriptions rather than both being renamed to `Issue #…`, which would leave one key for
two patches.

### How a patch is tested

The test runs inside your project's own Composer, in the
[Drupdater agent](../docker-images.md#drupdater-agent) the image installs, where the addon
itself runs. It looks the package up at the new version in **the repositories your project
declares**, in their order and with your credentials, downloads it into a temporary
directory, and applies the patches to it in order. Nothing is installed into your project,
and no Composer installer event fires, so your own plugins never act on the download.

A package served only from a private registry — a private fork of a contrib module, an
in-house module, anything behind [Private Packagist](../../how-to/use-private-packagist.md)
— is therefore tested like any other.

Patches are applied the way `cweagans/composer-patches` 1.x applies them, so the verdict is
the one your own `composer install` would reach:

- With `git apply` first when the package was installed from source, then with `patch`.
- At levels `-p1`, `-p0`, `-p2` and `-p4`, in that order — or only at the level
  `extra.patchLevel` names for the package.
- A remote patch is downloaded with Composer's own HTTP client, and retried once.

Two deliberate differences: each level is tried with a dry run first, so a failed attempt
leaves no hunks behind for the next one, and a patch that is already applied is rejected
rather than offered for reversal.

### Conflict

If no working patch can be found, the package is **pinned to its currently installed
version** rather than updated. The rest of the update proceeds. The pin is reported as a
conflict so you know a package was deliberately held back and why.

The pin is this addon's proposal for the [update
plan](../../explanation/addon-architecture.md#steering-the-update-through-proposals). The
patches are checked against the plan as `Prepare` settled it, so on a `--security` run only
the packages the audit selected are checked.

A pin means *the patch was tested and rejected*. When the test cannot be carried out at
all — the package could not be downloaded, a registry was unreachable, a credential was
refused, a remote patch could not be downloaded or a local patch file is missing — the
package is **not** pinned and no conflict is reported; the run logs a warning naming the
package and the reason and leaves it to update normally. An unverifiable patch is not
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

--8<-- "internal/mergerequest/testdata/composer_patches.md"

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

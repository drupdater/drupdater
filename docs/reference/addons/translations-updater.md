# `translations_updater`

Refreshes each site's interface translations after the code update, and commits the
result.

| | |
|---|---|
| Runs | Configurable — in the `normal` defaults, not in `security` |
| Events | `post-site-update` (Normal) |
| Report key | `translations_updater` |
| Pull request section | *(none)* |
| Commits | `Update translations` |
| Requires | The `locale_deploy` module enabled on the site |

## What it does

For each site, after that site's update hooks and configuration export have run:

1. Checks whether the `locale_deploy` module is enabled. If not, the site is **skipped**.
2. Resolves the site's translation path. If it does not resolve, the site is **skipped**.
3. Updates the interface translations via Drush.
4. Stages the translation path and commits `Update translations` — only if something was
   actually staged.

Sites run concurrently, so the results map is mutex-guarded and keyed by site.

## Why it exists

Updating a module frequently changes its translatable strings. On a project that commits
its `.po` files, those translations go stale at exactly the moment the module is updated —
so the natural time to refresh them is in the same pull request.

## Report section

```json
{
  "addons": {
    "translations_updater": {
      "default": {
        "path": "web/sites/default/files/translations",
        "updated": true
      },
      "intranet": {
        "updated": false,
        "skipped": "locale_deploy module is not enabled"
      }
    }
  }
}
```

| Field | Meaning |
|---|---|
| `path` | The translation directory that was updated |
| `updated` | Whether anything was actually committed |
| `skipped` | Present with a reason when the addon bailed out early |

`skipped` distinguishes a site the addon deliberately passed over from one it ran against
and found nothing to commit. Both skip conditions — `locale_deploy` not enabled, and an
unresolvable translation path — are recorded with their reason.

Omitted when no site produced a result at all.

## Requirements

The site must have the **`locale_deploy`** module enabled. Without it, Drupdater skips
the site rather than attempting an update that would not be committable — `locale_deploy`
is what makes translations a file-based, deployable artefact rather than database state.

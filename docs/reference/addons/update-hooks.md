# `update_hooks`

Collects the database update hooks that the update leaves pending, per site, so the
reviewer knows what will run on deploy.

| | |
|---|---|
| Runs | **Always** — mandatory, cannot be disabled |
| Events | `pre-site-update` (Min) |
| Report key | `update_hooks` |
| Pull request section | "📄 Job Logs" → "⚙️ Update Hooks" |

## What it does

Runs immediately before each site's update hooks are executed, and asks Drush for the
pending hook list. It runs at the **lowest** priority on `pre-site-update` so it observes
the state after any other addon on that event has finished.

Sites are processed concurrently, so the collected map is mutex-guarded and keyed by
site: the same module can have different pending hooks on different sites.

## Why it exists

A dependency bump that carries database update hooks is a different kind of change from
one that does not. Hooks run on deploy, may take a long time on a large site, and may not
be reversible. Listing them turns "this updates Drupal core" into "this updates Drupal
core and will run these four schema changes", which is the information a reviewer
actually needs to schedule the deploy.

## Report section

```json
{
  "addons": {
    "update_hooks": {
      "default": {
        "system_update_10301": {
          "module": "system",
          "update_id": "10301",
          "description": "Add the 'revision_default' field to all revisionable entity types.",
          "type": "hook_update_n"
        }
      },
      "intranet": {}
    }
  }
}
```

Keyed by site, then by hook name. A site with no pending hooks appears as an empty
object; the whole section is omitted only when no site had any.

## Pull request section

--8<-- "internal/addon/testdata/update_hooks.md"

In a multi-site run each site gets its own `### Site: <name>` heading. With a single site
the heading is omitted, since it would carry no information.

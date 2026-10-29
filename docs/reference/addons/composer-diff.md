# `composer_diff`

Produces the dependency changelog: every package the update installed, upgraded,
downgraded or removed, with links to the upstream project and compare view.

| | |
|---|---|
| Runs | **Always** — mandatory, cannot be disabled |
| Events | `post-composer-update` (Inspect) |
| Report key | `composer_diff` |
| Pull request section | "🛠️ Dependency updates" |

## What it does

Runs `composer diff --format=json --with-links`, which compares the working
`composer.lock` with the one committed at `HEAD`, and records one entry per changed
package. The same list is logged at info level, one line per package, without the links.

It runs at the **Inspect** stage on `post-composer-update`, so every other addon on that
event has already finished mutating the lock file before the diff is taken.

## Why it exists

Everything else in the pull request describes work Drupdater did. This describes what
changed in the dependency tree, which is what the reviewer is approving.

## Report data

```json
{
  "composer_diff": [
    {
      "package": "drupal/core",
      "operation": "upgrade",
      "from": "10.3.8",
      "to": "10.3.9",
      "dev": false,
      "direct": true,
      "compare_url": "https://github.com/drupal/core/compare/10.3.8...10.3.9",
      "project_url": "https://github.com/drupal/core"
    }
  ]
}
```

`operation` is one of `install`, `upgrade`, `downgrade`, `remove`, or `change` — a new
commit of the same version, as with a `dev-` branch. `from` is absent for an install and
`to` for a removal. `dev` marks a `require-dev` package and `direct` one `composer.json`
names. `compare_url` and `project_url` are absent where `composer diff` cannot derive one
from the package's source.

Production packages come first, then dev packages, each sorted by name. Omitted when the
lock did not change.

The report's top-level `packages` field lists the same changes from `composer update`'s
own output. This section adds what the pull request needs and that output lacks: the dev
split and the links.

## Pull request section

Production and dev packages get a table each, wrapped in an open `<details>` block because
on a large update it can run to hundreds of rows:

--8<-- "internal/mergerequest/testdata/composer_diff.md"

If the whole description would exceed GitHub's 65,536-byte limit, the tables are rendered
without the links.

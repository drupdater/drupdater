# `composer_diff`

Produces the dependency changelog: a table of every package the update installed,
upgraded, downgraded or removed, with links to the upstream changelogs.

| | |
|---|---|
| Runs | **Always** — mandatory, cannot be disabled |
| Events | `post-composer-update` (Min) |
| Report key | *(none — deliberately)* |
| Pull request section | "🛠️ Dependency updates" |

## What it does

Runs `composer diff` twice against the pre-update and post-update lock files:

- Once with links, producing a markdown table where each version is a link to the
  upstream compare view or release page. This goes in the pull request.
- Once plain, logged at info level so the same information is in the run log without
  markdown noise.

It runs at the **lowest** priority on `post-composer-update`, so any other addon on that
event has already finished mutating the lock file before the diff is taken.

## Why it exists

Everything else in the pull request describes work Drupdater did. This describes what
changed in the dependency tree, which is what the reviewer is approving.

## Pull request section

The table is wrapped in an open `<details>` block, because on a large update it can run
to hundreds of rows:

```markdown
## 🛠️ Dependency updates

<details open>
<summary>Open/close</summary>

| Production Changes | | | |
| --- | --- | --- | --- |
| drupal/core | 10.1.8 | 10.2.0 | [Compare](https://github.com/drupal/core/compare/10.1.8...10.2.0) |
| drupal/token | 1.12.0 | 1.13.0 | [Compare](https://github.com/drupal/token/compare/8.x-1.12...8.x-1.13) |

</details>
```

## Why it reports nothing

This addon contributes no section to the [run report](../run-report.md), and that is
deliberate. The same information is already in the report's top-level `packages` field,
in structured form, straight from `composer update`:

```json
{
  "packages": [
    { "action": "Upgrade", "package": "drupal/core", "from": "10.1.8", "to": "10.2.0" }
  ]
}
```

A markdown table embedded in a JSON document would be strictly worse than that. Consume
`packages` instead — see [Consume the run
report](../../how-to/consume-the-run-report.md).

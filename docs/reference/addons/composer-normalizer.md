# `composer_normalizer`

Runs `composer normalize` so the update's `composer.json` changes land in the project's
canonical formatting.

| | |
|---|---|
| Runs | Configurable — in the `normal` defaults, not in `security` |
| Events | `post-composer-update` (Min) |
| Report key | *(none — deliberately)* |
| Pull request section | *(none)* |

## What it does

Checks whether
[`ergebnis/composer-normalize`](https://github.com/ergebnis/composer-normalize) is
installed in the project. If it is, `composer normalize` runs. If it is not, a warning is
logged and the addon does nothing.

It never installs the normaliser itself. A project that has not adopted
`ergebnis/composer-normalize` has not asked for its `composer.json` to be reordered, and
doing so unprompted would produce a large, unreviewable diff.

It runs at the **lowest** priority on `post-composer-update`, so it normalises the final
state after every other addon on that event has finished.

## Why it exists

`composer update` and `composer require` write `composer.json` in whatever order they
happen to produce. On a project that normalises its `composer.json`, that means every
Drupdater pull request carries an unrelated reformatting diff — or worse, gets reverted
by the next developer who runs the normaliser locally.

## Requirements

```bash
composer require --dev ergebnis/composer-normalize
```

Enabling this addon without that package installed is harmless — it just logs:

```text
ergebnis/composer-normalize is not installed, skipping normalization
```

## Why it reports nothing

This addon contributes no section to the [run report](../run-report.md). It only reorders
`composer.json`; that a run normalised it is fully visible in the diff and has no
consumer beyond it. There is no failure mode a report field would catch.

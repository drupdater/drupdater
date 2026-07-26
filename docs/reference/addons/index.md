# Addons

Nearly everything Drupdater does beyond `composer update` itself is an addon: a unit that
subscribes to [workflow events](../../explanation/addon-architecture.md) and does one job
when its event fires. Addons never call each other.

Each addon may contribute a section to the pull or merge request description, a section
to the [run report](../run-report.md), or both.

## Catalogue

| Addon | Runs | Events | Report key |
|---|---|---|---|
| [`composer_allow_plugins`](composer-allow-plugins.md) | Always | `pre-composer-update`, `post-composer-update` | — |
| [`composer_patches`](composer-patches.md) | Always | `pre-composer-update` | `composer_patches` |
| [`composer_diff`](composer-diff.md) | Always | `post-composer-update` | — |
| [`update_hooks`](update-hooks.md) | Always | `pre-site-update` | `update_hooks` |
| [`composer_audit`](composer-audit.md) | Automatically in `--security` | `pre-composer-update`, `post-code-update`, `pre-merge-request-create` | `composer_audit` |
| [`code_beautifier`](code-beautifier.md) | Configurable | `post-code-update` | `code_beautifier` |
| [`deprecations_remover`](deprecations-remover.md) | Configurable | `post-code-update` | `deprecations_remover` |
| [`translations_updater`](translations-updater.md) | Configurable | `post-site-update` | `translations_updater` |
| [`composer_normalizer`](composer-normalizer.md) | Configurable | `post-composer-update` | — |
| [`unsupported_modules`](unsupported-modules.md) | Configurable | `pre-site-update` | `unsupported_modules` |

## Mandatory versus configurable

**Four addons always run** and cannot be disabled: `composer_allow_plugins`,
`composer_patches`, `composer_diff` and `update_hooks`. They are not optional because
each one is either required for the update to succeed at all
(`composer_allow_plugins`, `composer_patches`) or is the changelog the reviewer needs
(`composer_diff`, `update_hooks`).

**`composer_audit` is added automatically** when `--security` is passed. It is what makes
a security run a security run: it decides which packages get updated.

**The remaining five are configurable** per run type in
[`.drupdater.yaml`](../configuration.md#run_typestypeaddons):

```yaml
run_types:
  normal:
    addons:
      - code_beautifier
      - deprecations_remover
      - translations_updater
      - composer_normalizer
      - unsupported_modules
  security:
    addons: []
```

Those `normal` values are the defaults. The `security` default is empty so a security fix
stays minimal and focused.

## Reading the "Report key" column

Addons that have something to say contribute a section under `addons` in the [run
report](../run-report.md). An addon with nothing to report is omitted rather than present
and empty.

Two addons deliberately report nothing at all:

- **`composer_diff`** — its content duplicates the top-level `packages` field.
- **`composer_normalizer`** — it only reorders `composer.json`.

Everything else reports even when its work is "only" a code change. The diff tells you
what changed but not whether an addon ran at all, and most addons log and swallow their
own failures — so an addon that silently did nothing looks exactly like one with nothing
to do unless it says so. See [Why a run
report](../../explanation/why-a-run-report.md).

## Order of execution

Addons subscribing to the same event run in priority order, and for two events that order
is deliberate rather than incidental:

- On `pre-composer-update`, `composer_audit` runs at the **highest** priority because it
  decides *what* the update is allowed to touch. Everything else reacts to that decision.
- On `post-code-update`, the order is `deprecations_remover` → `code_beautifier` →
  `composer_audit`. Rector's temporary `composer.json` churn must be committed before
  PHPCBF stages anything, and the final audit must see the final code.

See [The addon architecture](../../explanation/addon-architecture.md).

## Writing your own

See [Writing an addon](../../contributing/writing-an-addon.md).

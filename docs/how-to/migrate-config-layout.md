# Migrate the config layout

`addons` and `auto_merge` used to be top-level keys in `.drupdater.yaml`, each split by
mode. They now live under `run_types.<mode>`.

A config still in the old shape **fails at startup** with a message showing the
replacement, so this is a fail-fast migration rather than a silent behaviour change.

## The error

```text
in .drupdater.yaml: "addons" and "auto_merge" are now grouped per run type. Replace:
  addons:
    normal: [code_beautifier, ...]
    security: []
  auto_merge:
    normal: false
    security: true
with:
  run_types:
    normal:
      addons: [code_beautifier, ...]
      auto_merge: false
    security:
      addons: []
      auto_merge: true
```

## The migration

Invert the nesting: group by run type first, then by setting.

=== "Before"

    ```yaml
    sites: [default]
    timeout: 30m

    addons:
      normal:
        - code_beautifier
        - deprecations_remover
        - translations_updater
        - composer_normalizer
        - unsupported_modules
      security: []

    auto_merge:
      normal: false
      security: true
    ```

=== "After"

    ```yaml
    sites: [default]
    timeout: 30m

    run_types:
      normal:
        addons:
          - code_beautifier
          - deprecations_remover
          - translations_updater
          - composer_normalizer
          - unsupported_modules
        auto_merge: false
      security:
        addons: []
        auto_merge: true
    ```

Nothing else changes. `sites` and `timeout` are global and stay at the root; the values of
`addons` and `auto_merge` are carried over unchanged.

## Verify

```bash
drupdater check
```

```text
✓ .drupdater.yaml valid (sites: default)
✓ addon names resolve
```

That is the whole verification: `check` loads the file exactly as a run does, in about a
second, and names the sites it resolved. To see every resolved value — timeout, both
addon lists, both `auto_merge` flags — run the update itself with `--verbose`, which logs
them at debug level.

## Why the layout changed

Configuring a security run used to mean picking the `security` field out of every setting
in the file, scattered across as many blocks as there were settings. Keying on the run
type instead means it is one stanza to read and one to change.

It also keeps the run types from ever colliding with a future global key: anything under
`run_types` is unambiguously per-run-type, and anything at the root is unambiguously
global.

## Why the error message exists

The file is decoded strictly, so the old keys were already rejected — but only as:

```text
field addons not found in type internal.fileConfig
```

Which is accurate and completely unhelpful. The dedicated check runs before the strict
decode purely so the error can name the replacement. See [The configuration
model](../explanation/configuration-model.md).

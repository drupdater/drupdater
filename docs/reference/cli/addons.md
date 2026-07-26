# `drupdater addons`

Lists the addon names that can be set in [`.drupdater.yaml`](../configuration.md).

```text
drupdater addons
```

Takes no arguments and no flags.

## Output

```text
Addons you can set under run_types.normal.addons / run_types.security.addons in .drupdater.yaml:
  code_beautifier
  composer_normalizer
  deprecations_remover
  translations_updater
  unsupported_modules
```

## What is and is not listed

The command lists only the **configurable** addons — the ones it is meaningful to put in
a `run_types.*.addons` list. It deliberately omits:

- The four [mandatory addons](../addons/index.md) (`composer_allow_plugins`,
  `composer_patches`, `composer_diff`, `update_hooks`), which always run and cannot be
  disabled.
- [`composer_audit`](../addons/composer-audit.md), which is added automatically in
  `--security` mode.

An addon name in an active list that is not in the registry aborts the run:

```text
unknown addon "code_beautifer" (run "drupdater addons" to list valid names)
```

Names in **both** run type blocks are validated at startup, regardless of which mode the
run is in — so a typo in the `security` block fails a normal run too, rather than lying
in wait until the next security run.

For what each addon actually does, see the [addon reference](../addons/index.md).

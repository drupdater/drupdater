# `drupdater addons`

Lists the addon names that can be set in [`.drupdater.yaml`](../configuration.md).

```text
drupdater addons
```

Takes no arguments and no flags. The list comes from the Drupdater agent installed in Composer
— the [catalogue](../docker-images.md#drupdater-agent) it answers `drupdater:addons` with — so
the command needs `composer` on `PATH` with the agent installed, as every run does, and fails
without it.

## Output

```text
Addons you can set under run_types.normal.addons / run_types.security.addons in .drupdater.yaml
(a normal run that sets none gets the ones marked default):
  code_beautifier (default)
  composer_normalizer (default)
  deprecations_remover (default)
  translations_updater (default)
```

## What is and is not listed

The command lists only the **configurable** addons — the ones it is meaningful to put in
a `run_types.*.addons` list. It deliberately omits:

- The six [mandatory addons](../addons/index.md) (`composer_allow_plugins`,
  `composer_patches`, `composer_diff`, `update_hooks`, `composer_audit`,
  `unsupported_modules`), which always run and cannot be disabled.

An addon name in an active list that is not in the catalogue aborts the run:

```text
unknown addon "code_beautifer" (run "drupdater addons" to list valid names)
```

Names in **both** run type blocks are validated at startup, regardless of which mode the
run is in — so a typo in the `security` block fails a normal run too, rather than lying
in wait until the next security run.

For what each addon actually does, see the [addon reference](../addons/index.md).

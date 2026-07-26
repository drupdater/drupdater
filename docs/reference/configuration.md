# Configuration file

`.drupdater.yaml` describes **what the project needs**: which sites exist, how long a run
may take, and which addons should run. It is committed to the repository root, so the
settings travel with the project rather than with the pipeline that invokes it.

Everything about *how a particular run is invoked* — tokens, dry runs, concurrency — is a
[CLI flag](cli/drupdater.md#flags) instead. The two tiers do not overlap. For the
reasoning, see [The configuration model](../explanation/configuration-model.md).

## Location

Read from `<working-dir>/.drupdater.yaml`, or from the path given to `--config`.

**The file is optional.** A missing file is not an error: the built-in defaults apply. An
empty file, or one containing only comments, is treated the same way.

## Complete schema

```yaml
sites: [default]      # Drupal site directories to update (must not be empty)
timeout: 30m          # overall run timeout (Go duration; 0 disables)

run_types:            # per-run-type settings; --security picks which block applies
  normal:
    addons:                      # configurable addons (mandatory ones always run)
      - code_beautifier          # phpcbf code-style fixes
      - deprecations_remover     # drupal-rector deprecation removal
      - translations_updater     # interface translations
      - composer_normalizer      # normalize composer.json
      - unsupported_modules      # report modules with no supported release
    auto_merge: false            # merge the request once its pipeline passes
  security:
    addons: []                   # minimal by default — don't interfere with the fix
    auto_merge: false
```

The values above **are** the defaults. A file that sets only `sites` gets all of the rest
exactly as shown.

## Keys

### `sites`

| | |
|---|---|
| Type | list of strings |
| Default | `[default]` |
| Required | The key is optional, but must not be an empty list |

The Drupal site directory names to update — the directories under `web/sites/`. Each one
gets a baseline install, its own update hooks run, and its own configuration export.

An empty list is rejected:

```text
no sites configured: "sites" must list at least one Drupal site name
```

This is a hard error rather than a no-op because every per-site phase iterates this list.
An empty list would silently skip the baseline install, the update hooks and the config
export, and still open a merge request for an update that was never validated against a
site.

See [Update multiple sites](../how-to/update-multiple-sites.md).

### `timeout`

| | |
|---|---|
| Type | Go duration string, or `0` |
| Default | `30m` |

An overall deadline for the run. When it expires, the run's context is cancelled and
cleanup proceeds.

Accepts any [Go duration](https://pkg.go.dev/time#ParseDuration): `45m`, `2h`, `90s`. The
bare integer `0` disables the timeout entirely.

```yaml
timeout: 2h     # a large multi-site project
timeout: 0      # no deadline
```

An unparseable value fails at startup:

```text
invalid timeout "30 minutes" (use a Go duration like "30m" or "2h", or 0 to disable)
```

### `run_types`

Everything that differs between a normal update and a security update. Two blocks,
`normal` and `security`, with identical shapes. `--security` selects `security`;
otherwise `normal` applies.

#### `run_types.<type>.addons`

| | |
|---|---|
| Type | list of strings |
| Default (`normal`) | `[code_beautifier, deprecations_remover, translations_updater, composer_normalizer, unsupported_modules]` |
| Default (`security`) | `[]` |

The configurable addons to run. The four mandatory addons always run regardless, and are
not valid entries here. Run [`drupdater addons`](cli/addons.md) to list valid names, or
see the [addon reference](addons/index.md).

The security default is empty on purpose: a security update should be a minimal, focused
fix, so only the mandatory addons and the automatically-added
[`composer_audit`](addons/composer-audit.md) run.

Names in **both** blocks are validated at startup regardless of the active mode. An
unknown name aborts the run:

```text
unknown addon "code_beautifer" (run "drupdater addons" to list valid names)
```

#### `run_types.<type>.auto_merge`

| | |
|---|---|
| Type | boolean |
| Default | `false` in both blocks |

Ask the platform to merge the request as soon as its pipeline succeeds. Nothing is merged
while checks are failing or pending; if the project has no pipeline at all, the merge
happens immediately.

The two run types are separate on purpose: auto-merging routine dependency bumps is a
different risk decision from auto-merging a security fix.

Enabling auto-merge is best-effort — a failure is logged as a warning and recorded in the
[run report](run-report.md), but does not fail the run. See [Enable
auto-merge](../how-to/enable-auto-merge.md) for the platform requirements.

## Validation

### Unknown keys are rejected

The file is decoded strictly, so a typo fails loudly at startup rather than being
silently ignored:

```yaml
site: [default]     # should be "sites"
```

```text
parsing .drupdater.yaml: yaml: unmarshal errors:
  line 1: field site not found in type internal.fileConfig
```

### Partial files are fine

Decoding is layered over a fully-populated default struct, so only the keys actually
present are overridden. This is a complete, valid file:

```yaml
sites: [default, intranet]
```

### The pre-`run_types` layout is rejected with instructions

`addons` and `auto_merge` used to be top-level keys, each split by mode. A file still in
that shape produces an error naming the replacement rather than a bare "field not found".
See [Migrate the config layout](../how-to/migrate-config-layout.md).

## Examples

A single site that only wants dependency bumps and nothing else:

```yaml
sites: [default]
run_types:
  normal:
    addons: []
```

A large multi-site project that auto-merges security fixes but reviews everything else:

```yaml
sites: [default, intranet, careers]
timeout: 2h

run_types:
  normal:
    addons:
      - code_beautifier
      - deprecations_remover
      - composer_normalizer
      - unsupported_modules
    auto_merge: false
  security:
    addons: []
    auto_merge: true
```

A project with no custom code, so no code-style or deprecation work to do:

```yaml
sites: [default]
run_types:
  normal:
    addons:
      - translations_updater
      - composer_normalizer
      - unsupported_modules
```

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Does

**Drupdater** is a Go CLI tool that automates Drupal site updates: it runs against an existing checkout (or clones one with `--clone` for testing), runs Composer updates, applies code quality fixes (PHPCBF, Rector), updates Drupal config/translations, and opens a merge/pull request on GitHub or GitLab with a detailed changelog.

## Commands

```bash
make build          # Build binary
make test           # Run all tests (go test -v ./...)
make lint           # Run vet + staticcheck + golangci-lint
make fmt            # Format code
make mock           # Regenerate mocks (requires mockery v3)
make update         # Update Go dependencies
make docker-build   # Build multi-stage Docker image (Go binary + PHP runtime)
```

Run a single test:
```bash
go test -v -run TestName ./path/to/package/...
```

Run the tool locally:
```bash
make docker-run REPO=<git-url> TOKEN=<token>
```

## Architecture

### Entry Point → Workflow

`main.go` → `cmd/root.go` (Cobra) → `internal/services/workflow_base.go`

`root.go` is where CLI flags are parsed, core services are initialized (logger, cache, Composer/Drush/Git wrappers), the VCS provider (GitHub or GitLab) is detected via factory, and all addons are registered before the workflow starts. The repository URL is read from the checkout's `origin` remote unless `--clone`/`--repository-url` is given.

### Workflow Phases

The workflow in `workflow_base.go` operates on a **single working directory** (the existing checkout by default, or a fresh clone with `--clone`). Old and new code live there sequentially; phases run linearly, with per-site work fanned out concurrently (limited to CPU cores):

1. **acquire working copy** – open the existing checkout (default) or clone (`--clone`), then `composer install`
2. **installSite(s)** – install each Drupal site via Drush at the current (old) code to build the baseline database
3. **updateSharedCode** – run `composer update`, fire addon events, commit changes, create the update branch
4. **updateSite(s)** – run Drush update hooks and config export per site against the updated code

The site databases are SQLite files written beside the working directory (`{dir}/../{site}.sqlite`); checkout-mode runs clean these up afterward. At the end (unless `--dry-run`), a merge/pull request is created with a generated description.

### Addon System (`internal/addon/`)

Addons implement the `Addon` interface and subscribe to workflow events via `gookit/event`. They hook into pre/post composer update and pre/post site update events.

Which addons run is data-driven: `cmd/root.go` holds an `addonRegistry` (name → constructor) and `mandatoryAddons`. Four addons always run — `composer_allow_plugins`, `composer_patches`, `composer_diff`, `update_hooks` — and `composer_audit` is additionally mandatory in security mode. The rest are *configurable* and listed per mode in `.drupdater.yaml` under `addons.regular` / `addons.security`; `--security` selects which list is used. Configurable addon names: `code_beautifier`, `deprecations_remover`, `translations_updater`, `composer_normalizer`, `unsupported_modules`. An unknown name in the active list aborts the run.

Addons use Go templates in `internal/addon/templates/` to render the MR description sections.

### VCS Provider (`internal/codehosting/`)

Factory pattern in `factory.go` detects GitHub vs. GitLab from the repo URL and returns the appropriate implementation. Both implement the same platform interface for creating branches and merge/pull requests.

### `pkg/` — CLI Wrappers

Each subdirectory wraps an external tool:
- `pkg/composer/` – Composer commands (install, update, audit, normalize)
- `pkg/drush/` – Drush commands (site install, updatedb, config-import, translation)
- `pkg/repo/` – go-git operations (clone, checkout, commit, push)
- `pkg/phpcs/` – PHPCBF execution
- `pkg/rector/` – Drupal-Rector execution
- `pkg/drupalorg/` – HTTP calls to Drupal.org for patch metadata

### Events (`internal/services/event.go`)

Events fired during the workflow: `PreComposerUpdateEvent`, `PostComposerUpdateEvent`, `PostCodeUpdateEvent`, `PreSiteUpdateEvent`, `PostSiteUpdateEvent`, `PreMergeRequestCreateEvent`.

## Configuration

Config is split into two tiers, with no overlap:

- **CLI flags** — how a given run is invoked (volatile): token (positional arg, or the `DRUPDATER_TOKEN` env var when the arg is omitted), plus the flags below.
- **`.drupdater.yaml`** — what the project needs (committed at the repo root, read from `<working-dir>/.drupdater.yaml` or `--config`). Loaded by `internal/configfile.go`; a missing file falls back to built-in defaults, absent keys keep their default, and unknown keys are rejected (strict decode). Keys: `sites`, `timeout` (Go duration string, e.g. `30m`), and `addons.normal` / `addons.security`. Addon names in both lists are validated up front via `validateAddons`; `drupdater addons` lists valid names.

### CLI Flags

| Flag | Default | Effect |
|------|---------|--------|
| `--branch` | `main` | Branch to update/MR target; only used with `--clone` (checkout mode reads it from the checkout or CI branch var) |
| `--working-dir` | `.` | Existing checkout to update in place (also where `.drupdater.yaml` is read from) |
| `--clone` | false | Clone instead of using the checkout (needs `--repository-url`); for testing |
| `--repository-url` | _(from `origin`)_ | Repo URL; required with `--clone`, else read from `origin` |
| `--security` | false | Only apply security updates; selects the `addons.security` list |
| `--dry-run` | false | Skip branch creation and MR |
| `--verbose` | false | Debug-level structured logging |
| `--config` | _(`<working-dir>/.drupdater.yaml`)_ | Path to the config file |

### `.drupdater.yaml`

```yaml
sites: [default]      # Drupal site names
timeout: 30m          # overall run timeout (Go duration; 0 disables)
addons:               # configurable addons per mode; mandatory addons always run
  normal:
    - code_beautifier
    - deprecations_remover
    - translations_updater
    - composer_normalizer
    - unsupported_modules
  security: []            # minimal by default; composer_audit is added automatically
```

## Mocking

Mocks are generated with mockery v3 (config in `.mockery.yml`). After changing an interface, regenerate with `make mock`. Mock files live alongside their source packages with a `mock_*.go` naming pattern.

## Docker

The Dockerfile is multi-stage: stage 1 builds the Go binary, stage 2 is a PHP 8.3 runtime image with Composer and required PHP extensions. The Go binary is copied into the PHP image as the final artifact.

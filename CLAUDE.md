# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Does

**Drupdater** is a Go CLI tool that automates Drupal site updates: it clones a repo, runs Composer updates, applies code quality fixes (PHPCBF, Rector), updates Drupal config/translations, and opens a merge/pull request on GitHub or GitLab with a detailed changelog.

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
make run REPO=<git-url> TOKEN=<token>
```

## Architecture

### Entry Point → Workflow

`main.go` → `cmd/root.go` (Cobra) → `internal/services/workflow_base.go`

`root.go` is where CLI flags are parsed, core services are initialized (logger, cache, Composer/Drush/Git wrappers), the VCS provider (GitHub or GitLab) is detected via factory, and all addons are registered before the workflow starts.

### Workflow Phases

The workflow in `workflow_base.go` runs these phases with goroutine-based concurrency (limited to CPU cores):

1. **installCode** – clone the repo and run `composer install`
2. **installSite(s)** – install each Drupal site via Drush
3. **updateSharedCode** – run `composer update`, fire addon events, commit changes
4. **updateSite(s)** – run Drush update hooks and config import per site

At the end (unless `--dry-run`), a merge/pull request is created with a generated description.

### Addon System (`internal/addon/`)

Addons implement the `Addon` interface and subscribe to workflow events via `gookit/event`. They hook into pre/post composer update and pre/post site update events. The 10 addons include: ComposerAudit (security only flag), CodeBeautifier (`--skip-cbf`), DeprecationsRemover (`--skip-rector`), TranslationsUpdater, ComposerAllowPlugins, ComposerNormalizer, ComposerPatches1, ComposerDiff, UpdateHooks.

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

## Key Flags

| Flag | Default | Effect |
|------|---------|--------|
| `--branch` | `main` | Target branch to update from |
| `--sites` | `default` | Comma-separated Drupal site names |
| `--security` | false | Only apply security updates |
| `--skip-cbf` | false | Skip PHP Code Beautifier |
| `--skip-rector` | false | Skip Drupal-Rector deprecation removal |
| `--dry-run` | false | Skip branch creation and MR |
| `--verbose` | false | Debug-level structured logging |

## Mocking

Mocks are generated with mockery v3 (config in `.mockery.yml`). After changing an interface, regenerate with `make mock`. Mock files live alongside their source packages with a `mock_*.go` naming pattern.

## Docker

The Dockerfile is multi-stage: stage 1 builds the Go binary, stage 2 is a PHP 8.3 runtime image with Composer and required PHP extensions. The Go binary is copied into the PHP image as the final artifact.

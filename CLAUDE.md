# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Does

**Drupdater** is a Go CLI tool that automates Drupal site updates: it runs against an existing checkout (or clones one with `--clone` for testing), runs Composer updates, applies code quality fixes (PHPCBF, Rector), updates Drupal config/translations, and opens a merge/pull request on GitHub or GitLab with a detailed changelog.

Drupdater itself runs as a step in the project's CI pipeline, which already runs the project's test suite (PHPUnit/Behat/etc.) on the resulting MR/branch. Don't propose or add test-running functionality inside Drupdater — that responsibility belongs to CI, not this tool.

## Documentation lives in `docs/`

**`docs/` is the single source of truth for user-facing and architectural documentation**, published to <https://drupdater.github.io/> by `.github/workflows/docs.yml` on every push to `main`. It is organized on the [Diátaxis](https://diataxis.fr/) model:

| Directory | Contains |
|---|---|
| `docs/tutorials/` | Learning-oriented walkthroughs |
| `docs/how-to/` | Task-oriented recipes |
| `docs/reference/` | CLI flags, `.drupdater.yaml` schema, all addons, report schema, preflight checks, Docker images |
| `docs/explanation/` | Workflow phases, addon architecture, config model, credential handling, VCS detection, non-goals |
| `docs/contributing/` | Development setup, writing an addon, releasing, security policy |

**Do not duplicate reference material in this file or in the README.** The CLI flag table and the `.drupdater.yaml` schema used to exist in three places and drifted. They now live in `docs/reference/` only.

### Keep docs in sync with code

Before finishing any task, check whether it touched something in the table below and, if so,
update the matching page — don't leave it for a follow-up.

A change to any of the following **must** update its page in the same PR:

| Change | Page to update |
|---|---|
| A CLI flag | `docs/reference/cli/drupdater.md` (or `check.md`) |
| A `.drupdater.yaml` key or default | `docs/reference/configuration.md` |
| An addon — behaviour, events, report data | `docs/reference/addons/<name>.md` **and** the catalogue table in `docs/reference/addons/index.md` |
| The report schema | `docs/reference/run-report.md` |
| A preflight check | `docs/reference/preflight-checks.md` |
| An environment variable | `docs/reference/environment-variables.md` |
| A workflow phase or event | `docs/explanation/how-a-run-works.md`, `docs/explanation/addon-architecture.md` |
| Published PHP versions | `docs/reference/docker-images.md` |

Verify with `make docs-build` (runs `mkdocs build --strict`, which fails on broken internal links and bad nav entries). Preview with `make docs-serve`.

Some pages embed files from the repo via `pymdownx.snippets`, so they cannot drift:

- `internal/addon/testdata/*.md` → the "pull request section" examples on addon pages
- `.github/assert-report.jq`, `.github/assert-lock-matches-report.jq` → `docs/how-to/consume-the-run-report.md`

Changing a golden file changes the published example. `internal/addon/testdata/composer_diff.md` is a `Dummy Table` placeholder and is deliberately *not* embedded — that page hand-writes its example.

## Commands

```bash
make build          # Build binary
make test           # Run all tests (go test -v ./...)
make test-property  # Only the property tests, with far more generated cases (rapid)
make mutate         # Mutation testing over the whole module (mutago, pinned in go.mod)
make lint           # golangci-lint (govet, staticcheck, gosec, etc. — see .golangci.yml) + hadolint on the Dockerfile
make fmt            # Format code
make fix            # Apply go fix modernizers (interface{} → any, strings.Cut, etc.)
make deadcode       # Find unreachable functions (go tool deadcode)
make mock           # Regenerate mocks (requires mockery v3)
make update         # Update Go dependencies
make docker-build   # Build multi-stage Docker image (Go binary + PHP runtime)
make docs-serve     # Preview the documentation site
make docs-build     # Build the documentation with --strict
```

`make lint`'s hadolint step shells out to `docker run hadolint/hadolint`, which needs a Docker
daemon and registry access. In the Claude Code remote/web environment neither is available, so
that step fails to even start — run `golangci-lint run ./...` directly there instead of `make
lint` and don't treat the hadolint failure as a real lint error.

Run a single test:
```bash
go test -v -run TestName ./path/to/package/...
```

Run the tool locally:
```bash
make docker-run REPO=<git-url> TOKEN=<token>
```

## Code Map

Where things live. For *how they work*, read `docs/explanation/` rather than duplicating it here.

| Path | Contains |
|---|---|
| `main.go` → `cmd/root.go` | Cobra commands, flag parsing, service construction, `addonRegistry`, `mandatoryAddons` |
| `cmd/check.go` | The `check` command and its cheap/full check tiers |
| `internal/services/workflow_base.go` | `StartUpdate` and the eight phases |
| `internal/services/event.go` | The six workflow events and `AbortError` |
| `internal/services/preflight.go` | Checks shared between `check` and the run's own `preflight` phase |
| `internal/addon/` | The ten addons |
| `internal/addon/report.go` | **Every addon's report contribution, together** — this file *is* the report's `addons` schema |
| `internal/addon/templates/` | Go templates rendering the MR description sections |
| `internal/configfile.go` | `.drupdater.yaml` loading, defaults, strict decode, legacy-layout rejection |
| `internal/codehosting/` | GitHub and GitLab implementations, provider factory |
| `internal/report/` | The published JSON report schema and its atomic writer |
| `internal/logging/redact.go` | Value-based secret redaction, wrapping the zap core |
| `pkg/` | One directory per wrapped external tool: `composer`, `drush`, `repo` (go-git), `phpcs`, `rector`, `drupal` (installer), `drupalorg` |
| `scripts/` | PHP helpers copied into the image (`rector.php`, `unsupported-modules.php`, `config-resave.php`) |

### Invariants worth knowing before editing

- **Addons never call each other.** They communicate only through mutable event payloads (`PackagesToUpdate`, `PackagesToKeep`, `MinimalChanges`, `Title`).
- **Event priority is load-bearing** on `pre-composer-update` and `post-code-update`. See `docs/explanation/addon-architecture.md` before changing one.
- **`internal/addon/report.go` is a published contract.** Renaming a field there is a breaking change to `schema_version`.
- **Report types mirror rather than reuse internal types** (`report.PackageChange` vs `composer.PackageChange`) so an internal refactor can't rename a published field.
- **The report's deferred write is registered first**, so it runs last and is emitted on every exit path.
- **Per-site events fire concurrently.** Addon state accumulated across sites must be mutex-guarded, and maps handed to the report must be copies.

## Comments

**Default to no comment.** Readable code with a good name is the goal; a comment is what you fall
back to when the *reason* for the code can't be expressed in the code itself.

Hard rules:

- **One line. Under ~100 characters.** If it doesn't fit, the comment is explaining too much — cut it
  to the single non-obvious fact, or delete it. Multi-line blocks are reserved for doc comments on
  exported API and for `internal/addon/report.go`'s published schema.
- **Say *why*, never *what*.** The code already says what. `// lock the mutex`, `// returns an error
  if the path is empty`, `// loop over the packages` — all deleted.
- **No signature restating**, no `// Foo does X` on unexported helpers whose name already says X.
- **No narration of the diff**: `// changed to handle the new case`, `// moved from workflow_base`,
  `// see PR #123`. Git holds that.
- **No commented-out code**, no TODO/FIXME without a concrete reason, no section banners
  (`// --- helpers ---`), no filler.
- **Tests**: name the test after what it asserts instead of commenting it. No `// arrange` /
  `// act` / `// assert` scaffolding.

The comments worth keeping are the ones a reader can't derive: a workaround for an upstream bug (name
it), a non-obvious ordering or concurrency constraint, an event-priority dependency, a deliberate
deviation from the obvious implementation. Everything else goes.

```go
// bad — three lines restating the code
// getPackages collects the packages that need updating. It iterates over
// the installed packages, filters out the ones in PackagesToKeep and
// returns the remaining slice.

// good — one line, states what the reader can't see
// Composer resolves patches lazily, so the lock has to be re-read after `require`.
```

## Configuration

Two tiers with no overlap: **CLI flags** (how a run is invoked) and **`.drupdater.yaml`** (what the project needs, committed at the repo root). `Config.ActiveRunType()` is the single place `--security` maps to a config block — call it rather than branching on `config.Security`.

Full schema, defaults and validation rules: `docs/reference/configuration.md`. Rationale: `docs/explanation/configuration-model.md`.

## Mocking

Mocks are generated with mockery v3 (config in `.mockery.yml`). After changing an interface, regenerate with `make mock`. The `testify` template emits one consolidated `mocks_test.go` per package, in the package it mocks.

## Mutation testing

`mutago` (pinned via the `tool` directive in `go.mod`, configured in `mutago.yaml`) scores whether the tests *assert* rather than merely execute. CI enforces it on the lines a PR changes (`mutation` job in `.github/workflows/go.yml`, fails below 75 % MSI) and reports the whole module weekly (`.github/workflows/mutation.yml`, one matrix leg per package, never blocking). Suppress a genuinely equivalent mutant with a `// mutator-disable-next-line <mutator>` comment and a reason — never by lowering the threshold. Details: `docs/contributing/development.md`.

## Property-based testing

`pgregory.net/rapid` states invariants over generated input, next to the example-based tests. Convention: file `<subject>_property_test.go`, every test named `TestProperty…` (`make test-property` selects on that prefix). A property must state a law — idempotent, order-independent, round-trips, leaks nothing — never a second implementation of the function.

**The seed is random on every run.** So when a property finds a bug, fix the code *and* add an ordinary test naming the counterexample: the blocking mutation gate must not depend on the exploration happening to hit the right input. Counterexamples rapid records under `testdata/rapid/*.fail` are replayed automatically and belong in the commit. Details: `docs/contributing/development.md`.

## Docker

The Dockerfile is multi-stage: stage 1 builds the Go binary, stage 2 is a PHP runtime image (`PHP_VERSION` build arg, default 8.3) with Composer and required PHP extensions. The Go binary is copied into the PHP image as the final artifact.

Known gap: the Dockerfile's build stage does **not** pass `-ldflags`, so published images report `drupdater_version: "dev"` in the run report. Documented in `docs/reference/cli/index.md` and `docs/contributing/releasing.md`.

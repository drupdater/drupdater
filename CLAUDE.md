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
| An agent command | `docs/reference/docker-images.md#drupdater-agent`, `docs/contributing/development.md#the-agent` |

Verify with `make docs-build` (runs `mkdocs build --strict`, which fails on broken internal links and bad nav entries). Preview with `make docs-serve`.

Some pages embed files from the repo via `pymdownx.snippets`, so they cannot drift:

- `internal/mergerequest/testdata/*.md` → the "pull request section" examples on addon pages,
  rendered from the example report
- `internal/addon/testdata/run_report.json` → the example report on `docs/reference/run-report.md`
- `.github/assert-report.jq`, `.github/assert-lock-matches-report.jq` → `docs/how-to/consume-the-run-report.md`

Changing a golden file changes the published example.

## Commands

```bash
make build          # Build binary
make test           # Run all tests (go test -v ./...)
make test-race      # Run the suite under the race detector (~1 min; CI runs this too)
make test-agent     # The agent's PHPUnit suite (agent/; needs PHP and Composer)
make test-property  # Only the property tests, with far more generated cases (rapid)
make fuzz           # Fuzz every target (FUZZTIME=30s each by default)
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
| `main.go` → `cmd/root.go` | Cobra commands built per call by `app`, which holds every outside dependency |
| `cmd/addons.go` | `builtinAddons()`, the addon catalogue, and the `addonComposer`/`addonDrush` unions |
| `cmd/secrets.go`, `cmd/checkout.go` | Token resolution and secret registration; checkout URL/branch and `safe.directory` |
| `cmd/check.go` | The `check` command and its cheap/full check tiers |
| `internal/workflow/runner.go` | `Runner.Run` and the nine phases; their bodies in `update.go`, `sites.go`, `branch.go`, `publish.go` |
| `internal/workflow/event.go` | The five workflow events and `AbortError` |
| `internal/workflow/hooks.go` | Addon hooks, their stages, and the order they run in |
| `internal/workflow/preflight.go` | Checks shared between `check` and the run's own `preflight` phase |
| `internal/addon/` | The ten addons |
| `internal/addon/report.go` | **Every addon's report contribution, together** — this file *is* the report's `addons` schema |
| `internal/mergerequest/` | The MR title and description, rendered from the run report; its templates |
| `internal/config/` | `Config`; `file.go` loads `.drupdater.yaml`: defaults, strict decode, legacy-layout rejection |
| `internal/buildinfo/` | `Version`, set by `-ldflags` |
| `internal/codehosting/` | GitHub and GitLab implementations, provider factory |
| `internal/report/` | The published JSON report schema and its atomic writer |
| `internal/logging/redact.go` | Value-based secret redaction, wrapping the zap core |
| `internal/golden/` | Golden-file comparison and the `-update` flag; imported only from tests |
| `internal/composer/`, `drush/`, `repo/`, `phpcs/`, `rector/`, `drupalorg/` | One directory per wrapped external tool: `repo` is the git CLI, `drupalorg` is drupal.org issues and its GitLab at git.drupalcode.org |
| `internal/drupal/` | Making drush-reported absolute paths relative to the project |
| `internal/siteinstall/` | Installs a throwaway SQLite site for the `check --full` tier |
| `internal/php/` | What the PHP wrappers share: running a command under Composer's environment, decoding JSON output, and the `CommandFactory` each wrapper holds as its test seam |
| `scripts/` | PHP embedded in the binary, extracted per run: `rector.php` and the `drupdater:*` Drush commands (`drush --include`) |
| `agent/` | `drupdater/agent`, the global Composer plugin the image installs: the `composer drupdater:*` commands, its PHPUnit suite, and `protocol/`, the request/reply fixtures both sides test against |
| `internal/agent/` | `agent.Call`: runs a `composer drupdater:*` command, decodes its envelope, returns an `*agent.Error` with a `Kind` |
| `internal/composer/project.go` | `composer.Project`, the agent's snapshot of the project: web root, custom code, `allow-plugins` and `extra.patches` as written, locked packages, dependency patches, lock hash. Ask again after anything rewrites `composer.json` or the lock |
| `tools/` | The PHPCS and Rector the image installs to `/opt/drupdater/tools`, so addons install nothing into the project |

### Invariants worth knowing before editing

- **Addons never call each other.** On `pre-composer-update` they return a `workflow.UpdatePlan` proposal and read the merged plan of earlier stages (`internal/workflow/plan.go`); no event is mutable.
- **A run never touches the user's checkout.** Checkout mode works in a linked worktree (`repo.AddWorktree`), commits are signed explicitly, and nothing writes the repository's git config; the update branch is the only thing a run leaves behind.
- **Every git operation goes through `repo.Git`**, the git CLI with `commonArgs` (`internal/repo/repo.go`): ownership check off, no hooks, no signing. A token reaches git only through the credential helper's environment, never argv or a config file.
- **Addons never touch git.** A `workflow.Produce` (or, on `pre-composer-update`, `workflow.ProduceProposal`) hook returns the paths it changed as `Commits`, and an `On` hook can commit nothing; the runner's committer (`internal/workflow/commit.go`) commits exactly those, and refuses when something else is already staged.
- **Each addon declares the tool interfaces it uses**, in its own file, with only the methods it calls (`AuditComposer`, `UpdateHooksDrush`). No two addons share one — `TestNoTwoAddonsShareAToolInterface` fails otherwise — and `cmd`'s `addonComposer`/`addonDrush` embed them all.
- **An event type names itself and its `Hooks` field** (`name()`, `hook()` in `internal/workflow/event.go`), so `workflow.Fire` runs any event and `Pipeline` has no per-event code. A new event is that type, its `Hooks` field, and the `Fire` call in the runner.
- **Hook stages are load-bearing** on `pre-composer-update` and `post-code-update`. See `docs/explanation/addon-architecture.md` before changing one.
- **The MR is rendered from the report**, through its JSON. An addon reaches the description only via its report section; findings from two addons are joined there, never handed between them.
- **A new question for Composer goes to the agent, not to its console output.** The agent answers in JSON with a closed error `Kind`, decided from the exception it caught. A change to a request or reply changes `agent/protocol/` and both sides' tests; a breaking one bumps `protocol` in `agent/src/Protocol.php` and `internal/agent/agent.go` together.
- **`internal/addon/report.go` is a published contract.** Renaming a field there is a breaking change to `schema_version`, and `internal/addon/testdata/run_report.json` is what makes such a rename fail rather than ship. A new reporting addon belongs in that golden's `reportingAddons()`.
- **Report types mirror rather than reuse internal types** (`report.PackageChange` vs `composer.PackageChange`) so an internal refactor can't rename a published field.
- **The report's deferred write is registered first**, so it runs last and is emitted on every exit path.
- **`cmd` has no mutable package state.** Everything that reaches outside the process — building the runner and the platform, the `--full` tier's clone, trusting a checkout in the global git config — is a field of `app`, and commands are built per call by `app.newRootCmd()`. A test overrides a field on its own `testApp()`; it never swaps a package variable.
- **A `workflow.Runner` holds only its shared `services`.** Per-run state lives on `run`, built complete by `newRun` once the working copy exists; nothing on it is assigned later.
- **Sites update in two steps.** `pre-site-update` and the database update run from a goroutine per site, concurrently; once every site is done, `post-site-update`, config export and the site's commit run in a plain loop in `--sites` order (`Runner.updateSites`). Like every hook, a per-site hook *returns* its finding; the pipeline records it in the addon's `workflow.Findings`, which `ReportData(*workflow.Findings)` and the addon's later `workflow.ProduceFrom` hooks read. No addon has a map, slice or mutex field of its own — `TestAddonsKeepNoStateOfTheirOwn` fails the build otherwise.

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
it), a non-obvious ordering or concurrency constraint, a hook-stage dependency, a deliberate
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

## Fuzzing

Go's built-in fuzzer covers what a hand-written rapid generator cannot reach: arbitrary bytes, invalid UTF-8, degenerate separators. Convention: file `<subject>_fuzz_test.go`, every target named `Fuzz…`, seeded via `f.Add` with the real shapes *and* the degenerate ones. Three targets, on input drupdater does not control — the repository URL, `.drupdater.yaml`, and the redactor.

Same discipline as properties: a failing input lands in `testdata/fuzz/<target>/<hash>`, gets committed (`go test` replays it forever after), **and** gets an ordinary test naming the counterexample, because the blocking mutation gate must not depend on a generative run. Fuzzing itself is not a PR gate — `fuzz.yml` runs weekly; `go.yml`'s `test` job runs the seeds and counterexamples on every push. Details: `docs/contributing/development.md`.

## Docker

The Dockerfile is multi-stage: stage 1 builds the Go binary, stage 2 is a PHP runtime image (`PHP_VERSION` build arg, default 8.3) with Composer and required PHP extensions. The Go binary is copied into the PHP image as the final artifact.

Known gap: the Dockerfile's build stage does **not** pass `-ldflags`, so published images report `drupdater_version: "dev"` in the run report. Documented in `docs/reference/cli/index.md` and `docs/contributing/releasing.md`.

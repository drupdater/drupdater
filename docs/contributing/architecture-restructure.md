# Architecture restructure (working document)

A working plan for restructuring the packages. It is deleted once the restructure lands; the
lasting result goes into `CLAUDE.md`'s Code Map, `docs/contributing/development.md` and
`docs/explanation/addon-architecture.md`.

Ground rule throughout: **no behaviour change.** Every golden file stays byte-identical, and
flags, `.drupdater.yaml` keys, env vars, events, hook stages and `schema_version` stay as they are.

## 1. Current dependency graph

From `go list -f '{{.Imports}}'` and `{{.TestImports}} {{.XTestImports}}`, module-internal edges
only. Module prefix `github.com/drupdater/drupdater` dropped.

```text
L5  main ─► cmd
L4  cmd ─► internal, internal/addon, internal/codehosting, internal/logging, internal/report,
           internal/workflow, pkg/composer, pkg/drupal, pkg/drupalorg, pkg/drush,
           pkg/phpcs, pkg/rector, pkg/repo                     (13 of 16 packages)
L3  internal/addon ─► internal/workflow, pkg/composer, pkg/drupalorg, pkg/drush, pkg/phpcs,
                      pkg/rector
L2  internal/workflow ─► internal, internal/codehosting, internal/mergerequest,
                         internal/report, pkg/composer, pkg/repo
L1  internal/mergerequest ─► internal/report
    pkg/drupal ─► pkg/composer
    pkg/drush  ─► pkg/composer
    pkg/phpcs  ─► pkg/composer
    pkg/rector ─► pkg/composer
L0  internal (Config, Version), internal/codehosting, internal/golden, internal/logging,
    internal/report, pkg/composer, pkg/drupalorg, pkg/repo         (no internal imports)
```

Test-only edges (not present in the non-test graph):

| Package's tests | Extra imports |
|---|---|
| `cmd` | none beyond its own imports, minus `pkg/drupal`, `pkg/phpcs`, `pkg/rector` |
| `internal/addon` | `internal/golden`, `internal/report` |
| `internal/mergerequest` | `internal/addon`, `internal/golden`, `pkg/composer`, `pkg/drush` (render the example report) |
| `internal/report` | `internal/logging` |
| `internal/workflow` | `internal/golden` |

There are no cycles. `make deadcode` is clean today.

## 2. One responsibility per package

| Package | Responsibility | Verdict |
|---|---|---|
| `cmd` | Parses flags **and** builds every service **and** owns the addon registry and `mandatoryAddons` **and** resolves the token, secrets and project config. | Split: the wiring belongs in a composition root |
| `internal` | Holds the run `Config` **and** loads `.drupdater.yaml` **and** the build `Version`. | Split and rename |
| `internal/addon` | Implements the ten addons and their report sections. | OK (`report.go` is the deliberate, documented exception, §3.7) |
| `internal/codehosting` | Opens merge requests on GitHub or GitLab, picked from the repository URL. | OK |
| `internal/golden` | Compares test output with golden files. | OK, test-only |
| `internal/logging` | Redacts secrets from log output. | OK |
| `internal/mergerequest` | Renders the MR title and description from a run report. | OK |
| `internal/report` | Defines the run report envelope, records it during a run and writes it atomically. | OK |
| `internal/workflow` | Orchestrates a run through its phases and fires addon events. | OK as a package; `runner.go` mixes phases with branch/push plumbing (§3.5) |
| `pkg/composer` | Runs Composer **and** parses audit/diff/update output **and** the allow-plugins policy **and** lock hashing **and** a scratch project for patch tests **and** the shared exec helper for other tools. | Split (files) and extract the exec helper |
| `pkg/drupal` | Installs a Drupal site onto a throwaway SQLite database. | OK |
| `pkg/drupalorg` | Queries drupal.org issues. | OK |
| `pkg/drush` | Runs drush. | OK |
| `pkg/phpcs` | Runs PHPCS/PHPCBF. | OK |
| `pkg/rector` | Runs Rector. | OK |
| `pkg/repo` | Opens, clones and worktrees git repositories through go-git. | OK |

## 3. The suspected issues, checked

### 3.1 `internal` as a package — confirmed

`internal/config.go` and `internal/configfile.go` are `package internal`. 13 files spell
`internal.Config` / `internal.Version` (`cmd/*`, `internal/workflow/runner*.go`). The name says
nothing, and it holds two unrelated things:

- `Config`, `RunTypesConfig`, `ActiveRunType` and `.drupdater.yaml` loading — configuration.
- `Version`, set by `-ldflags "-X github.com/drupdater/drupdater/internal.Version=…"` in the
  `Makefile` (line 7) and the `Dockerfile` (line 27) — build metadata.

**Move:** `internal/config` (package `config`) and `internal/buildinfo` (`buildinfo.Version`). The
`-X` path changes in both files in the same commit; `drupdater --version` is the check.

Naming note: `cmd/root.go` has a package-level `var config internal.Config`, which would shadow
the new package name. That variable becomes `cfg` in the same commit.

### 3.2 Tool wrappers depending on Composer — confirmed, with a nuance

What each wrapper takes from `pkg/composer`:

| Wrapper | Uses |
|---|---|
| `pkg/drush` | `composer.Command` only |
| `pkg/phpcs` | `composer.Command` only |
| `pkg/rector` | `composer.Command` only (also as a parameter type in `projectAutoloader`) |
| `pkg/drupal` | `composer.WebRoot` — real Composer semantics (`extra.drupal-scaffold`) |

The nuance: drush and the project's own phpcs/rector really do run through `composer exec`, and
`Command.build` applies `composer.Env` (`COMPOSER_PROCESS_TIMEOUT=0`, `COMPOSER_NO_AUDIT=1`) to
**every** invocation, including the bundled tools that set `Program`. So the environment is not a
Composer-only concern; it is the environment every PHP tool drupdater spawns runs under.

**Move:** a leaf package `internal/tools/phptool` holding `CommandFactory`, `Command` and `Env`
(the code in `pkg/composer/exec.go` plus `Env`/`requiredComposerEnv`), unchanged in behaviour.
`composer`, `drush`, `phpcs` and `rector` import it; three of the four `→ composer` edges disappear.
`drupal → composer` stays, because `WebRoot` is genuine Composer knowledge.

### 3.3 `pkg/composer/composer.go` (1009 lines) — confirmed

Responsibilities by line range today:

| Lines | Concern | Target file |
|---|---|---|
| 22–103 | `CLI`, options, `Env`, exec plumbing | `cli.go` (Env goes to `phptool`, §3.2) |
| 104–135 | `Versions` | `cli.go` |
| 136–228 | `Update`/`Install`, update-output parsing | `update.go` |
| 229–365 | `Audit` and its JSON flattening | `audit.go` |
| 366–420 | `CheckPlatformReqs` | `platform.go` |
| 421–505 | `Normalize`, `Diff` and its parsing | `diff.go` |
| 506–602, 928–954, 980–1009 | config get/set/unset, `AllowPlugins`, installed packages/plugins, `WebRoot`, custom code dirs | `config.go`, `packages.go` |
| 603–927 | dependency patches, scratch project, patch-apply checks | `patches.go` |
| 955–979 | lock hash | `lock.go` |

Sub-packages: **rejected.** Every piece is a method on `*CLI` sharing its exec and temp-dir state,
and all of them have the same dependents (`addon`, `workflow`, `cmd`). Nothing has a distinct
dependent that a sub-package would serve. Files only; tests (`composer_test.go`, 1150 lines;
`composer_extra_test.go`, 507) split along the same lines.

### 3.4 `pkg/` vs `internal/` — move

Nothing in `pkg/` is meant for outside consumers:

- No doc, README section or changelog offers it as a library; `docs/contributing/development.md`
  describes it as "thin wrappers around external tools" for this binary.
- The APIs are shaped by drupdater: `drush.CLI` takes an otter cache, `phpcs.NewBundledCLI`
  takes the image's tool path, and `composer` bakes in drupdater's environment policy.
- No external repository can rely on them without accepting breaking changes on every release;
  `internal/` makes that explicit and lets us change them freely.

Cost: anyone importing `github.com/drupdater/drupdater/pkg/...` today breaks. Acceptable for a
CLI's helpers, and stated in the release notes.

**Move:** the PHP tool wrappers go to `internal/tools/{composer,drush,phpcs,rector,drupal}`
(beside the new `phptool`); `pkg/repo` → `internal/repo`; `pkg/drupalorg` → `internal/drupalorg`.
Package names stay the same, so only import paths change. `repo` and `drupalorg` are not PHP
tools, so they sit at the top of `internal/`.

### 3.5 `runner.go` (601 lines) and `cmd/root.go` (527 lines) — partly confirmed

`runner.go` mixes three things in one file:

1. phase orchestration (`Run`, `runPhases`, `renderMergeRequest`);
2. shared-code and site work (`resolveUpdate`, `updateSharedCode`, `stageScaffoldChanges`,
   `forEachSite`, `updateSite`, `commitSiteChanges`, `toReportPackages`);
3. branch and publish plumbing with go-git types (`createUpdateBranch`,
   `ensureUpdateBranchAvailable`, `removeUpdateBranch`, `publishWork`).

VCS, report and MR are **already behind narrow seams**: `Platform` and `Repository` are
workflow-owned interfaces, the report is a `*report.Recorder` plus an optional sink, and MR
rendering is one call to `mergerequest.Description/Title`. Wrapping those further adds an
abstraction with no second implementation. **Move:** split the file along the three lines above
(pure move, same package). No new interfaces.

`cmd/root.go` confirmed mixed: flags and Cobra commands, token resolution, secret registration,
`.drupdater.yaml` loading, git safe-directory, service construction, `addonRegistry`,
`mandatoryAddons`, `createAddons`, `validateAddons`, `configurableAddons`. `cmd/check.go` builds
services again in `newFullCheckDeps`. `cmd` therefore imports 13 of the 16 packages.

**Move:** a composition-root package `internal/app` that owns the addon registry
(`addonRegistry`, `mandatoryAddons`, `bundledToolsBin`, building and validating the addon list,
listing configurable addons) and service construction for `run` and `check --full`. `cmd` keeps
Cobra, flags, token/secret handling, logger setup and the test seams it already has
(`newRunner`, `newFullCheckDeps`, `newVcsProvider`, `osExit`). The registry's tests move with it.

### 3.6 Interfaces owned by consumers — mostly yes, two exceptions

Good already: `workflow.{Composer,Drush,Repository,Installer,Platform}`, the preflight
interfaces, `drupal.{Drush,Composer}`, `cmd`'s `cheapChecksComposer`/`fullCheckComposer`/
`siteInstaller`, `addon.{Drush,PHPCS,Rector,DrupalOrg,HTTPClient}`.

Flagged:

- **`addon.Composer` is fat**: 16 methods shared by seven addons, each using 1–8 of them
  (`composer_diff` uses only `Diff`; `composer_audit` only `Audit`). **Move:** one small
  interface per consuming addon, declared next to it; regenerate mocks; tests swap
  `NewMockComposer` for the narrow mock with the same `EXPECT()` calls. `cmd`/`app` then hold
  the concrete `*composer.CLI`.
- **`workflow.GitRepository` / `workflow.Worktree`** are type aliases of `repo.Repository` /
  `repo.Worktree` "to avoid duplication" — re-export shims. **Move:** delete them and spell
  `repo.Repository` / `repo.Worktree` (no outside users: zero references outside `workflow`).
- `repo.Repository` / `repo.Worktree` are defined next to their implementation. **Left alone:**
  `repo.Checkout` returns them, and inverting would make `repo` return raw `*git.Repository` and
  move the `SetBranch`/`RemoveBranch` adapter into `workflow` — churn with no new dependent.
- `codehosting.Platform` sits next to its implementations because `codehosting.New` returns it;
  `workflow` declares its own. **Left alone.**
- `composer.ConfigGetter` is the parameter type of `WebRoot`. Idiomatic. **Left alone.**

### 3.7 `internal/report` vs `internal/addon/report.go` — deliberate, one leak found

The split is deliberate and documented (`report.go` header; `CLAUDE.md` invariants;
`writing-an-addon.md`): `internal/report` owns the envelope, recorder and writer and knows
addons only through the structural `report.Reporter`; `internal/addon/report.go` owns every
section, together, so a rename is visible in one file. **Not merged.**

Leak: the invariant "report types mirror rather than reuse internal types" is broken in
`addon/report.go`. `SecurityAdvisories` publishes `composer.Advisory` (and the nested
`composer.Source`) and `composer.AbandonedPackage`; `unsupported_modules` publishes
`drush.UnsupportedModule` as-is. A rename of a field in the composer/drush wrapper would silently
change the report. **Move:** mirror these as report types in `addon/report.go` with identical JSON
tags and field order; the golden proves the bytes are unchanged.

### 3.8 Edges that point the wrong way or exist only to reach a helper

| Edge | Why | Fix |
|---|---|---|
| `drush`, `phpcs`, `rector` → `composer` | reach the exec helper | §3.2 |
| `workflow` → `internal` (for `Version`) | build metadata reached through the config package | §3.1 |
| `cmd` → `phpcs`, `rector`, `drupalorg`, `addon` | wiring in the CLI layer | §3.5 |
| `addon/report.go` → `composer.Advisory`, `drush.UnsupportedModule` in the published schema | reuse of internal types | §3.7 |

Not wrong: `workflow → composer` (`PackageChange`, `Versions`, `WebRoot`), `workflow →
codehosting` (`MergeRequest` value type), `workflow → mergerequest` (rendering is a phase).

## 4. Target

### Layout

```text
main.go                    → cmd
cmd/                       Cobra commands, flags, token and logger setup
internal/
  app/                     composition root: services, addon registry, mandatory addons
  workflow/                runner, phases, events, hooks, preflight, committer
  addon/                   the ten addons; report.go = their report sections
  mergerequest/            MR title and description from the report
  report/                  report envelope, recorder, atomic writer
  config/                  Config, run types, .drupdater.yaml
  buildinfo/               Version (set by -ldflags)
  codehosting/             GitHub and GitLab
  repo/                    go-git
  drupalorg/               drupal.org API
  logging/                 redaction
  golden/                  golden files (tests only)
  tools/
    phptool/               run a PHP tool in a dir, with logging and the image's env
    composer/ drush/ phpcs/ rector/ drupal/
```

### Layering rule

A package may import only packages in a **lower** layer (same-layer imports are forbidden unless
listed).

| Layer | Packages | May import (internal) |
|---|---|---|
| 0 leaves | `buildinfo`, `config`, `logging`, `golden`, `report`, `codehosting`, `repo`, `drupalorg`, `tools/phptool` | nothing |
| 1 tools | `tools/composer`, `tools/drush`, `tools/phpcs`, `tools/rector` | `tools/phptool` |
|   | `tools/drupal` | `tools/composer` (for `WebRoot`) |
| 2 | `mergerequest` | `report` |
| 3 | `workflow` | `config`, `buildinfo`, `report`, `mergerequest`, `codehosting`, `repo`, `tools/composer` |
| 4 | `addon` | `workflow`, `tools/*`, `drupalorg` — **not** `report`, `mergerequest`, `codehosting`, `repo` |
| 5 | `app` | anything in `internal/` except `golden` |
| 6 | `cmd` | `app`, `config`, `buildinfo`, `workflow`, `report`, `logging`, `codehosting`, `repo` — **not** `addon` or `tools/*` |

Plus: `golden` only from `_test.go`; tests may additionally import `golden` and lower layers, and
`mergerequest`'s tests may import `addon` to render the example report.

Enforcement (Phase 3): `depguard` rules in `.golangci.yml`, one `files:` glob per layer with a
deny list. If a rule cannot be expressed (test-file exceptions), a small `go test` that walks
`go list -deps -test` and fails on a forbidden edge.

### Ordered moves — one commit each, tree green after each

Pure moves first:

1. **Split `pkg/composer/composer.go` into files** (§3.3), tests alongside. Same package.
2. **Split `internal/workflow/runner.go` into files** (§3.5): `runner.go`, `sharedcode.go`,
   `site.go`, `branch.go`. Same package.
3. **`internal` → `internal/config` + `internal/buildinfo`** (§3.1), including the `-X` path in
   `Makefile` and `Dockerfile`, and the fuzz/property tests of `configfile`.
4. **`pkg/repo` → `internal/repo`, `pkg/drupalorg` → `internal/drupalorg`.**
5. **`pkg/{composer,drush,phpcs,rector,drupal}` → `internal/tools/…`.** Re-home every
   `testdata/` with them.

Decoupling moves:

6. **Extract `internal/tools/phptool`** from `composer/exec.go` + `Env` (§3.2).
7. **Mirror `composer.Advisory`/`Source`/`AbandonedPackage` and `drush.UnsupportedModule` as
   report types** in `addon/report.go` (§3.7).
8. **Narrow `addon.Composer` into per-addon interfaces**; `make mock` (§3.6).
9. **Drop `workflow.GitRepository`/`workflow.Worktree` aliases**; `make mock` (§3.6).
10. **Extract `internal/app`** (addon registry, `mandatoryAddons`, service construction for
    `run` and `check --full`) (§3.5).

Clean-up and lock-in:

11. **`make deadcode`** sweep; delete anything the moves made unreachable.
12. **depguard layering rules** (and the `go list` test only if needed).
13. **Docs and tooling**: `CLAUDE.md` Code Map and invariants, `docs/explanation/{addon-architecture,
    how-a-run-works}.md`, `docs/contributing/{development,writing-an-addon}.md`,
    `.claude/rules/addons.md`, every page naming a moved path, the Makefile and
    `.github/workflows/*` comments naming `pkg/composer`/`pkg/drush`; delete this document.

`.mockery.yml` is recursive over the module and `mutago.yaml` names no paths, so neither needs
editing unless a move proves otherwise.

### Deliberately left alone

- `repo.Repository`/`Worktree` ownership, `codehosting.Platform`, `composer.ConfigGetter` (§3.6).
- `workflow`'s direct go-git use in `branch.go`/`commit.go`: `repo.Repository` is itself
  expressed in go-git types, so hiding them would need a new VCS abstraction with one
  implementation.
- Merging `internal/report` and `addon/report.go` (§3.7).
- Sub-packages of `composer` (§3.3).

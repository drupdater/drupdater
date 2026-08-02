# Development setup

## Prerequisites

- **Go 1.26.4+** — the version in `go.mod`.
- **`make`**.
- **Docker** — for `make mock`, the hadolint half of `make lint`, and building images.

```bash
git clone https://github.com/drupdater/drupdater.git
cd drupdater
make build
```

## Make targets

```bash
make build          # build the binary (injects the version via -ldflags)
make test           # go test -v ./...
make test-property  # only the property tests, with far more generated cases
make mutate         # mutation testing over the whole module (mutago)
make lint           # golangci-lint + hadolint on the Dockerfile
make fmt            # go fmt ./...
make fix            # go fix ./... (interface{} → any, strings.Cut, …)
make mock           # regenerate mocks (mockery v3, via Docker)
make deadcode       # find unreachable functions
make update         # go get -u ./... && go mod tidy
make docker-build   # build the multi-stage image locally
make docs-serve     # preview this documentation at :8000
make docs-build     # build the documentation with --strict
make help           # list every target
```

!!! note "Analytics only exists on the published site"

    The GoatCounter script in `overrides/main.html` is emitted only when
    `DRUPDATER_GOATCOUNTER` is set, which happens in `.github/workflows/docs.yml` and
    nowhere else. A local preview or build produces pages with no tracking script.

Run a single test:

```bash
go test -v -run TestName ./path/to/package/...
```

## Running it locally

Both of these use `--clone`, so they never touch a real checkout:

```bash
make run REPO=<repository-url> TOKEN=<token>           # go run
make docker-run REPO=<repository-url> TOKEN=<token>    # built image
```

Add flags with `OPTIONS`:

```bash
make run REPO=<repository-url> TOKEN=<token> OPTIONS="--dry-run --verbose"
```

For a run that touches nothing remote at all, use checkout mode against a local clone —
no token needed:

```bash
go run . --working-dir /path/to/a/drupal/checkout --dry-run --verbose
```

### Running outside the image

`make build`, `go run`, and a binary a CI job downloaded all invoke whatever `composer`,
`php` and `git` are on `PATH`, against whatever Composer configuration your environment
provides. That is supported: the two Composer settings the tool's own correctness depends
on (`COMPOSER_PROCESS_TIMEOUT`, `COMPOSER_NO_AUDIT`) are forced by `composer.Env` on every
invocation rather than inherited from the image — including the `composer exec` calls that
`pkg/drush`, `pkg/phpcs` and `pkg/rector` make. The image's remaining Composer variables
are deployment policy and need no local equivalent — see [environment
variables](../reference/environment-variables.md#set-by-drupdater).

What the image does give you is a known-good PHP with the required extensions and the
helper plugins installed globally. If a run fails locally in a way it does not in CI,
check that first.

## Enforced on commit

A pre-commit hook runs two gates. Read its output rather than assuming it passed.

### Lint

`make lint` must report zero issues. Do not disable a linter or relax `.golangci.yml` to
get past it — fix the code.

!!! note "If `golangci-lint` refuses to run"

    A `golangci-lint` built with an older Go than this module targets will refuse:

    ```text
    can't load config: the Go language version (go1.24) used to build golangci-lint
    is lower than the targeted Go version (1.26.4)
    ```

    Build the CI-pinned version with the module's toolchain, once per session:

    ```bash
    GOTOOLCHAIN=go1.26.4 go install \
      github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
    cp "$(go env GOPATH)/bin/golangci-lint" /usr/local/bin/golangci-lint
    ```

    Take the version from `.github/workflows/go.yml` and the toolchain from `go.mod` if
    they have drifted.

    `make lint` also runs hadolint via Docker, which needs a running daemon. If that
    cannot start, run `golangci-lint run ./...` directly — it is the part that gates code
    changes.

### Coverage

Changed packages must reach **≥ 90%** coverage. The hook prints per-package totals; add
tests before committing if any package is below.

## Mutation testing

Coverage proves a line *ran*. It does not prove a test *fails* when that line is wrong — a
test that exercises a function without asserting anything scores the same as one that checks
every branch. Mutation testing closes that gap: it rewrites the source in small ways (`==` →
`!=`, `&&` → `||`, dropping a `defer`, zeroing a return value) and reports every mutant the
suite fails to notice. A surviving mutant is a behaviour change no test objects to.

The tool is [mutago](https://github.com/quality-gates/mutago), pinned as a `tool` directive in
`go.mod` and configured in `mutago.yaml`. Run it over the whole module:

```bash
make mutate
```

That takes 25–35 minutes — the module produces roughly 3000 mutants and each one re-runs the
tests for its package. To iterate on one package, pass it directly:

```bash
RAPID_NOFAILFILE=1 go tool mutago --config mutago.yaml --coverage ./pkg/phpcs
```

(`RAPID_NOFAILFILE=1` keeps the property tests from littering `testdata/rapid` — see
[Property-based testing](#property-based-testing) below. `make mutate` sets it for you.)

Each survivor prints as `ESCAPED <file>:<line> (<mutator>)` with a diff of the change that
went unnoticed. Read it as a question: *which assertion would have caught this?*

### In CI

| Where | Scope | Blocking |
|---|---|---|
| `mutation` job in `go.yml` | Only the lines the pull request changes | **Yes** — fails below 75 % MSI |
| `mutation.yml` | The whole module, weekly and on demand | No |

Scoring the full module on every pull request would be slow and would fail on survivors
nobody touched, so the gate looks at changed lines only: new code has to be tested properly,
existing gaps are left to the weekly run. A pull request that changes no mutable code
generates no mutants and passes.

The module scored 67 % MSI when mutation testing was introduced, at 90 %+ line coverage
throughout — the tests ran the code without checking much of it. Every package was then
brought above the gate, so 75 % is the standard the codebase already meets rather than a
stretch target.

Note that a small diff makes the score coarse: with seven mutants, one extra survivor moves
the number by 14 points. A narrowly-failing pull request usually needs one more assertion,
not a rewrite.

### What the survivors kept turning out to be

The same few shapes accounted for most of them, and they are worth recognising before writing
new tests:

- **`mock.Anything` for a `context.Context`.** By far the biggest single cause. It lets a call
  site pass `nil` instead of propagating the context, silently disabling cancellation and the
  run timeout for everything below it. Match the context instead — exactly where the value is
  the one under test, or with a non-nil matcher where the workflow derives a child context.
- **Asserting absence rather than the result.** `assert.NotContains(secret)` passes whether the
  secret was redacted or the whole field was dropped. Assert what the output became.
- **`assert.Contains(err.Error(), …)` for a wrapped error.** Removing `%w` leaves the message
  unchanged, so only `ErrorIs`/`ErrorAs` catches a broken error chain.
- **Struct literals nobody reads back.** Request payloads, constructor fields and check
  results were built and never inspected, so any field could be dropped.
- **One malformed input for a multi-clause guard.** A check of three conditions needs one case
  per clause, or two thirds of it can be deleted unnoticed.
- **Error branches that need a failing filesystem.** An in-memory `afero.Fs` never fails, so
  write, close and flush error paths stay unreachable until a wrapper makes them fail on
  demand. `internal/report` and `pkg/drupal` both have one to copy.

The weekly run scores one package per matrix leg rather than the module in one go, so it
finishes in the time of its slowest package and a failed leg can be re-run on its own. A
final job sums the per-package counts into the module-wide score and writes both tables to
the job summary.

Each leg uploads a `mutation-report-<package>` artifact containing `mutago-agentic.json` —
every survivor with its diff, surrounding context and the test file that should have caught
it. That is the worklist for improving existing tests.

### When a mutant is a false positive

Some mutations are genuinely equivalent — logging text, a `cap` hint on a slice, a defensive
branch that cannot be reached. Suppress those at the source, never by lowering the threshold:

```go
// mutator-disable-func
func alwaysEquivalent() { … }

// mutator-disable-next-line statement/remove
buf := make([]byte, 0, 64)
```

Use the narrowest form that works — name the specific mutator rather than `*` — and add a
comment saying *why* the mutation is equivalent. If you cannot articulate the reason, the
mutant is probably telling you about a missing assertion.

## Property-based testing

Mutation testing asks whether a test would notice if the code changed. Property-based testing
asks the other half of the question: whether what the test asserts is true for every input, or
only for the one somebody typed. An example test says *`SanitizeURL` removes the password from
this URL*. A property says *`SanitizeURL` removes the password from **any** URL*, and then goes
looking for a URL where it does not.

The tool is [rapid](https://github.com/flyingmutant/rapid). It generates inputs, and when one
fails it shrinks the counterexample to the smallest input that still fails before reporting it.

```bash
make test                    # properties run with the rest of the suite, 100 cases each
make test-property           # only the properties, 10 000 cases each
go test ./internal/logging -run TestProperty -rapid.checks=1000000   # one package, hard
```

Every `-rapid.*` flag has a `RAPID_*` environment variable twin, and across `./...` the variable
is the one to use: rapid registers its flags only in test binaries that import it, so
`-rapid.checks` makes `go test ./...` fail on every package that has no property tests.
`make test-property` sets `RAPID_CHECKS` for that reason.

### The convention

- Properties live in `<subject>_property_test.go`, next to the ordinary `<subject>_test.go`.
- Every property is named `TestProperty…`. That prefix is not decoration: `make test-property`
  selects on it, so a property named anything else silently drops out of the deep run.
- Generators stay local to the file that uses them. They are type-specific, and a shared package
  of them would couple the test files together for no gain.
- **A property must not re-implement the function.** If the expected value is computed the same
  way the code computes it, the test passes by construction. State a law instead — idempotent,
  order-independent, leaks nothing, round-trips — or compare against a *rule* that is simpler
  than the implementation.

### Reproducing a failure

Every run uses a **fresh random seed**, so the suite explores new inputs each time and a
property that passes once is not proven. When one fails, rapid prints how to get it back and
writes a `.fail` file next to the test:

```text
To reproduce, specify -run="TestPropertyRedactCoversURLEncodedForms"
  -rapid.failfile="testdata/rapid/…/…-20260801210044-6470.fail" (or -rapid.seed=11040429362938902097)
```

Commit that file. rapid replays every `.fail` file it finds before generating anything new, so a
counterexample found once is checked forever after — the property-test equivalent of adding a
regression case. Delete it only when the property itself was wrong.

!!! warning "Run mutation testing with `RAPID_NOFAILFILE=1`"

    A mutation run rewrites the source thousands of times, and every mutant a property kills
    leaves a `.fail` file behind — hundreds of them, none of which says anything about the real
    code, all of which would then be replayed by the next ordinary test run. `make mutate` sets
    `RAPID_NOFAILFILE=1` for exactly that reason. Set it yourself when invoking `mutago`
    directly:

    ```bash
    RAPID_NOFAILFILE=1 go tool mutago --config mutago.yaml --coverage ./internal/logging
    ```

    If you find `testdata/rapid` full of files after a mutation run, delete them — the ones
    worth keeping came from a plain `go test`.

### Properties and the mutation gate

The mutation gate is blocking and the seed is random, which do not mix: a mutant killed only
because that run happened to generate the right input would pass on one pull request and fail on
the next. So when a property finds a real bug, fix the code **and** add an ordinary test naming
the concrete counterexample. The property is the net; the example test is what reliably kills
the mutant. Both fixes in `internal/logging` and `internal/addon` are written that way.

Keep properties cheap for the same reason — mutation testing re-runs the suite once per mutant,
so a property that takes a second at 100 cases costs an hour across the module.

### When not to write one

A function with two branches and no interesting input space is a table test, not a property.
`ActiveRunType` and `tokenRequired` are covered better by listing their cases than by generating
them. Properties earn their keep where the input space is large and the promise is absolute:
parsers, path handling, ordering, serialisation, and anything that must never leak a credential.

## Integration tests

The Go suite is hermetic: every subprocess and every HTTP call is faked. What a run does to a
real Drupal site is covered separately, by
[`.github/workflows/integration-test.yml`](https://github.com/drupdater/drupdater/blob/main/.github/workflows/integration-test.yml),
which builds the Docker image and runs Drupdater end to end against fixture repositories whose
`composer.lock` is deliberately frozen so every run has something to update.

It never runs on push — each leg installs Drupal for real. There are two ways in:

* **Add the `integration-test` label to a pull request.** Every fixture runs in each mode it
  declares, always as a dry run, and each leg reports as its own check. Remove and re-add the
  label to re-run.
* **Dispatch it from the Actions tab** to aim at a single repository and branch, pick the mode,
  or do a real (non-dry) run.

### The fixtures

Registered in [`.github/integration-fixtures.json`](https://github.com/drupdater/drupdater/blob/main/.github/integration-fixtures.json).
Adding one is appending an entry — the workflow needs no edit.

| Fixture | Covers |
|---|---|
| `drupal-cms` | The heavy end of the addon catalogue: recipes, patches, PHPCBF, Rector, normalisation, against a single site |
| `drupal-multisite` | Two sites over one codebase, so the per-site phases run concurrently against separate configuration trees |
| `multisite-patches` | A patch that stops applying when its package moves, so `composer_patches` has to report a conflict |
| `multisite-shallow` | Preflight rejecting a `--depth 1` checkout |
| `multisite-unsatisfiable` | A root constraint nothing can satisfy, so `composer update` fails |
| `multisite-missing-site` | A configured site with no directory, so one of the concurrent baseline installs fails |

The last four exist because assertions that only ever see a successful run cannot catch a
regression that makes Drupdater fail in the wrong place, or report a failure as a success.

An entry may narrow `modes` (default: both `normal` and `security`) and set `shallow`. A fixture
whose `expect.status` contains `failed` is a **failure fixture**: Drupdater is expected to exit
non-zero, and the leg fails if it exits `0` instead.

### What each leg asserts

The run report is the verdict, not the exit status — Drupdater exits `0` on an abort, and a run
reports `success` even when an addon logged and swallowed its own failure. On top of
`assert-report.jq` (see [Consume the run report](../how-to/consume-the-run-report.md)), a
successful leg also checks the repository the run left behind: that HEAD is the branch the report
names, that it descends from the base branch, that nothing is left uncommitted, that
`composer.lock` agrees with the report's package list, and that running Drupdater a second time
finds nothing left to do.

## Mocks

Mocks are generated by [mockery v3](https://vektra.github.io/mockery/), configured in
`.mockery.yml` to cover the whole module recursively.

After changing any interface:

```bash
make mock
```

mockery's `testify` template emits one consolidated `mocks_test.go` per package, in the
package it mocks. Do not edit those files by hand.

## Code layout

| Path | Contains |
|---|---|
| `main.go` → `cmd/` | Cobra commands, flag parsing, service construction, the addon registry |
| `internal/services/` | The workflow, its phases, events, and preflight checks |
| `internal/addon/` | The addons, their report data, and their description templates |
| `internal/codehosting/` | The GitHub and GitLab implementations and the provider factory |
| `internal/report/` | The published JSON report schema and its writer |
| `internal/logging/` | The redactor |
| `pkg/` | Thin wrappers around external tools |
| `scripts/` | PHP helper scripts copied into the Docker image |

`pkg/` wraps one external tool per directory: `composer`, `drush`, `repo` (go-git),
`phpcs`, `rector`, `drupal` (site installer) and `drupalorg` (Drupal.org HTTP client).

For how these fit together, read [Explanation](../explanation/index.md) — in particular
[How a run works](../explanation/how-a-run-works.md) and [The addon
architecture](../explanation/addon-architecture.md).

## Description templates

Templates in `internal/addon/templates/` render into the pull request description. Keep
them consistent with the existing style:

- Section headers: `## {emoji} **{Title}**`
- Table cells: always use the `{{ cell }}` helper, which escapes pipe characters and
  newlines
- Wrap long sections in `<details>` / `<summary>`, as the existing templates do
- **Always verify the output with `--dry-run` before shipping a new template**

Each template has a golden file in `internal/addon/testdata/`. Those files are embedded
directly into this documentation, so updating one updates the published example too.

## Docker image

Multi-stage: a Go build stage, then the PHP runtime image the binary is copied into.

```bash
make docker-build
docker build --build-arg PHP_VERSION=8.4 -t drupdater-local:php8.4 .
```

`PHP_VERSION` defaults to `8.3`. Released images are built for 8.2, 8.3, 8.4 and 8.5.

## Devcontainer

`.devcontainer/devcontainer.json` builds the repository's own Dockerfile with PHP 8.3 and
adds the Go toolchain and Docker-in-Docker, so the full toolchain including `make mock`
works inside it.

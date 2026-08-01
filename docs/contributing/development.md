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
go tool mutago --config mutago.yaml --coverage ./pkg/phpcs
```

Each survivor prints as `ESCAPED <file>:<line> (<mutator>)` with a diff of the change that
went unnoticed. Read it as a question: *which assertion would have caught this?*

### In CI

| Where | Scope | Blocking |
|---|---|---|
| `mutation` job in `go.yml` | Only the lines the pull request changes | **Yes** — fails below 70 % MSI |
| `mutation.yml` | The whole module, weekly and on demand | No |

Scoring the full module on every pull request would be slow and would fail on survivors
nobody touched, so the gate looks at changed lines only: new code has to be tested properly,
existing gaps are left to the weekly run. A pull request that changes no mutable code
generates no mutants and passes.

For calibration, packages measured when the gate was introduced scored 68–74 % MSI — all of
them at 90 %+ line coverage:

| Package | MSI | Covered-code MSI |
|---|---|---|
| `pkg/phpcs` | 73.1 % | 73.1 % |
| `internal/report` | 73.6 % | 81.8 % |
| `pkg/composer` | 72.9 % | 77.6 % |
| `internal/logging` | 67.7 % | 73.0 % |

So 70 % is roughly today's standard rather than a stretch target. Note that a small diff
makes the score coarse — with seven mutants, one extra survivor moves the number by 14
points — so a narrowly-failing pull request usually needs one more assertion, not a rewrite.

The weekly run uploads a `mutation-report` artifact containing `mutago-agentic.json` — every
survivor with its diff, surrounding context and the test file that should have caught it.
That is the worklist for improving existing tests.

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

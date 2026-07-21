---
paths:
  - "**/*.go"
  - "Dockerfile"
  - ".golangci.yml"
---

`make lint` runs automatically on `git commit` (pre-commit hook) — read its output before committing. Work is only complete when it reports zero issues.

## Environment setup (Claude Code web / remote sessions)

The pre-installed `golangci-lint` is built with an older Go than this module
targets, so it refuses to run:

```
can't load config: the Go language version (go1.24) used to build golangci-lint
is lower than the targeted Go version (1.26.4)
```

Why: the base `go` on `PATH` is older (e.g. go1.24.7); `GOTOOLCHAIN=auto` only
upgrades to the module's Go (see the `go` directive in `go.mod`) when a build
needs it. `golangci-lint`'s own module has a low `go` directive, so it gets
built with the base toolchain and then rejects this repo.

Fix — build the CI-pinned `golangci-lint` with the module's toolchain, once per
session:

```bash
# Version must match .github/workflows/go.yml (golangci-lint-action `version:`).
# Toolchain must match go.mod's `go` directive.
GOTOOLCHAIN=go1.26.4 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
cp "$(go env GOPATH)/bin/golangci-lint" /usr/local/bin/golangci-lint  # so `make lint` picks it up

go version "$(go env GOPATH)/bin/golangci-lint"   # confirm: "... go1.26.4"
make lint
```

If the versions above have drifted, read the current values from
`.github/workflows/go.yml` and `go.mod` and substitute them.

Notes:
- `make lint` also runs **hadolint** on the `Dockerfile` via
  `docker run hadolint/hadolint`. That needs the Docker daemon running and the
  image pullable; both may be unavailable in a sandbox (no daemon, or the
  registry pull is blocked). The Go lint (`golangci-lint run ./...`) is the part
  that gates code changes — run it directly if the hadolint step can't start.
- Don't disable linters or relax `.golangci.yml` to make lint pass; fix the code.

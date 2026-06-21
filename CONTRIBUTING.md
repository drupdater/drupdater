# Contributing to Drupdater

Thanks for your interest in improving Drupdater. This guide covers how to get a
change merged.

## Before you start

- For anything beyond a small fix, **open an issue first** to discuss the
  approach. It avoids wasted work on a PR that doesn't fit the direction.
- Check existing [issues](https://github.com/drupdater/drupdater/issues) and
  [pull requests](https://github.com/drupdater/drupdater/pulls) to avoid
  duplication.

## Development setup

Requires **Go 1.26+** and `make`.

```bash
git clone https://github.com/drupdater/drupdater.git
cd drupdater
make build
```

Common tasks:

```bash
make test    # run all tests
make lint    # vet + staticcheck + golangci-lint
make fmt     # format code
make mock    # regenerate mocks after changing an interface (mockery v3)
```

Run a single test:

```bash
go test -v -run TestName ./path/to/package/...
```

## Project layout

See [`CLAUDE.md`](CLAUDE.md) for the architecture overview — workflow phases, the
addon system, the VCS provider abstraction, and the `pkg/` CLI wrappers.

## Submitting a pull request

1. Branch off `main`.
2. Keep the PR focused on a single concern.
3. Add or update tests for behavior changes.
4. If you changed an interface, run `make mock`.
5. Run `make lint test` and make sure both pass.
6. Write a clear PR description explaining the *why*, not just the *what*.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/) where it fits
(`feat:`, `fix:`, `refactor:`, `docs:`, `chore:`) — it keeps the history readable
and matches the existing log.

## Reporting bugs

Open an issue with: what you ran, what you expected, what happened, and the
relevant log output (run with `--verbose` for detail). Redact any tokens.

## Security issues

Do **not** open a public issue for vulnerabilities. See [`SECURITY.md`](SECURITY.md).

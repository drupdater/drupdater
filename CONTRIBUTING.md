# Contributing to Drupdater

Thanks for your interest in improving Drupdater.

**The full contributor guide is at <https://drupdater.github.io/contributing/>.** It
covers development setup, the `make` targets, the lint and coverage gates, writing an
addon, and releasing.

## Before you start

- For anything beyond a small fix, **open an issue first** to discuss the approach. It
  avoids wasted work on a PR that doesn't fit the direction.
- Check existing [issues](https://github.com/drupdater/drupdater/issues) and
  [pull requests](https://github.com/drupdater/drupdater/pulls) to avoid duplication.

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
make mutate  # mutation testing (mutago) — checks the tests actually assert
make lint    # vet + staticcheck + golangci-lint
make fmt     # format code
make mock    # regenerate mocks after changing an interface (mockery v3)
make help    # list every target
```

Run a single test:

```bash
go test -v -run TestName ./path/to/package/...
```

See [Development setup](https://drupdater.github.io/contributing/development/) for the
full list, including what to do when `golangci-lint` refuses to run.

## Project layout

See [Explanation](https://drupdater.github.io/explanation/) for the architecture overview —
workflow phases, the addon system, the VCS provider abstraction, and the configuration
model.

## Submitting a pull request

1. Branch off `main`.
2. Keep the PR focused on a single concern.
3. Add or update tests for behavior changes — changed packages need ≥ 90% coverage. CI also
   runs mutation testing on the lines you changed; those tests need to actually *assert*, not
   just execute the code. See [Mutation
   testing](https://drupdater.github.io/contributing/development/#mutation-testing).
4. If you changed an interface, run `make mock`.
5. Run `make lint test` and make sure both pass.
6. Write a clear PR description explaining the *why*, not just the *what*.

## Documentation

Documentation sources live in [`docs/`](docs/) and are published to
<https://drupdater.github.io/> on every push to `main`. A change that alters a flag, a
config key, an addon, or the report schema should update its documentation in the same PR.

```bash
make docs-serve    # preview at http://127.0.0.1:8000
make docs-build    # build with --strict
```

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/) where it fits
(`feat:`, `fix:`, `refactor:`, `docs:`, `chore:`) — it keeps the history readable
and matches the existing log.

## Reporting bugs

Open an issue with: what you ran, what you expected, what happened, and the
relevant log output (run with `--verbose` for detail). Redact any tokens.

## Security issues

Do **not** open a public issue for vulnerabilities. See [`SECURITY.md`](SECURITY.md).

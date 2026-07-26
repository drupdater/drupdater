# Contributing

Thanks for your interest in improving Drupdater. This section covers how to get a change
merged.

## Before you start

- For anything beyond a small fix, **open an issue first** to discuss the approach. It
  avoids wasted work on a pull request that does not fit the direction.
- Check existing [issues](https://github.com/drupdater/drupdater/issues) and [pull
  requests](https://github.com/drupdater/drupdater/pulls) to avoid duplication.

## Where to go next

- [Development setup](development.md) — prerequisites, `make` targets, running a single
  test, regenerating mocks, and the lint and coverage rules enforced on commit.
- [Writing an addon](writing-an-addon.md) — the four steps, plus what to wire up in the
  report and the pull request description.
- [Releasing](releasing.md) — how a version tag and its Docker images are produced.
- [Security policy](security-policy.md) — reporting a vulnerability.

For the architecture — workflow phases, the addon system, the VCS provider abstraction —
read [Explanation](../explanation/index.md).

## Submitting a pull request

1. Branch off `main`.
2. Keep the pull request focused on a single concern.
3. Add or update tests for behaviour changes.
4. If you changed an interface, run `make mock`.
5. Run `make lint test` and make sure both pass.
6. Write a clear description explaining the *why*, not just the *what*.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/) where it fits (`feat:`,
`fix:`, `refactor:`, `docs:`, `chore:`) — it keeps the history readable and matches the
existing log.

## Reporting bugs

Open an issue with: what you ran, what you expected, what happened, and the relevant log
output (run with `--verbose` for detail). Redact any tokens.

## Security issues

Do **not** open a public issue for vulnerabilities. See the [security
policy](security-policy.md).

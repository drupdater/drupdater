# Command line

Drupdater is a single binary with three commands.

| Command | Purpose |
|---|---|
| [`drupdater [token]`](drupdater.md) | Run an update and open a pull or merge request |
| [`drupdater check [token]`](check.md) | Validate prerequisites without running an update |
| [`drupdater addons`](addons.md) | List the addon names valid in `.drupdater.yaml` |

Inside the Docker image the binary is at `/opt/drupdater/bin` and is the image's
`ENTRYPOINT`. See [Docker images](../docker-images.md) for how that affects GitLab CI.

## The access token

The token is a positional argument, and the same for every command that takes one:

```bash
drupdater <token> [flags]
```

If the argument is omitted, the token is read from the **`DRUPDATER_TOKEN`** environment
variable instead. Prefer the environment variable: it keeps the token out of the process
list and the shell history.

### When a token is required

A token is required whenever the run will actually use one — that is, whenever it clones
or publishes:

| Mode | Token required? |
|---|---|
| Checkout mode, real run | Yes |
| Checkout mode, `--dry-run` | **No** — no VCS client is constructed at all |
| `--clone`, real run | Yes |
| `--clone`, `--dry-run` | Yes — cloning may itself require authentication |
| `drupdater check` | Never — a token only sharpens one check |

Without a token when one is required, the run fails immediately with:

```text
no token provided: pass it as the argument or set DRUPDATER_TOKEN
```

For what the token needs permission to do, see [Credentials and
redaction](../../explanation/credentials-and-redaction.md).

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The run succeeded — **or** it stopped early with nothing to do |
| `1` | The run failed, or a `check` reported at least one failing check |

The "nothing to do" cases exit `0` deliberately. When `composer update` produces no
changes, or the update branch already exists, or a `--security` run finds no advisories,
Drupdater raises an internal abort signal that is logged as a warning rather than an
error. A scheduled pipeline should not turn red because a site was already up to date.

In the [run report](../run-report.md) those cases are distinguishable: `status` is
`no_changes` rather than `success`.

## Version information

There is currently **no `--version` flag and no `version` subcommand**. The build version
is only reported in the `drupdater_version` field of the [run report](../run-report.md).

!!! warning "Published images report `dev`"

    The version string is injected at build time via `-ldflags`, which the `Makefile`
    does but the `Dockerfile` does not. Binaries inside the published
    `ghcr.io/drupdater/drupdater-php*` images therefore report `drupdater_version:
    "dev"` rather than the release tag. To know which version you are running, rely on
    the image tag you pinned rather than the report field.

## Global behaviour

- All flags on the root command are **persistent**, so `check` accepts every one of them
  too.
- `--verbose` raises logging to debug level. On an update it also logs the resolved
  configuration; `check` reports the configuration through its own results instead.
- Log output is structured and passes through a redactor, so registered secrets never
  appear — see [Credentials and
  redaction](../../explanation/credentials-and-redaction.md).
- `SIGINT` and `SIGTERM` cancel the run's context so cleanup still runs. Interrupting a
  run does not leave site databases or a scratch clone behind.

# Reference

Precise, information-oriented descriptions of Drupdater's surfaces. These pages describe
*what things are*; for *how to achieve something*, see the [how-to
guides](../how-to/index.md), and for *why it works this way*, see
[explanation](../explanation/index.md).

## Invoking Drupdater

| Page | Covers |
|---|---|
| [Command line](cli/index.md) | Token resolution, exit codes, the command list |
| [`drupdater`](cli/drupdater.md) | The root update command and all ten persistent flags |
| [`drupdater check`](cli/check.md) | Preflight validation, `--full` |
| [`drupdater addons`](cli/addons.md) | Listing valid addon names |
| [Docker images](docker-images.md) | Published tags, PHP variants, entrypoint, baked-in environment |

## Configuring Drupdater

| Page | Covers |
|---|---|
| [Configuration file](configuration.md) | The complete `.drupdater.yaml` schema, defaults and validation |
| [Environment variables](environment-variables.md) | Every variable Drupdater reads, and the one it sets |
| [Addons](addons/index.md) | All ten addons — events, behaviour, report output, pull request sections |

## Output

| Page | Covers |
|---|---|
| [Run report](run-report.md) | The `--report` JSON schema, version 1 |
| [Preflight checks](preflight-checks.md) | Every check `drupdater check` runs, and what a failure means |

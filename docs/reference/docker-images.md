# Docker images

Drupdater ships as a Docker image containing the Go binary plus the PHP runtime,
Composer, Drush's dependencies and the helper scripts it needs. There is no other
supported distribution.

## Published images

```text
ghcr.io/drupdater/drupdater-php8.2
ghcr.io/drupdater/drupdater-php8.3
ghcr.io/drupdater/drupdater-php8.4
ghcr.io/drupdater/drupdater-php8.5
```

**Pick the variant matching your project's PHP version.** A mismatch is caught by the
`PHP platform requirements satisfied` [preflight check](preflight-checks.md) rather than
failing obscurely later.

Images are built for `linux/amd64` and `linux/arm64`, with build provenance attestation
pushed to the registry.

## Tags

Each variant is tagged on release:

| Tag form | Example | Moves? |
|---|---|---|
| `<major>.<minor>.<patch>` | `0.3.6` | No |
| `v<major>.<minor>.<patch>` | `v0.3.6` | No |
| `<major>.<minor>` | `0.3` | Yes, within the minor |
| `<major>` | `0` | Yes, within the major |
| `latest` | | Yes, always |

!!! warning "Pin in production"

    Drupdater is pre-1.0. The CLI surface and config format may change between minor
    versions, so `latest` and the floating `0` tag can change behaviour under you.

    The examples throughout this documentation use `latest` for brevity. In a scheduled
    pipeline, replace it with the full version from the [latest
    release](https://github.com/drupdater/drupdater/releases) and bump it deliberately.

## Entrypoint

The binary lives at **`/opt/drupdater/bin`** and is the image's `ENTRYPOINT`.

This matters in GitLab CI, where the image is used as a job container and its entrypoint
must be cleared before `script:` can run:

```yaml
image:
  name: ghcr.io/drupdater/drupdater-php8.3:latest
  entrypoint: [""]
script:
  - /opt/drupdater/bin "$DRUPDATER_TOKEN"
```

In GitHub Actions the container is used the same way, so the full path is needed there
too:

```yaml
container:
  image: ghcr.io/drupdater/drupdater-php8.3:latest
steps:
  - run: /opt/drupdater/bin "${{ secrets.DRUPDATER_TOKEN }}"
```

Run directly with `docker run`, the entrypoint applies and you pass only arguments:

```bash
docker run ghcr.io/drupdater/drupdater-php8.3:latest <token> --dry-run
```

## What is in the image

Built on `php:<version>-cli-trixie`, with:

- **PHP extensions** — `pdo_mysql`, `gd`, `zip`, `imagick`, `intl`. `memory_limit` is
  unlimited.
- **System packages** — `git`, `unzip`, `patch`, `sqlite3`. SQLite is what the baseline
  site installs run on.
- **Composer**, pinned to a **minor** (`composer:2.10`) rather than to a floating `2`.
  Composer is the largest single influence on what a run does, and its defaults change
  between minors — `config.audit` giving way to `config.policy`, say — so a minor bump is a
  deliberate commit that CI runs against rather than something that arrives in a published
  image unannounced. Patch releases, where fixes live, still flow in. Every report names the
  [Composer version](run-report.md#composer_version-and-php_version) that produced it.
- Plus two globally-required Composer plugins:
  [`mglaman/composer-drupal-lenient`](https://github.com/mglaman/composer-drupal-lenient)
  and [`ion-bazan/composer-diff`](https://github.com/IonBazan/composer-diff), both
  pre-allow-listed.
- **Helper scripts** at `/opt/drupdater/` — the Rector configuration, the unsupported
  modules query, and the configuration resave script.

The Composer environment is tuned for unattended runs; see [environment
variables](environment-variables.md#set-inside-the-docker-image) for the values and why.

The image build takes a `VERSION` build argument, which the release workflow sets to the
tag being built, so the [run report](run-report.md)'s `drupdater_version` field names the
release the image came from.

## Volumes and the working directory

Drupdater writes each site's SQLite database and private files **beside** the working
directory, not inside it:

```text
<working-dir>/../<site>.sqlite
<working-dir>/../private/<site>/
```

So the checkout's parent must be a real, writable directory inside the container. Mount
a parent directory and point `--working-dir` at the checkout within it:

```bash
docker run -v "$(pwd)":/workspace/project -w /workspace \
  ghcr.io/drupdater/drupdater-php8.3:latest \
  "$DRUPDATER_TOKEN" --working-dir /workspace/project
```

Mounting the checkout directly at `/` or at a mount root leaves nowhere to write those
files.

## Building locally

```bash
make docker-build                                  # tags drupdater-local:latest
docker build --build-arg PHP_VERSION=8.4 -t drupdater-local:php8.4 .
make docker-run REPO=<git-url> TOKEN=<token>       # clone mode, for testing
```

The `Dockerfile` is multi-stage: a Go build stage, then the PHP runtime image the binary
is copied into.

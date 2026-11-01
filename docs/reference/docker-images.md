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
- **The Drupdater agent** at `/opt/drupdater/agent/`, a third global plugin — see below.
- **Bundled tools** at `/opt/drupdater/tools/` — see below.

### Drupdater agent

`drupdater/agent`, built from `agent/` in the Drupdater repository, is a Composer plugin
installed globally. It adds hidden `composer drupdater:*` commands that answer Drupdater's
questions about a project from inside that project's own Composer — its repositories, its
credentials, its configuration — without installing anything into it.

| Command | Used by |
|---|---|
| `drupdater:handshake` | The [`drupdater agent installed in Composer`](preflight-checks.md#drupdater-agent-installed-in-composer) preflight check |
| `drupdater:patch-check` | [`composer_patches`](addons/composer-patches.md#how-a-patch-is-tested), to test whether patches still apply |

Each command reads one JSON request on stdin and prints one JSON reply on stdout, versioned
by a `protocol` number the binary and the agent must agree on. The agent ships in the same
image as the binary, so they always do; the preflight check catches a binary run anywhere
else.

### Bundled tools

[`code_beautifier`](addons/code-beautifier.md) and
[`deprecations_remover`](addons/deprecations-remover.md) need PHPCS with `drupal/coder` and
Drupal Rector. Rather than installing them into the project for each run, the image installs
them once into `/opt/drupdater/tools/`, from the repository's `tools/composer.lock`, and runs
them against the project from there. A project that already depends on `drupal/coder` or
`palantirnet/drupal-rector` runs its own copy instead.

`rector/rector` stays below 2.6.2, which removed rule sets Drupal Rector still relies on;
Drupal Rector's own `conflict` holds it there.

`mglaman/phpstan-drupal` is installed beside Drupal Rector so that Rector knows the type of
a `\Drupal::service()` call; see [`deprecations_remover`](addons/deprecations-remover.md#service-calls).

The Composer environment is tuned for unattended runs; see [environment
variables](environment-variables.md#set-inside-the-docker-image) for the values and why.

The image build takes a `VERSION` build argument, which the release workflow sets to the
tag being built, so the [run report](run-report.md)'s `drupdater_version` field names the
release the image came from.

## Volumes and the working directory

Drupdater writes each site's SQLite database and private files to a directory of its own
under the container's temp dir (`$TMPDIR`, else `/tmp`), removed when the run ends. Only
the checkout itself needs to be mounted:

```bash
docker run -v "$(pwd)":/workspace -w /workspace \
  ghcr.io/drupdater/drupdater-php8.3:latest \
  "$DRUPDATER_TOKEN" --working-dir /workspace
```

## Building locally

```bash
make docker-build                                  # tags drupdater-local:latest
docker build --build-arg PHP_VERSION=8.4 -t drupdater-local:php8.4 .
make docker-run REPO=<git-url> TOKEN=<token>       # clone mode, for testing
```

The `Dockerfile` is multi-stage: a Go build stage, then the PHP runtime image the binary
is copied into.

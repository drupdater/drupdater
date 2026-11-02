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
- Plus a globally-required Composer plugin,
  [`mglaman/composer-drupal-lenient`](https://github.com/mglaman/composer-drupal-lenient),
  pre-allow-listed.
- **Helper scripts**, embedded in the binary and extracted per run — drupdater's Drush commands:
  the site facts query and the configuration resave.
- **The Drupdater agent** at `/opt/drupdater/agent/`, a second global plugin, which holds every
  addon — see below. It brings
  [`ion-bazan/composer-diff`](https://github.com/IonBazan/composer-diff) with it as a library;
  that package's own plugin, and with it the `composer diff` command, is disabled.
- **Bundled tools** at `/opt/drupdater/tools/` — see below.

### Drupdater agent

`drupdater/agent`, built from `agent/` in the Drupdater repository, is a Composer plugin
installed globally. It holds [every addon](../explanation/addon-architecture.md#where-an-addon-runs),
and adds hidden `composer drupdater:*` commands through which Drupdater runs them and asks a
project's own Composer — its repositories, its credentials, its configuration — without
installing anything into it.

| Command | Used by |
|---|---|
| `drupdater:addons` | Reading `.drupdater.yaml`, the `addons` command and the `addon names resolve` check: the addon catalogue |
| `drupdater:hook` | Every addon hook of every run, one call per hook |
| `drupdater:report` | The [run report](run-report.md#addons)'s addon sections, and so the merge request description |
| `drupdater:handshake` | The [`drupdater agent installed in Composer`](preflight-checks.md#drupdater-agent-installed-in-composer) preflight check |
| `drupdater:platform-check` | The [`PHP platform requirements satisfied`](preflight-checks.md#php-platform-requirements-satisfied) preflight check |
| `drupdater:project` | Staging the scaffold files an update rewrote, naming the update branch, and the site checks |
| `drupdater:resolve` | The run's [`resolve update`](../explanation/how-a-run-works.md) phase |

`drupdater:addons` lists the catalogue: each addon, whether every run needs it, whether a
normal run gets it when the project names none, and the stage it hooks each event at. It needs
no project, so drupdater asks it from an empty directory.

`drupdater:hook` runs one addon's hook for one workflow event. The request names the addon and
carries the event — its name, plus what that event hands its hooks: the update plan on
`pre-composer-update`, the committed `composer.lock` on `post-composer-update`, the site and
its facts on the site events — with what the addon's own hooks found earlier in the run, and
whether the run is a security run. The reply is what the hook returned: its finding, the
commits it asks for, its proposal for the update, or the reason the run has nothing to do. The
agent commits nothing itself; drupdater makes the commits before it calls the next hook.

`drupdater:report` builds the addons' sections of the run report from what their hooks found.
A section depends on its findings alone, so this too needs no project, and the final report
can ask for it after the working copy is gone.

`drupdater:resolve` runs `composer update --no-install --with-all-dependencies
--ignore-platform-reqs --bump-after-update` with the run's package restriction, pins and
`--minimal-changes`, and reports each package that changed in the lock: upgraded, downgraded,
removed or installed, with its versions as the lock prints them (`1.x-dev 0424dff` for a
branch). A new commit of a branch counts as an upgrade, as in Composer's own output; a package
that only moved between `require` and `require-dev` did not change. When the update cannot be
resolved, Composer's own explanation is carried into the run's error.
[`composer_patches`](addons/composer-patches.md) runs the same update inside its own hook as a
dry run, with `--no-scripts`, and puts `composer.json` and `composer.lock` back afterwards.

`drupdater:platform-check` reports the platform requirements — of the locked packages and of
the root package, `require-dev` included — that the running PHP does not meet, deciding each
the way `composer check-platform-reqs --lock` does: a requirement is met when one candidate, the
platform package itself or a package that provides or replaces it, satisfies every constraint
on it. The project's `config.platform` is ignored, so the answer is about the real runtime.

`drupdater:project` reports the web root — `extra.drupal-scaffold.locations.web-root`, without
a trailing slash — and the lock's `content-hash`, which names the
[update branch](cli/drupdater.md). It is asked again whenever `composer.json` or the lock may
have changed, so a reader never sees a stale answer.

Each command reads one JSON request on stdin and prints one JSON reply on stdout, versioned
by a `protocol` number the binary and the agent must agree on, with what the command logged.
The agent ships in the same image as the binary, so they always do; the preflight check catches
a binary run anywhere else.

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

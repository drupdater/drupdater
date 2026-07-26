# Environment variables

Drupdater reads seven environment variables and sets one for its subprocesses. None of
them are bound to CLI flags — each is read directly where it is used.

## Read by Drupdater

### `DRUPDATER_TOKEN`

The VCS access token, used when no positional argument is given. Preferred over the
argument because it keeps the token out of the process list and the shell history.

Applies to both `drupdater` and `drupdater check`. See [when a token is
required](cli/index.md#when-a-token-is-required).

```bash
export DRUPDATER_TOKEN="glpat-..."
drupdater
```

### `DRUPALCODE_ACCESS_TOKEN`

A [Drupal.org GitLab](https://git.drupalcode.org) personal access token, used by the
[`composer_patches`](addons/composer-patches.md) addon to query issue metadata and
download updated patch files.

**Without it, patch management is substantially degraded.** Drupdater can still detect
patches for packages that are no longer installed, but it cannot tell whether an issue
was fixed upstream and cannot fetch a newer patch. A patch that stops applying then
results in the package being pinned to its old version rather than repaired.

See [Enable patch management](../how-to/enable-patch-management.md).

### `COMPOSER_AUTH`

Composer authentication JSON for private registries. Drupdater does not consume it; it is
passed through to the Composer subprocess. See the [Composer
documentation](https://getcomposer.org/doc/03-cli.md#composer-auth) for the format.

```bash
export COMPOSER_AUTH='{"http-basic":{"repo.packagist.com":{"username":"token","password":"<secret>"}}}'
```

Every string value inside the parsed JSON is registered with the log redactor
individually, **except** values keyed `username`. Composer's documented form for package
registries commonly sets the username to the literal word `token`, which is not a secret
and must not be redacted from unrelated output. If the value does not parse as JSON, the
raw string is registered as a fallback.

See [Use a private Composer registry](../how-to/use-private-packagist.md).

### `GITHUB_ACTIONS`

When set to `true`, forces the GitHub provider regardless of the repository hostname.
GitHub Actions sets this automatically. It is authoritative over hostname sniffing, which
is what makes an enterprise host with a non-`github` domain resolve correctly.

### `GITLAB_CI`

When set to `true`, forces the GitLab provider regardless of hostname. GitLab CI sets
this automatically. This is what makes a self-hosted GitLab whose hostname does not
contain `gitlab` resolve correctly.

See [VCS provider detection](../explanation/vcs-provider-detection.md).

### `GITHUB_REF_NAME` and `CI_COMMIT_REF_NAME`

The target branch, used in checkout mode when the checkout is in detached HEAD — the
normal state of a CI checkout. `GITHUB_REF_NAME` is consulted first, then
`CI_COMMIT_REF_NAME`; both are set automatically by their respective platforms.

If the checkout is detached and neither is set, the run fails:

```text
could not determine the target branch: the checkout is in detached HEAD and no CI
branch variable (GITHUB_REF_NAME, CI_COMMIT_REF_NAME) is set
```

Neither variable is consulted in `--clone` mode, where `--branch` applies instead.

## Set by Drupdater

### `SITE_NAME`

Set on **every** Drush invocation, to the name of the site currently being processed.

This is the hook a multi-site project uses to resolve which site Drush is operating on.
Your `web/sites/sites.php` reads it to map the current request onto a site directory. See
[Update multiple sites](../how-to/update-multiple-sites.md) for the snippet.

## Set inside the Docker image

The published images bake in a Composer environment tuned for unattended runs. These are
image defaults, not something Drupdater reads:

| Variable | Value | Why |
|---|---|---|
| `COMPOSER_HOME` | `/usr/local/composer` | Shared global install of the helper plugins |
| `COMPOSER_CACHE_DIR` | `/tmp/composer/cache` | Writable regardless of the user the container runs as |
| `COMPOSER_ALLOW_SUPERUSER` | `1` | Containers commonly run as root |
| `COMPOSER_NO_AUDIT` | `1` | Auditing is the [`composer_audit`](addons/composer-audit.md) addon's job, on its own schedule |
| `COMPOSER_FUND` | `0` | Suppresses funding notices in log output |
| `COMPOSER_PROCESS_TIMEOUT` | `0` | A large `composer update` can legitimately exceed the default 300s |

See [Docker images](docker-images.md).

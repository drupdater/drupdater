# Enable patch management

The [`composer_patches`](../reference/addons/composer-patches.md) addon always runs, but
its most useful half — reading Drupal.org issue status and downloading replacement
patches — needs a Drupal.org access token.

Without it, a patch that stops applying results in the package being **pinned to its old
version**. With it, Drupdater can usually drop the patch (because the issue was fixed
upstream) or replace it with a newer one.

## 1. Create a Drupal.org token

Drupal.org's code hosting runs on GitLab at
[git.drupalcode.org](https://git.drupalcode.org).

1. Sign in with your Drupal.org account.
2. Go to **Preferences → Access tokens**.
3. Create a token with the **`read_api`** scope. Drupdater only reads issue metadata and
   downloads diffs; it never writes to Drupal.org.
4. Copy the value — it is shown once.

## 2. Store it as a secret

=== "GitHub Actions"

    Add a repository secret named `DRUPALCODE_ACCESS_TOKEN`, then:

    ```yaml
      - run: /opt/drupdater/bin
        env:
          DRUPDATER_TOKEN: ${{ secrets.DRUPDATER_TOKEN }}
          DRUPALCODE_ACCESS_TOKEN: ${{ secrets.DRUPALCODE_ACCESS_TOKEN }}
    ```

=== "GitLab CI"

    Add a **masked, protected** CI/CD variable named `DRUPALCODE_ACCESS_TOKEN`. It is then
    already in the job environment, and nothing further is needed.

=== "Local docker run"

    ```bash
    docker run -e DRUPALCODE_ACCESS_TOKEN \
      ghcr.io/drupdater/drupdater-php8.3:v0.12.0 \
      <token> --clone --repository-url <repository-url>
    ```

The token is registered with the log redactor as soon as it is read, so it never appears
in log output or in the [run report](../reference/run-report.md).

## What changes

| | Without the token | With the token |
|---|---|---|
| Patch for an uninstalled or removed package | Removed | Removed |
| Patch already provided by a dependency | Removed | Removed |
| Issue fixed upstream | **Kept** — status unknown | **Removed**, with the reason recorded |
| Patch no longer applies | **Package pinned** to its old version | Newer patch fetched and used; pinned only if none exists |
| Patch descriptions | Left as written | Rewritten to `Issue #12345: [Title](url)` |

Replacement patches are stored in the repository under:

```text
patches/<project>/<issue-id>-<sha>-<title>.diff
```

They are committed as part of the update, so the project keeps working even if the fork
branch they came from later disappears.

## Verify it

Run with `--dry-run --report` and look at the `composer_patches` section:

```bash
drupdater --dry-run --report ./report.json
jq '.addons.composer_patches' report.json
```

```json
{
  "removed": [
    {
      "package": "drupal/example",
      "patch_description": "Issue #3123456: [Fix the thing](https://www.drupal.org/i/3123456)",
      "patch_path": "https://www.drupal.org/files/issues/3123456-12.patch",
      "reason": "Fixed"
    }
  ],
  "updated": [],
  "conflicts": []
}
```

A `reason` of `Fixed` is only ever produced when the Drupal.org token is working — it
requires reading issue status. If every entry is a plain "not installed anymore" removal,
the token is not being picked up.

## Watch the conflicts

`conflicts` is the part that needs a human:

```bash
jq -r '.addons.composer_patches.conflicts[] | "\(.package): held at \(.fixed_version), wanted \(.new_version)"' report.json
```

Each entry is a package this run could **not** update, and which will stay behind on every
subsequent run until someone rerolls the patch or the upstream issue is fixed. Reviewing
these periodically is how a project avoids quietly accumulating pinned dependencies.

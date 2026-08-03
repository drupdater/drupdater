# Update multiple sites in one repository

Drupdater updates every site in a multi-site repository in a single run, producing one
branch and one pull or merge request covering all of them.

Shared code is updated once. Each site then gets its own baseline install, its own update
hooks, and its own configuration export.

## 1. Let Drush resolve the active site

Drupdater sets the **`SITE_NAME`** environment variable on every Drush invocation, to the
site it is currently processing. Your `web/sites/sites.php` needs to read it and map it
onto a site directory.

Add this to `web/sites/sites.php`, or to a file it includes:

```php
$site_name = getenv('SITE_NAME');
if (is_string($site_name) && $site_name !== "") {
  $scheme = $request->getScheme();
  $port = $request->getPort();
  $site = $request->getHost();
  if ($site !== '') {
    if (('http' === $scheme && 80 != $port) || ('https' === $scheme && 443 != $port)) {
      $site = $port . '.' . $site;
    }
    if (!isset($sites[$site])) {
      $sites[$site] = $site_name;
    }
  } else {
    $sites[str_replace('/', '.', dirname($script_name))] = $site_name;
  }
}
```

The two branches handle Drush being invoked both with and without a resolvable host. Your
existing host-to-directory mappings are untouched — the snippet only fills in a mapping
when `SITE_NAME` is set, which is only ever the case under Drupdater.

## 2. List the sites

In `.drupdater.yaml`:

```yaml
sites: [default, intranet, careers]
```

These are directory names under `web/sites/`. Each must have its own `settings.php`
committed, or the run cannot install a baseline for it.

## 3. Verify before scheduling

```bash
drupdater check
```

You should see one settings check per site:

```text
✓ .drupdater.yaml valid (sites: default, intranet, careers)
✓ site "default": settings.php
✓ site "intranet": settings.php
✓ site "careers": settings.php
```

Then prove each one actually installs:

```bash
drupdater check --full
```

## Tuning concurrency

Sites are installed and updated **concurrently**, limited by `--concurrency` — which
defaults to `GOMAXPROCS(0)`, reflecting the container's CPU quota.

Site installs are as much I/O-bound as CPU-bound, so the CPU-derived default is a
starting point rather than an answer:

```bash
# Constrained runner, or a slow disk — serialise to avoid thrashing
drupdater "$DRUPDATER_TOKEN" --concurrency 1

# Fast NVMe with many small sites — push past the CPU count
drupdater "$DRUPDATER_TOKEN" --concurrency 8
```

If any site fails, the remaining ones are cancelled and the whole run fails. Sites are not
updated independently — the point is a single, coherent request.

## Disk requirements

Each site gets its own SQLite database and private files directory, written **beside** the
working directory:

```text
<working-dir>/../default.sqlite
<working-dir>/../intranet.sqlite
<working-dir>/../careers.sqlite
<working-dir>/../private/<site>/
```

Budget for one full database per site. Checkout-mode runs remove all of them afterwards,
including the `private/` directory itself — but only if it is empty, so a project's real
private files directory survives.

## What the request looks like

One branch, one request, covering every site. Addon sections that are per-site gain a
heading per site:

- [`update_hooks`](../reference/addons/update-hooks.md) lists pending hooks under a
  `### Site: <name>` heading for each.
- [`translations_updater`](../reference/addons/translations-updater.md) records each
  site's outcome separately in the [run report](../reference/run-report.md), including
  whether it was skipped and why.
- [`unsupported_modules`](../reference/addons/unsupported-modules.md) deduplicates across
  sites, so a module installed everywhere is listed once.

## A working example

[`drupdater/test-drupal-multisite`](https://github.com/drupdater/test-drupal-multisite) is
a two-site project laid out exactly as described above, and Drupdater's integration job
runs against it. Read it if you would rather copy a working `sites.php`,
`settings.php` and configuration layout than assemble one from the snippets here.

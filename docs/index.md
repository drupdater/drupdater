---
hide:
  - navigation
  - toc
---

<div class="dr-hero" markdown>
<div class="dr-hero__copy" markdown>

# Drupdater

<p class="dr-hero__tagline">Automated, reviewable Drupal updates — as a pull or merge
request, on every schedule.</p>

Point it at a Drupal checkout in CI. It runs `composer update`, repairs or drops patches,
applies code-quality fixes, exports configuration and translations, and opens a request
carrying the dependency diff and the pending database update hooks.

**You review and merge. Drupdater never deploys anything on its own.**

[Get started :material-arrow-right:](tutorials/first-run.md){ .md-button .md-button--primary }
[Browse the reference](reference/index.md){ .md-button }

<p class="dr-badges">
  <span class="dr-badge">GitHub &amp; GitLab</span>
  <span class="dr-badge">Ships as a Docker image</span>
  <span class="dr-badge">Multi-site</span>
  <span class="dr-badge dr-badge--warn">Pre-1.0 · v0.x</span>
</p>

</div>
<div class="dr-term">
  <div class="dr-term__bar"><i></i><i></i><i></i><em>drupdater</em></div>
  <pre class="dr-term__body"><code><span class="p">$</span> docker run ghcr.io/drupdater/drupdater-php8.3:latest "$TOKEN"
<span class="d">INFO</span>  running composer install
<span class="d">INFO</span>  updating dependencies
<span class="d">INFO</span>  dependencies updated  <span class="ok">{"total": 42, "upgraded": 39}</span>
<span class="d">INFO</span>  removing deprecations
<span class="d">INFO</span>  updating site         <span class="ok">{"site": "default"}</span>
<span class="d">INFO</span>  update hooks found    <span class="ok">{"site": "default", "count": 4}</span>
<span class="d">INFO</span>  merge request created <span class="ok">{"url": ".../pull/128"}</span>
<span class="d">INFO</span>  update finished</code></pre>
</div>
</div>

!!! warning "Project status: pre-1.0 (`v0.x`)"

    Drupdater is in active development and used in real pipelines, but the CLI surface
    and config format may still change between minor versions. Pin to a specific image
    tag in production.

## Start here

<div class="grid cards dr-cards" markdown>

-   :material-school:{ .lg .middle } **[Tutorials](tutorials/index.md)**

    ---

    Learning-oriented. Start with [your first run](tutorials/first-run.md) against a
    throwaway repository, then wire up [scheduled updates in
    CI](tutorials/scheduled-updates.md).

-   :material-wrench:{ .lg .middle } **[How-to guides](how-to/index.md)**

    ---

    Task-oriented recipes for a goal you already have: running in [GitHub
    Actions](how-to/github-actions.md) or [GitLab CI](how-to/gitlab-ci.md), [updating
    several sites](how-to/update-multiple-sites.md), [enabling
    auto-merge](how-to/enable-auto-merge.md), [troubleshooting a
    run](how-to/troubleshoot.md).

-   :material-book-open-variant:{ .lg .middle } **[Reference](reference/index.md)**

    ---

    The precise details. Every [CLI flag](reference/cli/drupdater.md), the
    [`.drupdater.yaml` schema](reference/configuration.md), all ten
    [addons](reference/addons/index.md), and the [run report
    schema](reference/run-report.md).

-   :material-lightbulb-on:{ .lg .middle } **[Explanation](explanation/index.md)**

    ---

    Why it is built this way. [How a run works](explanation/how-a-run-works.md), the
    [addon architecture](explanation/addon-architecture.md), [credential
    handling](explanation/credentials-and-redaction.md), and what Drupdater
    [deliberately does not do](explanation/non-goals.md).

</div>

## Quick start

Try it against any GitHub or GitLab repository. Pick the image matching your site's PHP
version:

```bash
docker run ghcr.io/drupdater/drupdater-php8.3:latest \
  <token> --clone --repository-url https://github.com/you/your-drupal-site.git
```

Add `--dry-run` to do everything except push the branch and open the pull request. See
[your first run](tutorials/first-run.md) for a guided walkthrough, or
[prerequisites](how-to/run-preflight-checks.md) to check a project is ready.

## What a run does

<div class="grid cards dr-cards" markdown>

-   :material-package-variant:{ .lg .middle } **Dependency updates**

    ---

    `composer update`, committed. `--security` narrows the run to packages with known
    advisories.

-   :material-bandage:{ .lg .middle } **Patch management**

    ---

    Drops obsolete patches, verifies the remaining ones still apply, and pulls updated
    patch files from Drupal.org.

-   :material-auto-fix:{ .lg .middle } **Code quality**

    ---

    `phpcbf`, auto-generating a `phpcs.xml` baseline if missing, plus deprecation
    removal through `drupal-rector`.

-   :material-translate:{ .lg .middle } **Config & translations**

    ---

    Exports Drupal configuration, and updates interface translations via Drush when
    `locale_deploy` is enabled.

-   :material-text-box-check:{ .lg .middle } **A readable changelog**

    ---

    Full dependency diff table and pending database update hooks, written into the
    pull/merge request description.

-   :material-source-branch:{ .lg .middle } **GitHub & GitLab**

    ---

    Including self-hosted GitLab, several sites in one repository, Composer hygiene, and
    an optional machine-readable [run report](reference/run-report.md).

</div>

Each of these is an [addon](reference/addons/index.md), and most can be turned on or off
per run type.

## How a run works

<ol class="dr-steps">
  <li><strong>Acquire working copy</strong><span>Use the checkout CI gave you, or clone it with <code>--clone</code>.</span></li>
  <li><strong>Preflight</strong><span>Refuse early on a project that cannot be updated safely.</span></li>
  <li><strong>Composer install</strong><span>Reproduce the dependency state as it is today.</span></li>
  <li><strong>Baseline site install</strong><span>Install each site from its committed configuration.</span></li>
  <li><strong>Update shared code</strong><span>Composer update, patches, phpcbf, Rector.</span></li>
  <li><strong>Site update</strong><span>Per-site configuration export and translations.</span></li>
  <li><strong>Publish</strong><span>Push the branch and open the pull or merge request.</span></li>
</ol>

The long version: [how a run works](explanation/how-a-run-works.md).

## Support

- **Bugs and features** — [GitHub Issues](https://github.com/drupdater/drupdater/issues)
- **Questions and ideas** —
  [GitHub Discussions](https://github.com/drupdater/drupdater/discussions)
- **Vulnerabilities** — see the [security policy](contributing/security-policy.md); do
  not open a public issue.

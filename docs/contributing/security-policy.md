# Security policy

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Drupdater handles access tokens and modifies dependency trees, so vulnerabilities can have
real impact. Report them privately instead:

- Preferred: open a [GitHub Security
  Advisory](https://github.com/drupdater/drupdater/security/advisories/new).

Please include:

- A description of the issue and its impact.
- Steps to reproduce, or a proof of concept.
- The affected version — image tag or commit.

We will acknowledge your report, investigate, and keep you informed of progress. Please
give us reasonable time to release a fix before any public disclosure.

## Supported versions

Drupdater is pre-1.0 and under active development. Security fixes are applied to the
**latest release only**. Pin to a specific image tag in production and update regularly.

## How Drupdater handles credentials

If you are assessing Drupdater's handling of secrets, [Credentials and
redaction](../explanation/credentials-and-redaction.md) documents the model in full. In
summary:

- Secrets are registered with a redactor as early as they become known, and the redactor
  wraps the logging core — so every entry at every level is filtered, including debug
  output.
- Redaction is **value-based**, not pattern-based: known values are replaced wherever they
  appear, together with their URL-escaped forms.
- The [run report](../reference/run-report.md) is redacted as a whole serialised document,
  not field by field, so a credential arriving through an unexpected field is still
  caught.
- Repository URLs have embedded userinfo stripped before being recorded.
- Credentials are never written to disk, embedded in commits, or included in the request
  description.

A checkout-mode `--dry-run` constructs no VCS client at all, so it cannot reach the
platform even accidentally.

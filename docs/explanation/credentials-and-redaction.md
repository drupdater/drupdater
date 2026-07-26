# Credentials and redaction

Drupdater handles a VCS token, optionally a Drupal.org token, and optionally Composer
registry credentials. It also shells out constantly to Composer, Drush and git — processes
whose output it does not control and cannot predict.

## When a token is required

Not always, and the rule is narrower than it first appears. A token is required only when
the run will actually **use** one — that is, when it clones or publishes:

| Mode | Token | Why |
|---|---|---|
| Checkout, real run | Required | Pushes a branch, opens a request |
| Checkout, `--dry-run` | **Not required** | Does neither |
| `--clone`, real run | Required | Both |
| `--clone`, `--dry-run` | Required | Cloning may itself be authenticated |
| `check` | Never required | Its absence just skips one check |

The checkout-mode dry run is stronger than "the token is unused". **No VCS client is
constructed at all.** There is no object that could make a request, so a dry run cannot
touch the platform even accidentally — which is what makes [the first
run](../tutorials/first-run.md) safe to hand to someone evaluating the tool.

There is one place this shows through: under `--dry-run`, the check for whether the update
branch already exists on the remote is skipped. Not because it would be expensive, but
because the git library sends an *empty password* rather than no credential at all, and
hosts reject that even for public repositories.

## What the token needs to do

Push a branch and open a pull or merge request. Nothing else — Drupdater never merges
(unless [auto-merge](../how-to/enable-auto-merge.md) is explicitly enabled), never deploys,
and never force-pushes.

On GitHub, the built-in `GITHUB_TOKEN` is sufficient for both. Its limitation is not
permissions but consequences: **pull requests opened with it do not trigger other
workflows**, so your test suite never runs on the update. Since the whole premise is that
you review a validated change, that limitation is usually disqualifying — hence the
recommendation to use a PAT or App token.

### The GitHub Actions bot identity

When the Actions `GITHUB_TOKEN` is used, asking GitHub who the token belongs to returns a
`403`. Drupdater recognises that specific response and falls back to the canonical
`github-actions[bot]` identity, so commits are attributed correctly without requiring a
PAT purely for the author field.

Any *other* `403` is treated as a real failure rather than silently papered over.

## Redaction is value-based

Log redaction works by **registering known secret values**, not by matching patterns that
look like secrets.

Pattern matching fails in both directions: it misses credentials in shapes it does not
recognise, and it mangles innocent output that happens to match. Value-based redaction has
neither problem — a registered secret is replaced wherever it appears, and nothing else
ever is.

Each secret is registered together with its URL-escaped form, since a credential embedded
in a URL is percent-encoded by the time a subprocess echoes it back. Secrets are sorted
longest-first, so one that is a substring of another is matched in full rather than
partially.

The redactor **wraps the logging core**, not individual log calls. Every entry at every
level passes through it, including debug output under `--verbose`. There is no code path
that can log around it.

### Registered as early as possible

Environment-carried secrets — `DRUPALCODE_ACCESS_TOKEN`, `COMPOSER_AUTH` — are registered
before the logger is even built. The VCS token is registered the instant it is resolved.
The window in which a value is known but not yet redactable is as close to zero as it can
be made.

The secrets themselves are never logged, including at debug level.

### `COMPOSER_AUTH` is unpacked first

`COMPOSER_AUTH` is a JSON object. Composer does not echo the blob — it echoes the
individual password or token inside it, typically embedded in a URL after a failed fetch.
Registering only the raw string would therefore never match the output that actually
leaks.

So every string leaf of the parsed JSON is registered individually. With one exception:
values keyed **`username`**. Private Packagist's documented form sets the username to the
literal word `token`, and redacting that word would replace it throughout unrelated log
output. If the value does not parse as JSON at all, the raw string is registered as a
fallback.

## The report is redacted too

The [run report](../reference/run-report.md) gets the same treatment, applied to the
**whole serialised document** rather than to individual fields.

Field-level redaction only protects the fields someone thought to redact. Whole-document
redaction also catches a credential arriving through an unexpected one — an error string
quoting an authenticated URL, say, which is exactly the kind of value that ends up in an
`error` field.

Separately, the repository URL has any embedded userinfo stripped before it is recorded, so
`https://user:token@host/repo.git` never appears even in unredacted form.

`drupdater check` prints its results straight to stdout rather than through the logger, so
its failure details are passed through the redactor explicitly.

## What Drupdater does not do

- It does not write credentials to disk.
- It does not embed the token in the pushed branch, the commits, or the request
  description.
- It does not send credentials anywhere except the host they belong to.

The push itself authenticates with HTTP basic auth using the token as the password and a
placeholder username — the username is not meaningful to either platform, it merely has to
be non-empty.

# The configuration model

Drupdater's configuration is split into two tiers with **no overlap**. Every setting lives
in exactly one of them, and which one is decided by a single question: does this describe
the project, or the invocation?

| Tier | Describes | Lives in |
|---|---|---|
| [CLI flags](../reference/cli/drupdater.md#flags) | How this particular run is invoked | The command line |
| [`.drupdater.yaml`](../reference/configuration.md) | What the project needs | The repository root |

## Why split them at all

Because they have different lifetimes and different owners.

`--dry-run` is true for one invocation and false for the next. A token rotates. `--report`
points at a path that only means something inside one CI job. None of that is a property
of the Drupal project — putting it in a committed file would mean committing a change to
rehearse a run.

Conversely, the list of sites is a fact about the project. It does not vary by invocation,
and someone reading the repository should be able to see it without finding the pipeline
that runs Drupdater. Making it a flag would mean the same value repeated in every job
definition, drifting the moment someone adds a site.

The test is: **would two different invocations against this project ever want different
values?** If yes, it is a flag. If no, it belongs in the file.

## The case of `--concurrency`

`--concurrency` looks like it belongs in the file — it is about the sites, after all — and
it deliberately does not.

It describes **the machine**, not the project. The right value on a constrained shared
runner differs from the right value on a fast NVMe box, for the same repository. Committing
it would bake one runner's characteristics into the project.

## Why the file is keyed by run type

Settings that differ between a normal and a security update live under `run_types`:

```yaml
run_types:
  normal:
    addons: [code_beautifier, deprecations_remover]
    auto_merge: false
  security:
    addons: []
    auto_merge: true
```

The obvious alternative — and what Drupdater used to do — is to key by setting instead:

```yaml
addons:
  normal: [code_beautifier, deprecations_remover]
  security: []
auto_merge:
  normal: false
  security: true
```

Both express the same thing. The difference is what happens when you want to *understand*
a security run. Keyed by setting, you read the `security` field out of every block in the
file, and the answer is assembled from as many places as there are settings. Keyed by run
type, it is one stanza.

That asymmetry gets worse with every setting added, which is why the layout changed. See
[Migrate the config layout](../how-to/migrate-config-layout.md).

Nesting under `run_types` has a second benefit: anything under that key is unambiguously
per-run-type and anything at the root is unambiguously global, so a future global setting
can never collide with a run type name.

### One place decides which block applies

The mapping from `--security` to a config block is stated exactly once, in a single
accessor. Every consumer — the addon builder, the publisher — calls it rather than
branching on the security flag itself.

Otherwise "which settings does a security run use" would be answered independently in
several places, and they would eventually disagree.

## Why unknown keys are rejected

The file is decoded strictly. A key Drupdater does not recognise fails the run.

The alternative is silently ignoring it, which means `site: [default]` — singular, a
plausible typo — behaves exactly like an empty file. The project would keep updating only
`default` while the config appears to say otherwise, and nothing would ever surface the
mistake.

Failing at startup costs one confused moment. Ignoring it costs an unbounded amount of time
much later.

The same reasoning applies to addon names: an unknown one aborts the run rather than being
skipped, and names in **both** run type blocks are validated regardless of which is active
— so a typo in the security block is caught by a normal run, rather than lying in wait for
the next advisory.

## Why an absent file is fine

A missing `.drupdater.yaml` is not an error. Neither is an empty one, or one containing
only comments.

Decoding is layered over a fully-populated default struct, so a file setting only `sites`
gets every other key at its default. There is no partially-configured state to reason
about: the config is always complete by the time anything reads it.

This means Drupdater works on a project that has never heard of it, which is what makes
[the first run](../tutorials/first-run.md) a single command rather than a setup task.

## The one thing that cannot be defaulted

`sites` must not be an empty list, and this is a hard error rather than a no-op.

Every per-site phase iterates that list. An empty one would skip the baseline install, the
update hooks and the configuration export — and then **still open a merge request**, for an
update that was never validated against a running site. A silently-empty list is the one
configuration mistake that produces a confidently wrong result rather than an obvious
failure.

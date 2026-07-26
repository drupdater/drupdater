# What Drupdater does not do

Some of the more useful things about a tool are the things it declines to do. These are
deliberate.

## It does not deploy

Drupdater pushes a branch and opens a pull or merge request. That is the end of its
involvement.

It never merges on its own — unless you explicitly turn on
[auto-merge](../how-to/enable-auto-merge.md), which delegates the decision to your
pipeline. It never pushes to your default branch, never force-pushes, and never touches a
deployment.

The reason is that a dependency update is a change like any other, and your project already
has a process for changes. Drupdater's job is to produce a good change with a good
description; getting it into production is your existing pipeline's job, using whatever
gates you already trust.

## It does not run your test suite

This is the most common feature request, and the answer is no.

Drupdater runs **as a step in your CI pipeline**, and that pipeline already runs your tests
on the branch and request it produces. Running them a second time inside Drupdater would:

- Duplicate infrastructure that already exists and is already configured correctly.
- Require Drupdater to know how your project runs PHPUnit, Behat, Cypress, or whatever else
  — configuration it has no business holding.
- Produce a second, differently-configured verdict that could disagree with your real one.

The tests that matter are the ones your pipeline runs on the resulting request, in the
environment you already trust. Drupdater deliberately has no opinion about them.

The exception proves the rule: the [baseline site install](how-a-run-works.md) does install
a real Drupal site. Not to test it — to build the database that update hooks need in order
to run at all.

## It does not decide what is safe

Drupdater reports; it does not judge.

It will tell you an update carries database update hooks, that a module is
[unsupported](../reference/addons/unsupported-modules.md), that a
[patch conflict](../reference/addons/composer-patches.md) held a package back, and which
[advisories remain open](../reference/addons/composer-audit.md) after the update. It will
not decide whether any of that is acceptable for your project.

That is why the request description is as detailed as it is. The tool's output is
information for a decision, not the decision.

## It does not modify your working copy on failure

A failed run restores the checkout to the commit it started from, discarding the throwaway
work branch and any temporary state — including the permissive Composer plugin
configuration that [`composer_allow_plugins`](../reference/addons/composer-allow-plugins.md)
sets during the update.

A failed run should leave your checkout exactly as it found it, so that rerunning is safe
and so that a developer machine is never left in a state that needs manual repair.

Similarly, [`drupdater check --full`](../reference/cli/check.md) clones to a scratch
directory rather than using the live working copy, precisely so a validation command can
never damage anything.

## It does not install tooling you have not adopted

[`composer_normalizer`](../reference/addons/composer-normalizer.md) runs `composer
normalize` **only if** the project already depends on the normaliser. If not, it logs and
skips.

A project that has not adopted a formatting tool has not asked for its `composer.json` to
be reordered, and doing so unprompted would produce a large diff nobody requested.

The addons that *do* install something — `drupal/coder` for PHPCBF, Rector for deprecation
removal — install it as a dev dependency for the duration and, in Rector's case, remove it
again afterwards.

## It does not have a plugin API

Addons are compiled in. There is no mechanism for loading an addon from a project, and no
stable Go API for writing one out of tree.

Adding an addon means [contributing one](../contributing/writing-an-addon.md). That keeps
the addon set reviewable as a whole, keeps the [report
schema](../reference/run-report.md) a single coherent contract, and means an update to
Drupdater cannot break someone's out-of-tree code that nobody knew existed.

## It is not a migration tool

Drupdater updates within what Composer can resolve. It does not perform major version
upgrades that need manual intervention, rewrite your code beyond
[Rector's deprecation rules](../reference/addons/deprecations-remover.md), or replace an
unsupported module with a different one.

When it finds something it cannot fix — an unsupported module, a patch that cannot be
rerolled, an advisory needing a major bump — it reports it clearly and moves on. Those are
exactly the cases that need a human, and the useful thing a tool can do is surface them
early rather than pretend to solve them.

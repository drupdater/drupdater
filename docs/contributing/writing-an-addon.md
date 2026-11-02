# Writing an addon

An addon is a unit of work that hooks into a [workflow
event](../explanation/addon-architecture.md) and does one job when it fires. Adding a
capability to Drupdater almost always means adding an addon rather than touching the
workflow.

Addons are PHP, in the [agent](development.md#the-agent): `agent/src/Addon/`. The binary finds
them in the agent's catalogue and calls their hooks; it has no code of its own per addon. See
[Where an addon runs](../explanation/addon-architecture.md#where-an-addon-runs).

## The rules

**Addons never call each other.** If your addon needs another one to have run first,
express that as the stage of its hook, not as a reference. If it needs to influence what another
addon does, and the event is `pre-composer-update`, return an [update
plan](../explanation/addon-architecture.md#steering-the-update-through-proposals) from the
hook. Your addon proposes. It never edits what another addon reads.

**Addons never touch git.** A hook returns the paths it changed as commits, and the binary
commits them — see [Committing changes](../explanation/addon-architecture.md#committing-changes).

**Addons keep no state.** Each hook runs in a process of its own. What an earlier hook of your
addon found comes back with the event, as `$event->earlier`.

**Addons swallow their own failures** where the work is optional. A transient failure in
translation updates should not throw away an otherwise complete dependency update. This is
also exactly why your addon must report to the run report — see step 4.

## 1. Implement the interface

Create `agent/src/Addon/YourAddon.php`, implementing `Drupdater\Agent\Addon\Addon`:

```php
final class YourAddon implements Addon
{
    public const COMMIT_MESSAGE = 'Describe the change';

    public function __construct(private readonly Log $log, private readonly Drush $drush)
    {
    }

    public static function name(): string
    {
        return 'your_addon';
    }

    public static function hooks(): array
    {
        return [EventName::PostSiteUpdate->value => Stage::Change];
    }

    public static function create(Services $services): static
    {
        return new self($services->log, $services->drush());
    }

    public function handle(Event $event): HookResult
    {
        // Change files under $event->projectDir, then name them: drupdater commits them.
        return new HookResult(['path' => 'translations'], [new Commit(self::COMMIT_MESSAGE, ['translations'])]);
    }

    public static function report(Findings $findings): mixed
    {
        // See step 4.
    }
}
```

- **`name()`** is what users write in `.drupdater.yaml`, and the key of the report section.
- **`hooks()`** lists each event the addon hooks and the [stage](#choosing-an-event-and-stage)
  its hook runs at. `handle()` is called only for those events; branch on `$event->name` when
  there are several.
- **`create()`** builds the addon from the agent's real tools: `Services` is the composition
  root, with the Composer instance, the project, the resolver, the patch tools, drupal.org and
  drupalcode clients, phpcs, Rector, drush and Composer run again. Take what you need as
  constructor arguments, so a test can build the addon with test doubles instead.
- **`handle()`** returns a `HookResult`: what the hook found (anything JSON can hold, `null`
  for nothing), the commits it asks for, and, on `pre-composer-update` only, its proposal for
  the update. To end a run that has nothing to do, throw `AbortRun`; to fail it, throw a
  `Failure`.

Log through `Log`, at `debug`, `info`, `warning` or `error`, with context as an array of
JSON-encodable values. The entries reach drupdater's log at the same level, redacted like the
rest of it. Never write to the output yourself: the agent's stdout is its reply.

A hook that has to undo or compare against what an earlier one of the same addon did —
`composer_allow_plugins` closing what its pre-update hook opened, `composer_audit` comparing
the audits before and after — reads that from `$event->earlier`: `shared()` for an event that
is not per site, `sites()` for one that is.

### Choosing an event and stage

| Event | Use for |
|---|---|
| `pre-composer-update` | Influencing what the update touches: return a plan, with any commits and finding |
| `post-composer-update` | Reacting to the new lock file and installed code, before it is committed |
| `post-code-update` | Changing code — Rector, PHPCBF, and similar |
| `pre-site-update` | Inspecting a site before its update hooks run |
| `post-site-update` | Per-site work after hooks, before configuration export |

Each event hands its hooks what it carries: `plan()` on `pre-composer-update`, `baseLock()` on
`post-composer-update`, `site()` and `facts()` on the site events. On `pre-site-update` the
facts are what the site reported before its update hooks ran, on `post-site-update` what it
reported after them, without the unsupported modules. Read a site's modules, paths or pending
hooks from there instead of running drush yourself; a new fact belongs in `drupdater:facts`
(`scripts/drush/Commands/DrupdaterCommands.php`) and `SiteFacts`.

Pick the stage from what the hook does: `Prepare` if later hooks on the event read what it
writes, `Change` if it edits the project, `Format` if it restyles what `Change` wrote, and
`Inspect` if it only observes, so it records the settled state. Read
[Stages](../explanation/addon-architecture.md#stages) before picking one.

### Concurrency

`pre-site-update` and `post-site-update` fire **once per site**. `pre-site-update` hooks run
concurrently, one site per goroutine of the binary and so one agent process each.
`post-site-update` hooks run one site at a time in `--sites` order, once every site's database
is updated, because they may write into the shared working tree. A per-site hook returns what
it found for its site, and `report()` merges every site's — deduplicating, say, as
`unsupported_modules` does.

## 2. Register it

Add a `CatalogueEntry` for it to `Catalogue::builtin()` in `agent/src/Addon/Catalogue.php`. The
catalogue's order is the order hooks sharing an event and a stage run in, so place it where
its stage-mates' order makes sense. An unknown name in `.drupdater.yaml` aborts a run, so the
name is user-visible.

## 3. Decide mandatory or configurable

**Mandatory** — `new CatalogueEntry(YourAddon::class, mandatory: true)`. Reserve this for
addons that are required for the update to succeed at all, or that produce the changelog a
reviewer needs. It is a high bar: a mandatory addon cannot be turned off by any project.

**Configurable** — the default. If a normal update should run it when the project names no
addons, add `enabledByDefault: true`.

A security run that names no addons runs the mandatory ones only, on purpose: a security
update should be a minimal, focused fix.

Then update the [configuration reference](../reference/configuration.md) and add a page under
[addon reference](../reference/addons/index.md).

## 4. Report

`report()` builds the addon's report section from its findings alone, and returns `null` when
there is nothing to report:

```php
public static function report(Findings $findings): mixed
{
    $sites = [];
    foreach ($findings->sites(EventName::PostSiteUpdate) as $site => $recorded) {
        $sites[$site] = ['path' => $recorded->finding()['path'], 'updated' => $recorded->committed(self::COMMIT_MESSAGE)];
    }
    return $sites === [] ? null : (object) $sites;
}
```

Each recorded finding knows which of its hook's commits were made, so a section can say
whether a change really landed, as `translations_updater`'s `updated` does.

Requirements:

- **Return `null` when there is nothing to report** — an omitted key is better than an
  empty one.
- **Report even when the work was "only" a code change.** Because addons swallow their own
  failures, one that silently broke looks exactly like one with nothing to do. This is not
  hypothetical: it [has happened](../explanation/why-a-run-report.md).
- **Sort anything unordered**, and key maps by sorted keys. Two runs over unchanged input
  should produce byte-identical output so reports diff cleanly.
- **Lists are lists, objects are objects.** Cast an associative array that can be empty to an
  object, or it encodes as `[]`.

If your addon records a skip, record **why** — see
[`translations_updater`](../reference/addons/translations-updater.md) for the pattern.

The sections are a published contract. Add an example of your addon's findings to
`agent/protocol/report/request.json` and its section to `response.json`; the agent's suite
asserts it builds exactly that. Then regenerate the whole report's golden, which embeds those
sections: `go test ./internal/report -update`.

## 5. Add a description section

The pull request description is rendered from the report by `internal/mergerequest`, so an
addon appears in it through its report section. To give it one:

1. Add `internal/mergerequest/templates/your_addon.go.tmpl`.
2. In `internal/mergerequest/mergerequest.go`, read your section into `sections` — declaring
   only the fields the template shows, by their JSON names — and add it to
   `sectionsInOrder`, where the order of the description is decided.
3. Regenerate the goldens: `go test ./internal/mergerequest -update`.

The renderer reads the report through its JSON, like any consumer, so it can only show what
your section publishes. Follow the conventions in [Development
setup](development.md#description-templates). The section's golden file in
`internal/mergerequest/testdata/` is embedded straight into the [addon
reference](../reference/addons/index.md), so a good one becomes the published example.

## 6. Test it

Test the addon in `agent/tests/Addon/`: build it with its constructor and test doubles, call
`handle()` with an event from `Event`'s named constructors, and assert on the `HookResult`; call
`report()` with findings built by `Records`. A tool the addon runs gets its own test against a
`FakeProcessRunner` or `FakeHttp`. When the addon asks Composer something, test against a real
Composer for a scratch project (`InProject`), and add a case to `EndToEndTest` to run its hook
through a real global Composer.

```bash
make test-agent
```

Then verify end to end against a real project:

```bash
go run . --working-dir /path/to/a/drupal/checkout --dry-run --verbose --report ./report.json
jq '.addons.your_addon' report.json
```

Running drupdater outside its image needs the agent installed globally from your checkout —
see [The agent](development.md#the-agent). Check the rendered description too —
`jq -r '.merge_request_description' report.json`. Template output is easy to get subtly wrong,
and `--dry-run` still renders it.

## Checklist

- [ ] Implements `Addon`, each hook at a justified stage
- [ ] Its tools taken as constructor arguments, built in `create()` from `Services`
- [ ] Registered in `Catalogue::builtin()`, mandatory or default decided
- [ ] `report()` returning `null` when empty, output sorted
- [ ] Its findings and section in `agent/protocol/report/`, and the report golden regenerated
- [ ] Description section, if any, rendered from the report section and pinned by a golden
- [ ] Tests in `agent/tests/`
- [ ] Documented: a page under `docs/reference/addons/`, listed in the catalogue table, and
      added to the [configuration reference](../reference/configuration.md)

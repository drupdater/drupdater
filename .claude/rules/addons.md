When adding a new addon:

1. Write it in PHP, in `agent/src/Addon/`, implementing `Drupdater\Agent\Addon\Addon`: `name()`, `hooks()` (each event at a stage), `create(Services)`, `handle(Event)` and `report(Findings)`. No calls between addons, no git: return the changed paths as commits in the `HookResult`.
2. Take every tool as a constructor argument and build it in `create()` from `Services`, so tests can build the addon with doubles. A tool the agent lacks goes in `agent/src/Tools/` (or a sibling directory), with its own test.
3. Register it in `Catalogue::builtin()` (`agent/src/Addon/Catalogue.php`), in the order hooks sharing a stage should run, and decide: `mandatory: true`, `enabledByDefault: true`, or neither.
4. The name is what users write in `.drupdater.yaml`; an unknown name aborts a run.
5. `report()` builds the section from the findings alone; return `null` when there is nothing to report, and sort anything unordered so reports stay byte-stable. Add an example of its findings to `agent/protocol/report/request.json` and its section to `response.json`, then regenerate the report golden with `go test ./internal/report -update`.
6. Test it in `agent/tests/` (`make test-agent`); nothing changes on the Go side for an addon.
7. Document it: a new page at `docs/reference/addons/<name>.md`, a row in the catalogue table in `docs/reference/addons/index.md`, an entry in `mkdocs.yml`'s nav, and the addon list in `docs/reference/configuration.md`.

The full procedure, including event/stage guidance and a checklist, is in `docs/contributing/writing-an-addon.md`.

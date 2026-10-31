# Build an addon outside Drupdater

An addon does not have to live in this repository. The public
[`sdk`](https://pkg.go.dev/github.com/drupdater/drupdater/sdk) package lets you write one in
your own Go module and compile it into a Drupdater binary of your own — every builtin addon,
plus yours. A project then turns it on in `.drupdater.yaml` like any other addon.

Use this for work that is specific to your agency or platform (resetting a file your hosting
needs, checking a house rule, reporting to an internal system). If it would help every Drupal
project, [contribute it as a builtin addon](../contributing/writing-an-addon.md) instead.

Your addon is held to the same rules as the builtin ones, described in [the addon
architecture](../explanation/addon-architecture.md): it hooks into events, never calls
another addon, returns the paths it changed rather than committing, and returns what it
found rather than keeping it.

## 1. Create a module

```bash
mkdir drupdater-acme && cd drupdater-acme
go mod init example.com/drupdater-acme
go get github.com/drupdater/drupdater@latest
```

The module needs the Go version named in Drupdater's own `go.mod`.

## 2. Write the addon

```go
package main

import (
	"os"
	"path/filepath"

	"github.com/drupdater/drupdater/sdk"
)

// RobotsTxt puts back the robots.txt drupal/core-composer-scaffold overwrites.
type RobotsTxt struct {
	deps sdk.Deps
}

func (a *RobotsTxt) Hooks() sdk.Hooks {
	return sdk.Hooks{
		PostCodeUpdate: sdk.Produce(sdk.Change, a.reset),
	}
}

func (a *RobotsTxt) reset(e *sdk.PostCodeUpdateEvent) (sdk.Result[bool], error) {
	path := filepath.Join(e.Path(), "web", "robots.txt")
	if err := os.WriteFile(path, []byte("User-agent: *\nDisallow: /\n"), 0o644); err != nil {
		return sdk.Result[bool]{}, err
	}
	return sdk.Result[bool]{
		Commits: []sdk.Commit{{Message: "Reset robots.txt", Paths: []string{"web/robots.txt"}}},
		Finding: true,
	}, nil
}
```

The hook builders, events, stages and `UpdatePlan` are the ones the builtin addons use; the
[addon guide](../contributing/writing-an-addon.md#choosing-an-event-and-stage) explains how
to choose an event and a stage. The `sdk.Deps` an addon is built with carries:

| Field | What it is |
|---|---|
| `Logger` | A `*zap.Logger` named after the addon, redacting secrets like every other log line |
| `Composer` | `Exec(ctx, dir, args...)`: runs composer under the environment Drupdater's own calls use |
| `Drush` | `Exec(ctx, dir, site, args...)`: runs drush for one site, through the project's `composer exec` |
| `Security` | Whether this is a `--security` run |

Site facts — enabled modules, pending update hooks, the config sync directory — are on the
site events as `e.Facts()`; read them there rather than asking Drush.

Per-site hooks run concurrently across sites, so an addon must keep no state of its own:
return findings from hooks and read them back in `ReportData`.

## 3. Report what it did

An addon that has something to tell a reviewer implements `sdk.Reporter`. Its report key
**must be its registered name**; Drupdater refuses to run otherwise.

```go
func (a *RobotsTxt) ReportKey() string { return "acme_robots_txt" }

func (a *RobotsTxt) ReportData(f *sdk.Findings) any {
	reset, ok := sdk.Shared[bool](f, sdk.EventPostCodeUpdate)
	if !ok || len(reset.Committed) == 0 {
		return nil
	}
	return map[string]bool{"reset": true}
}
```

The section appears under `addons.acme_robots_txt` in the [run report](../reference/run-report.md).
To also show it in the merge request description, implement `sdk.Describer`: a
[`text/template`](https://pkg.go.dev/text/template) rendered against your section as it
reads in the report's JSON, so it uses the JSON field names.

```go
func (a *RobotsTxt) DescriptionTemplate() string {
	return "## 🤖 robots.txt\n\n`web/robots.txt` was reset after the scaffold overwrote it."
}
```

Sections of external addons follow the builtin ones, ordered by name. The `cell` function
escapes a value for a markdown table cell. A template that does not parse or execute fails
the `render merge request` phase, before anything is pushed, so `--dry-run` catches it.

## 4. Register it and build the binary

```go
func main() {
	sdk.Main(sdk.Registration{
		Name: "acme_robots_txt",
		New:  func(d sdk.Deps) sdk.Addon { return &RobotsTxt{deps: d} },
	})
}
```

`sdk.Main` is the whole Drupdater command line — Drupdater's own `main.go` is `sdk.Main()`
with nothing registered. A name must be lowercase letters, digits and underscores, starting
with a letter, and may not be taken by a builtin addon; prefix it with your organisation
to stay clear of future ones. An invalid registration fails every command before it starts.

```bash
go build -o drupdater .
./drupdater addons   # acme_robots_txt is listed
```

## 5. Test it

`sdk/sdktest` fires your hooks through Drupdater's own pipeline, in the order a run would,
and records the commits they ask for instead of making them:

```go
func TestRobotsTxtIsReset(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "web"), 0o755)
	h := sdktest.New(dir, &RobotsTxt{})

	require.NoError(t, h.PostCodeUpdate(t.Context()))

	assert.Equal(t, "Reset robots.txt", h.Commits()[0].Message)
	assert.Equal(t, map[string]bool{"reset": true}, h.Report()["acme_robots_txt"])
	description, _ := h.Description()
	assert.Contains(t, description, "## 🤖 robots.txt")
}
```

`sdk.Run(ctx, args, registrations...)` runs a command line in-process and returns its exit
status, for a test that the binary starts with your registrations.

## 6. Ship it in the image

The binary needs the PHP runtime the [published images](../reference/docker-images.md)
carry. Build yours on top of one and replace its binary:

```dockerfile
FROM golang:1.27.1-trixie AS build
WORKDIR /build
COPY . .
RUN CGO_ENABLED=0 go build -o drupdater .

FROM ghcr.io/drupdater/drupdater-php8.3
COPY --from=build /build/drupdater /opt/drupdater/bin
```

Then enable the addon in the project's `.drupdater.yaml`:

```yaml
run_types:
  normal:
    addons: [code_beautifier, acme_robots_txt]
```

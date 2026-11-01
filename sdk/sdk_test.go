package sdk_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/drupdater/drupdater/sdk"
	"github.com/drupdater/drupdater/sdk/sdktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// robotsTxt is an addon as a module outside drupdater would write it, through sdk alone.
type robotsTxt struct{}

func (robotsTxt) Hooks() sdk.Hooks {
	return sdk.Hooks{
		PreComposerUpdate: sdk.ProduceProposal(sdk.Prepare, func(*sdk.PreComposerUpdateEvent) (sdk.Proposal[struct{}], error) {
			return sdk.Proposal[struct{}]{Plan: sdk.UpdatePlan{Keep: []sdk.Pin{{Package: "drupal/robotstxt", Version: "1.5.0"}}}}, nil
		}),
		PostCodeUpdate: sdk.Produce(sdk.Change, func(e *sdk.PostCodeUpdateEvent) (sdk.Result[int], error) {
			if err := os.WriteFile(filepath.Join(e.Path(), "robots.txt"), []byte("User-agent: *\n"), 0o600); err != nil {
				return sdk.Result[int]{}, err
			}
			return sdk.Result[int]{
				Commits: []sdk.Commit{{Message: "Reset robots.txt", Paths: []string{"robots.txt"}}},
				Finding: 1,
			}, nil
		}),
		PostSiteUpdate: sdk.Observe(func(e *sdk.PostSiteUpdateEvent) (bool, error) {
			return e.Facts().ModuleEnabled("robotstxt"), nil
		}),
	}
}

func (robotsTxt) ReportKey() string { return "robots_txt" }

func (robotsTxt) ReportData(f *sdk.Findings) any {
	written, ok := sdk.Shared[int](f, sdk.EventPostCodeUpdate)
	if !ok {
		return nil
	}
	var sites []string
	for site, enabled := range sdk.SiteFindings[bool](f, sdk.EventPostSiteUpdate) {
		if enabled {
			sites = append(sites, site)
		}
	}
	slices.Sort(sites)
	return map[string]any{"files": written.Finding, "committed": written.Committed, "module_sites": sites}
}

func (robotsTxt) DescriptionTemplate() string {
	return "## 🤖 robots.txt\n\nReset {{ .files }} file(s); the module is on {{ range .module_sites }}{{ . }} {{ end }}"
}

var (
	_ sdk.Addon     = robotsTxt{}
	_ sdk.Describer = robotsTxt{}
)

func TestAnExternalAddonRunsThroughThePipeline(t *testing.T) {
	ctx := context.Background()
	h := sdktest.New(t.TempDir(), robotsTxt{})

	plan, err := h.PreComposerUpdate(ctx, sdk.UpdatePlan{})
	require.NoError(t, err)
	require.NoError(t, h.PostCodeUpdate(ctx))
	require.NoError(t, h.PostSiteUpdate(ctx, "default", sdk.Facts{Modules: []string{"robotstxt"}}))
	require.NoError(t, h.PostSiteUpdate(ctx, "intranet", sdk.Facts{}))

	assert.Equal(t, []string{"drupal/robotstxt:1.5.0"}, plan.KeepConstraints())
	assert.FileExists(t, filepath.Join(h.Dir, "robots.txt"))
	assert.Equal(t, []sdk.Commit{{Message: "Reset robots.txt", Paths: []string{"robots.txt"}}}, h.Commits())
	assert.Equal(t, map[string]any{
		"files": 1, "committed": []string{"Reset robots.txt"}, "module_sites": []string{"default"},
	}, h.Report()["robots_txt"])

	description, err := h.Description()
	require.NoError(t, err)
	assert.Contains(t, description, "## 🤖 robots.txt\n\nReset 1 file(s); the module is on default \n")
}

func TestTheHarnessFiresEveryEvent(t *testing.T) {
	var fired []string
	record := func(name string) func(any) error {
		return func(any) error { fired = append(fired, name); return nil }
	}
	addon := hooksOnly{sdk.Hooks{
		PostComposerUpdate: sdk.On(sdk.Inspect, func(e *sdk.PostComposerUpdateEvent) error { return record("post-composer-update")(e) }),
		PreSiteUpdate:      sdk.On(sdk.Inspect, func(e *sdk.PreSiteUpdateEvent) error { return record("pre-site-update:" + e.Site())(e) }),
	}}
	h := sdktest.New(t.TempDir(), addon)

	require.NoError(t, h.PostComposerUpdate(context.Background()))
	require.NoError(t, h.PreSiteUpdate(context.Background(), "default", sdk.Facts{}))

	assert.Equal(t, []string{"post-composer-update", "pre-site-update:default"}, fired)
	assert.Empty(t, h.Report(), "an addon that is no Reporter adds no section")
}

func TestProduceFromSeesTheAddonsEarlierFindings(t *testing.T) {
	var before int
	addon := hooksOnly{sdk.Hooks{
		PreSiteUpdate: sdk.Observe(func(*sdk.PreSiteUpdateEvent) (int, error) { return 7, nil }),
		PostSiteUpdate: sdk.ProduceFrom(sdk.Change, func(e *sdk.PostSiteUpdateEvent, f *sdk.Findings) (sdk.Result[int], error) {
			before = sdk.Sites[int](f, sdk.EventPreSiteUpdate)[e.Site()].Finding
			return sdk.Result[int]{}, nil
		}),
	}}
	h := sdktest.New(t.TempDir(), addon)

	require.NoError(t, h.PreSiteUpdate(context.Background(), "default", sdk.Facts{}))
	require.NoError(t, h.PostSiteUpdate(context.Background(), "default", sdk.Facts{}))

	assert.Equal(t, 7, before)
}

type hooksOnly struct{ hooks sdk.Hooks }

func (a hooksOnly) Hooks() sdk.Hooks { return a.hooks }

func ExampleMain() {
	register := sdk.Registration{
		Name: "robots_txt",
		New:  func(sdk.Deps) sdk.Addon { return robotsTxt{} },
	}
	// In the addon's own main package: sdk.Main(register)
	fmt.Println(register.Validate())
	// Output: <nil>
}

func TestRunRegistersTheAddons(t *testing.T) {
	register := sdk.Registration{Name: "robots_txt", New: func(sdk.Deps) sdk.Addon { return robotsTxt{} }}

	assert.Equal(t, 0, sdk.Run(t.Context(), []string{"addons"}, register))
	assert.Equal(t, 1, sdk.Run(t.Context(), []string{"addons"}, register, register), "a name registered twice")
}

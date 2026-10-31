package sdktest_test

import (
	"testing"

	"github.com/drupdater/drupdater/sdk"
	"github.com/drupdater/drupdater/sdk/sdktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type everyEvent struct{}

func commitNamed(name string) sdk.Result[string] {
	return sdk.Result[string]{Commits: []sdk.Commit{{Message: name, Paths: []string{name}}}, Finding: name}
}

func (everyEvent) Hooks() sdk.Hooks {
	return sdk.Hooks{
		PreComposerUpdate: sdk.ProduceProposal(sdk.Change, func(e *sdk.PreComposerUpdateEvent) (sdk.Proposal[string], error) {
			return sdk.Proposal[string]{Plan: sdk.UpdatePlan{MinimalChanges: true}, Commits: commitNamed(e.Path()).Commits}, nil
		}),
		PostComposerUpdate: sdk.Produce(sdk.Change, func(*sdk.PostComposerUpdateEvent) (sdk.Result[string], error) {
			return commitNamed("composer"), nil
		}),
		PostCodeUpdate: sdk.Produce(sdk.Change, func(*sdk.PostCodeUpdateEvent) (sdk.Result[string], error) {
			return commitNamed("code"), nil
		}),
		PreSiteUpdate: sdk.Produce(sdk.Change, func(e *sdk.PreSiteUpdateEvent) (sdk.Result[string], error) {
			return commitNamed("pre-" + e.Site()), nil
		}),
		PostSiteUpdate: sdk.Produce(sdk.Change, func(e *sdk.PostSiteUpdateEvent) (sdk.Result[string], error) {
			return commitNamed("post-" + e.Site()), nil
		}),
	}
}

func (everyEvent) ReportKey() string { return "every_event" }

func (everyEvent) ReportData(f *sdk.Findings) any {
	code, _ := sdk.Shared[string](f, sdk.EventPostCodeUpdate)
	return map[string]string{"code": code.Finding}
}

func (everyEvent) DescriptionTemplate() string { return "## Every event: {{ .code }}" }

func TestHarnessFiresEachEventAndRecordsItsCommitsInOrder(t *testing.T) {
	h := sdktest.New("project", everyEvent{})

	plan, err := h.PreComposerUpdate(t.Context(), sdk.UpdatePlan{})
	require.NoError(t, err)
	require.NoError(t, h.PostComposerUpdate(t.Context()))
	require.NoError(t, h.PostCodeUpdate(t.Context()))
	require.NoError(t, h.PreSiteUpdate(t.Context(), "default", sdk.Facts{}))
	require.NoError(t, h.PostSiteUpdate(t.Context(), "default", sdk.Facts{}))

	assert.True(t, plan.MinimalChanges)
	var messages []string
	for _, c := range h.Commits() {
		messages = append(messages, c.Message)
		assert.Equal(t, []string{c.Message}, c.Paths)
	}
	assert.Equal(t, []string{"project", "composer", "code", "pre-default", "post-default"}, messages)
	assert.Equal(t, map[string]string{"code": "code"}, h.Report()["every_event"])
	description, err := h.Description()
	require.NoError(t, err)
	assert.Contains(t, description, "## Every event: code\n")
}

func TestHarnessCommitsAreACopy(t *testing.T) {
	h := sdktest.New("project", everyEvent{})
	require.NoError(t, h.PostCodeUpdate(t.Context()))

	h.Commits()[0].Message = "changed"

	assert.Equal(t, "code", h.Commits()[0].Message)
}

package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/drush"
)

type recordedCommit struct {
	message string
	paths   []string
}

// fakeCommitter records what an event was asked to commit and answers with result and err.
type fakeCommitter struct {
	commits []recordedCommit
	result  bool
	err     error
}

func (f *fakeCommitter) Commit(message string, paths ...string) (bool, error) {
	f.commits = append(f.commits, recordedCommit{message: message, paths: paths})
	return f.result, f.err
}

// assertCommitsThrough checks that evt hands a commit to the committer it was built with.
func assertCommitsThrough(t *testing.T, evt interface {
	Commit(string, ...string) (bool, error)
}, committer *fakeCommitter) {
	t.Helper()
	committed, err := evt.Commit("Update things", "a.php", "b/")
	require.ErrorIs(t, err, committer.err)
	assert.Equal(t, committer.result, committed)
	assert.Equal(t, []recordedCommit{{message: "Update things", paths: []string{"a.php", "b/"}}}, committer.commits)
}

func TestAbortError(t *testing.T) {
	err := AbortError{Msg: "something aborted"}
	assert.Equal(t, "something aborted", err.Error())
}

func TestNewPreComposerUpdateEvent(t *testing.T) {
	ctx := context.Background()
	committer := &fakeCommitter{result: true, err: assert.AnError}
	plan := UpdatePlan{Only: []string{"drupal/core"}, Keep: []Pin{{Package: "keep/me", Version: "1.0.0"}}, MinimalChanges: true}
	evt := NewPreComposerUpdateEvent(ctx, "/repo", committer, plan)

	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assertCommitsThrough(t, evt, committer)
	assert.Equal(t, plan, evt.Plan())
}

func TestPlanIsACopyAHookCannotEdit(t *testing.T) {
	evt := NewPreComposerUpdateEvent(context.Background(), "/repo", nil, UpdatePlan{
		Only: []string{"drupal/core"},
		Keep: []Pin{{Package: "keep/me", Version: "1.0.0"}},
	})

	edited := evt.Plan()
	edited.Only[0] = "drupal/other"
	edited.Keep[0].Version = "2.0.0"

	assert.Equal(t, []string{"drupal/core"}, evt.Plan().Only)
	assert.Equal(t, "1.0.0", evt.Plan().Keep[0].Version)
}

func TestNewPostComposerUpdateEvent(t *testing.T) {
	ctx := context.Background()
	committer := &fakeCommitter{result: true, err: assert.AnError}
	evt := NewPostComposerUpdateEvent(ctx, "/repo", committer)

	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assertCommitsThrough(t, evt, committer)
}

func TestNewPostCodeUpdateEvent(t *testing.T) {
	ctx := context.Background()
	committer := &fakeCommitter{result: true, err: assert.AnError}
	evt := NewPostCodeUpdateEvent(ctx, "/repo", committer)

	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assertCommitsThrough(t, evt, committer)
}

func TestNewPreSiteUpdateEvent(t *testing.T) {
	ctx := context.Background()
	committer := &fakeCommitter{result: true, err: assert.AnError}
	evt := NewPreSiteUpdateEvent(ctx, "/repo", committer, "default", drush.Facts{Modules: []string{"system"}})

	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assertCommitsThrough(t, evt, committer)
	assert.Equal(t, "default", evt.Site())
	assert.Equal(t, drush.Facts{Modules: []string{"system"}}, evt.Facts())
}

func TestNewPostSiteUpdateEvent(t *testing.T) {
	ctx := context.Background()
	committer := &fakeCommitter{result: true, err: assert.AnError}
	evt := NewPostSiteUpdateEvent(ctx, "/repo", committer, "production", drush.Facts{Modules: []string{"system"}})

	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assertCommitsThrough(t, evt, committer)
	assert.Equal(t, "production", evt.Site())
	assert.Equal(t, drush.Facts{Modules: []string{"system"}}, evt.Facts())
}

func TestSiteEventFactsAreCopies(t *testing.T) {
	evt := NewPreSiteUpdateEvent(context.Background(), "/repo", &fakeCommitter{}, "default", drush.Facts{Modules: []string{"system"}})

	evt.Facts().Modules[0] = "changed"

	assert.Equal(t, []string{"system"}, evt.Facts().Modules, "a hook must not change what the next hook reads")
}

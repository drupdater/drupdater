package workflow

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	evt := NewPreComposerUpdateEvent(ctx, "/repo", committer, []string{"drupal/core"}, []string{"keep/me"}, true)

	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assertCommitsThrough(t, evt, committer)
	assert.Equal(t, []string{"drupal/core"}, evt.PackagesToUpdate)
	assert.Equal(t, []string{"keep/me"}, evt.PackagesToKeep)
	assert.True(t, evt.MinimalChanges)
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
	evt := NewPreSiteUpdateEvent(ctx, "/repo", committer, "default")

	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assertCommitsThrough(t, evt, committer)
	assert.Equal(t, "default", evt.Site())
}

func TestNewPostSiteUpdateEvent(t *testing.T) {
	ctx := context.Background()
	committer := &fakeCommitter{result: true, err: assert.AnError}
	evt := NewPostSiteUpdateEvent(ctx, "/repo", committer, "production")

	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assertCommitsThrough(t, evt, committer)
	assert.Equal(t, "production", evt.Site())
}

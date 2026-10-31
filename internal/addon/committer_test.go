package addon

import (
	"context"
	"sync"

	"github.com/drupdater/drupdater/internal/workflow"
)

type recordedCommit struct {
	message string
	paths   []string
}

// fakeCommitter records the commits an addon asks for. It reports a commit made unless unchanged
// or err says otherwise.
type fakeCommitter struct {
	mu        sync.Mutex
	commits   []recordedCommit
	unchanged bool
	err       error
}

func (f *fakeCommitter) Commit(_ context.Context, message string, paths ...string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, recordedCommit{message: message, paths: paths})
	if f.err != nil {
		return false, f.err
	}
	return !f.unchanged, nil
}

// recordRemovals stands in for os.Remove, collecting the files an addon deletes.
func recordRemovals(removed *[]string) func(string) error {
	return func(name string) error {
		*removed = append(*removed, name)
		return nil
	}
}

// firePostCodeUpdate runs addon's post-code-update hook as a run does, its commits going to evt's
// committer, and returns the pipeline holding what the hook found.
func firePostCodeUpdate(a workflow.Addon, evt *workflow.PostCodeUpdateEvent) (*workflow.Pipeline, error) {
	p := workflow.NewPipeline([]workflow.Addon{a})
	return p, workflow.Fire(p, evt)
}

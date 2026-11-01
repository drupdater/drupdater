// Package sdktest fires an addon's hooks through drupdater's own pipeline, in stage order, so an
// addon built outside drupdater is tested the way a run would call it. Commits are recorded, not
// made: the test asserts on what the addon asked for.
package sdktest

import (
	"context"
	"slices"
	"sync"

	"github.com/drupdater/drupdater/internal/mergerequest"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/sdk"
)

// Harness runs addons' hooks against the project in Dir.
type Harness struct {
	Dir      string
	pipeline *workflow.Pipeline
	commits  *recordingCommitter
}

// New builds a harness for addons, which fire in the order given within a stage, as in a run.
func New(dir string, addons ...sdk.Addon) *Harness {
	return &Harness{Dir: dir, pipeline: workflow.NewPipeline(addons), commits: &recordingCommitter{}}
}

// PreComposerUpdate fires pre-composer-update on plan and returns it merged with every proposal.
func (h *Harness) PreComposerUpdate(ctx context.Context, plan sdk.UpdatePlan) (sdk.UpdatePlan, error) {
	return workflow.Settle(h.pipeline, workflow.NewPreComposerUpdateEvent(ctx, h.Dir, h.commits, plan))
}

func (h *Harness) PostComposerUpdate(ctx context.Context) error {
	return workflow.Fire(h.pipeline, workflow.NewPostComposerUpdateEvent(ctx, h.Dir, h.commits))
}

func (h *Harness) PostCodeUpdate(ctx context.Context) error {
	return workflow.Fire(h.pipeline, workflow.NewPostCodeUpdateEvent(ctx, h.Dir, h.commits))
}

func (h *Harness) PreSiteUpdate(ctx context.Context, site string, facts sdk.Facts) error {
	return workflow.Fire(h.pipeline, workflow.NewPreSiteUpdateEvent(ctx, h.Dir, h.commits, site, facts))
}

func (h *Harness) PostSiteUpdate(ctx context.Context, site string, facts sdk.Facts) error {
	return workflow.Fire(h.pipeline, workflow.NewPostSiteUpdateEvent(ctx, h.Dir, h.commits, site, facts))
}

// Commits returns every commit the hooks asked for so far, in the order the pipeline made them.
func (h *Harness) Commits() []sdk.Commit {
	return h.commits.made()
}

// Report returns each reporting addon's section by its key, as the run report would hold it.
func (h *Harness) Report() map[string]any {
	return h.snapshot().Addons
}

// Description renders the merge request description from the sections in Report.
func (h *Harness) Description() (string, error) {
	return mergerequest.DescriptionWith(h.snapshot(), h.pipeline.DescriptionTemplates())
}

func (h *Harness) snapshot() report.Report {
	rec := report.NewRecorder("test", report.ModeNormal, true, "", "", nil)
	h.pipeline.Report(rec)
	return rec.Snapshot()
}

// recordingCommitter reports every commit as made, so a test sees what the addon asked for.
type recordingCommitter struct {
	mu      sync.Mutex
	commits []sdk.Commit
}

func (c *recordingCommitter) Commit(_ context.Context, message string, paths ...string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commits = append(c.commits, sdk.Commit{Message: message, Paths: slices.Clone(paths)})
	return true, nil
}

func (c *recordingCommitter) made() []sdk.Commit {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.commits)
}

// Package sdk is how an addon is built outside drupdater: in its own Go module, compiled into a
// drupdater binary of its own with Main.
//
//	func main() {
//		sdk.Main(sdk.Registration{
//			Name: "acme_cache_tags",
//			New:  func(d sdk.Deps) sdk.Addon { return &CacheTags{logger: d.Logger, drush: d.Drush} },
//		})
//	}
//
// The binary is drupdater itself, with every builtin addon, plus the registered ones; a project
// turns one on by listing its name under run_types.<type>.addons in .drupdater.yaml.
//
// The types here are drupdater's own, re-exported, so an addon is held to the same rules as the
// builtin ones: it hooks into events, returns what it changed and found rather than committing
// or keeping it, and reaches the run report and merge request only through its report section.
// See https://drupdater.github.io/how-to/build-an-external-addon/.
package sdk

import (
	"context"

	"github.com/drupdater/drupdater/cmd"
	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/external"
	"github.com/drupdater/drupdater/internal/workflow"
)

// Main runs drupdater with addons registered next to the builtin ones, and exits. A registration
// that is invalid or shadows another addon's name fails every command before it starts.
func Main(addons ...Registration) {
	cmd.ExecuteWith(addons...)
}

// Run is Main without exiting: it runs the command line args, such as {"addons"}, and returns
// the exit status. A test of the binary's registrations can call it in-process.
func Run(ctx context.Context, args []string, addons ...Registration) int {
	return cmd.RunWith(ctx, args, addons...)
}

type (
	// Registration names an addon for .drupdater.yaml and builds it once per run.
	Registration = external.Registration
	// Deps is what an addon is built with.
	Deps = external.Deps
	// Composer runs composer in the project under the environment drupdater's own calls use.
	Composer = external.Composer
	// Drush runs drush against one site of the project.
	Drush = external.Drush
)

type (
	// Addon hands the workflow its hooks.
	Addon = workflow.Addon
	// Hooks is what an addon runs on each event; a nil field sits that event out.
	Hooks = workflow.Hooks
	// Hook is a handler for the event E, run at its Stage.
	Hook[E any] = workflow.Hook[E]
	// PlanHook is a pre-composer-update hook, which proposes changes to the update.
	PlanHook = workflow.PlanHook
	// Stage orders an event's hooks across addons.
	Stage = workflow.Stage
)

const (
	// Prepare produces what later hooks of the same event read.
	Prepare = workflow.Prepare
	// Change edits the project.
	Change = workflow.Change
	// Format rewrites what Change wrote into the project's style.
	Format = workflow.Format
	// Inspect reads the final state and changes nothing.
	Inspect = workflow.Inspect
)

type (
	PreComposerUpdateEvent  = workflow.PreComposerUpdateEvent
	PostComposerUpdateEvent = workflow.PostComposerUpdateEvent
	PostCodeUpdateEvent     = workflow.PostCodeUpdateEvent
	PreSiteUpdateEvent      = workflow.PreSiteUpdateEvent
	PostSiteUpdateEvent     = workflow.PostSiteUpdateEvent
)

// The events' names, which ReportData passes to Sites and Shared.
const (
	EventPreComposerUpdate  = workflow.EventPreComposerUpdate
	EventPostComposerUpdate = workflow.EventPostComposerUpdate
	EventPostCodeUpdate     = workflow.EventPostCodeUpdate
	EventPreSiteUpdate      = workflow.EventPreSiteUpdate
	EventPostSiteUpdate     = workflow.EventPostSiteUpdate
)

type (
	// Facts is what a site event reports about its site.
	Facts = drush.Facts
	// UpdateHook is one pending update hook in Facts.
	UpdateHook = drush.UpdateHook
	// UnsupportedModule is an installed module with no supported release, in Facts.
	UnsupportedModule = drush.UnsupportedModule
)

type (
	// UpdatePlan is what composer update is asked to do.
	UpdatePlan = workflow.UpdatePlan
	// Pin holds a package at a version in an UpdatePlan.
	Pin = workflow.Pin
	// Commit is one commit a hook asks the pipeline to make.
	Commit = workflow.Commit
	// Result is what a Produce hook returns.
	Result[F any] = workflow.Result[F]
	// Proposal is what a ProduceProposal hook returns.
	Proposal[F any] = workflow.Proposal[F]
	// Findings is what an addon's hooks returned over a run, handed to ReportData.
	Findings = workflow.Findings
	// Recorded is one hook's finding, with the messages of the commits it made.
	Recorded[F any] = workflow.Recorded[F]
	// Reporter is an addon contributing a section to the run report, keyed by its name.
	Reporter = workflow.Reporter
	// Describer is a Reporter that also adds a section to the merge request description.
	Describer = workflow.Describer
	// AbortError stops the run as having nothing to do rather than as failed.
	AbortError = workflow.AbortError
)

// On builds a hook that only acts on the project: it commits nothing and finds nothing.
func On[E any](stage Stage, fn func(E) error) *Hook[E] {
	return workflow.On(stage, fn)
}

// Produce builds a hook whose Result's commits the pipeline makes and whose finding it records.
func Produce[E, F any](stage Stage, fn func(E) (Result[F], error)) *Hook[E] {
	return workflow.Produce(stage, fn)
}

// ProduceFrom is Produce for a hook that reads what its own addon's earlier hooks found.
func ProduceFrom[E, F any](stage Stage, fn func(E, *Findings) (Result[F], error)) *Hook[E] {
	return workflow.ProduceFrom(stage, fn)
}

// Observe builds an Inspect hook that only finds something and changes nothing.
func Observe[E, F any](fn func(E) (F, error)) *Hook[E] {
	return workflow.Observe(fn)
}

// ProduceProposal builds the pre-composer-update hook, which returns a plan proposal.
func ProduceProposal[F any](stage Stage, fn func(*PreComposerUpdateEvent) (Proposal[F], error)) *PlanHook {
	return workflow.ProduceProposal(stage, fn)
}

// Sites returns every site's finding for a per-site event. It panics when one is not an F.
func Sites[F any](f *Findings, event string) map[string]Recorded[F] {
	return workflow.Sites[F](f, event)
}

// SiteFindings returns just the findings of Sites.
func SiteFindings[F any](f *Findings, event string) map[string]F {
	return workflow.SiteFindings[F](f, event)
}

// Shared returns the finding of an event not per site, and whether its hook recorded one.
func Shared[F any](f *Findings, event string) (Recorded[F], bool) {
	return workflow.Shared[F](f, event)
}

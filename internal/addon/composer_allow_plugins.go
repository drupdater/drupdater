package addon

import (
	"fmt"

	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

type ComposerAllowPlugins struct {
	logger   *zap.Logger
	composer Composer

	allowPlugins    map[string]bool
	newAllowPlugins []string
}

func NewComposerAllowPlugins(logger *zap.Logger, composer Composer) *ComposerAllowPlugins {
	return &ComposerAllowPlugins{
		logger:   logger,
		composer: composer,
	}
}

func (ap *ComposerAllowPlugins) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PreComposerUpdate:  workflow.On(workflow.Change, ap.preComposerUpdateHandler),
		PostComposerUpdate: workflow.On(workflow.Change, ap.postComposerUpdateHandler),
	}
}

func (ap *ComposerAllowPlugins) RenderTemplate() (string, error) {
	if len(ap.newAllowPlugins) == 0 {
		return "", nil
	}
	return render("allowplugins.go.tmpl", struct {
		NewAllowPlugins []string
	}{
		NewAllowPlugins: ap.newAllowPlugins,
	})
}

func (ap *ComposerAllowPlugins) preComposerUpdateHandler(evt *workflow.PreComposerUpdateEvent) error {

	var err error
	ap.allowPlugins, err = ap.composer.GetAllowPlugins(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to get composer allow plugins: %w", err)
	}

	return ap.composer.SetConfig(evt.Context(), evt.Path(), "allow-plugins", "true")
}

func (ap *ComposerAllowPlugins) postComposerUpdateHandler(evt *workflow.PostComposerUpdateEvent) error {

	allPlugins, err := ap.composer.GetInstalledPlugins(evt.Context(), evt.Path())
	if err != nil {
		return err
	}

	// Written to below, so it must never be nil: assigning into a nil map panics.
	if ap.allowPlugins == nil {
		ap.allowPlugins = map[string]bool{}
	}

	for key := range allPlugins {
		if _, ok := ap.allowPlugins[key]; !ok {
			ap.allowPlugins[key] = false
			ap.newAllowPlugins = append(ap.newAllowPlugins, key)
		}
	}
	if err := ap.composer.SetAllowPlugins(evt.Context(), evt.Path(), ap.allowPlugins); err != nil {
		return err
	}

	return nil
}

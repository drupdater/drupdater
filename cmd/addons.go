package cmd

import (
	"context"
	"fmt"
	"slices"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// addonCatalog is the agent's catalogue: every addon by the name .drupdater.yaml uses, in the
// order hooks sharing a stage run in.
type addonCatalog struct {
	addons []workflow.Addon
	// mandatory addons run on every run; defaults are what a normal run gets when it names none.
	mandatory []string
	defaults  []string
}

// agentAddonCatalog asks the agent installed in Composer for its catalogue.
func agentAddonCatalog(ctx context.Context, logger *zap.Logger) (addonCatalog, error) {
	addons, err := composer.NewCLI(logger).Addons(ctx)
	if err != nil {
		return addonCatalog{}, err
	}
	return newAddonCatalog(addons)
}

// newAddonCatalog refuses a catalogue the pipeline could not run: an unknown event or stage, or
// two addons of one name.
func newAddonCatalog(addons []agent.Addon) (addonCatalog, error) {
	var c addonCatalog
	for _, a := range addons {
		if slices.ContainsFunc(c.addons, func(known workflow.Addon) bool { return known.Name == a.Name }) {
			return addonCatalog{}, fmt.Errorf("the agent's catalogue lists %q twice", a.Name)
		}
		hooks := make(map[string]workflow.Stage, len(a.Hooks))
		for event, stageName := range a.Hooks {
			if !slices.Contains(workflow.Events, event) {
				return addonCatalog{}, fmt.Errorf("addon %q hooks the unknown event %q", a.Name, event)
			}
			stage, err := workflow.ParseStage(stageName)
			if err != nil {
				return addonCatalog{}, fmt.Errorf("addon %q hooks %s: %w", a.Name, event, err)
			}
			hooks[event] = stage
		}
		c.addons = append(c.addons, workflow.Addon{Name: a.Name, Hooks: hooks})
		switch {
		case a.Mandatory:
			c.mandatory = append(c.mandatory, a.Name)
		case a.Default:
			c.defaults = append(c.defaults, a.Name)
		}
	}
	return c, nil
}

// create returns the mandatory addons, then the ones the active run type configures. A normal run
// that configures none gets the catalogue's defaults; a security run stays a focused fix.
func (c addonCatalog) create(cfg config.Config) ([]workflow.Addon, error) {
	configured := cfg.ActiveRunType().Addons
	if configured == nil && !cfg.Security {
		configured = c.defaults
	}

	var addons []workflow.Addon
	for _, name := range slices.Concat(c.mandatory, configured) {
		if slices.ContainsFunc(addons, func(a workflow.Addon) bool { return a.Name == name }) {
			continue
		}
		i := slices.IndexFunc(c.addons, func(a workflow.Addon) bool { return a.Name == name })
		if i < 0 {
			return nil, fmt.Errorf("unknown addon %q", name)
		}
		addons = append(addons, c.addons[i])
	}
	return addons, nil
}

// validate checks both run types, so a typo under run_types.security fails a normal run too.
func (c addonCatalog) validate(cfg config.Config) error {
	for _, name := range slices.Concat(cfg.RunTypes.Normal.Addons, cfg.RunTypes.Security.Addons) {
		if !slices.ContainsFunc(c.addons, func(a workflow.Addon) bool { return a.Name == name }) {
			return fmt.Errorf("unknown addon %q (run \"drupdater addons\" to list valid names)", name)
		}
	}
	return nil
}

// configurable returns the sorted addon names, minus the mandatory ones.
func (c addonCatalog) configurable() []string {
	var names []string
	for _, a := range c.addons {
		if !slices.Contains(c.mandatory, a.Name) {
			names = append(names, a.Name)
		}
	}
	slices.Sort(names)
	return names
}

// newAddonsCmd lists the addon names that can be set in .drupdater.yaml.
func (a app) newAddonsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "addons",
		Short: "List the addon names that can be set in .drupdater.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			catalog, err := a.addonCatalog(cmd.Context(), zap.NewNop())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Addons you can set under run_types.normal.addons / run_types.security.addons in .drupdater.yaml")
			fmt.Fprintln(out, "(a normal run that sets none gets the ones marked default):")
			for _, n := range catalog.configurable() {
				if slices.Contains(catalog.defaults, n) {
					fmt.Fprintf(out, "  %s (default)\n", n)
					continue
				}
				fmt.Fprintf(out, "  %s\n", n)
			}
			return nil
		},
	}
}

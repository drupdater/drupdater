package cmd

import (
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"

	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/phpcs"
	"github.com/drupdater/drupdater/pkg/rector"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// addonComposer is every builtin addon's composer interface at once; each addon sees only its own.
type addonComposer interface {
	addon.AllowPluginsComposer
	addon.AuditComposer
	addon.BeautifierComposer
	addon.DeprecationsComposer
	addon.DiffComposer
	addon.NormalizerComposer
	addon.PatchesComposer
}

// addonDrush is every builtin addon's drush interface at once.
type addonDrush interface {
	addon.TranslationsDrush
}

// addonDeps carries everything the addon constructors need.
type addonDeps struct {
	logger    *zap.Logger
	drush     addonDrush
	composer  addonComposer
	drupalOrg addon.DrupalOrg
	// drupalCode is nil without DRUPALCODE_ACCESS_TOKEN.
	drupalCode addon.DrupalCode
	// security marks a --security run. Only composer_audit reads it.
	security bool
	// rectorConfig is drupdater's Rector config, extracted from the binary.
	rectorConfig string
}

// newAddonDeps adds the drupalcode client only when DRUPALCODE_ACCESS_TOKEN is set.
func newAddonDeps(logger *zap.Logger, drush addonDrush, composer addonComposer, drupalOrg addon.DrupalOrg, rectorConfig string) addonDeps {
	deps := addonDeps{logger: logger, drush: drush, composer: composer, drupalOrg: drupalOrg, rectorConfig: rectorConfig}
	if token := os.Getenv("DRUPALCODE_ACCESS_TOKEN"); token != "" {
		deps.drupalCode = drupalorg.NewDrupalCode(token)
	}
	return deps
}

// bundledToolsBin is where the image installs tools/, so addons install nothing into the project.
const bundledToolsBin = "/opt/drupdater/tools/vendor/bin"

// addonCatalog is every addon by the name .drupdater.yaml uses, and which of them always run.
type addonCatalog struct {
	factories map[string]func(addonDeps) workflow.Addon
	mandatory []string
}

// builtinAddons is the catalog drupdater ships. A function, so nothing can change the one in use.
func builtinAddons() addonCatalog {
	return addonCatalog{
		factories: map[string]func(addonDeps) workflow.Addon{
			"composer_audit": func(d addonDeps) workflow.Addon {
				return addon.NewComposerAudit(d.logger, d.composer, d.security)
			},
			"code_beautifier": func(d addonDeps) workflow.Addon {
				return addon.NewCodeBeautifier(d.logger, phpcs.NewCLI(d.logger), phpcs.NewBundledCLI(d.logger, bundledToolsBin), d.composer)
			},
			"deprecations_remover": func(d addonDeps) workflow.Addon {
				return addon.NewDeprecationsRemover(d.logger, rector.NewCLI(d.logger, d.rectorConfig), rector.NewBundledCLI(d.logger, bundledToolsBin, d.rectorConfig), d.composer)
			},
			"translations_updater":   func(d addonDeps) workflow.Addon { return addon.NewTranslationsUpdater(d.logger, d.drush) },
			"composer_allow_plugins": func(d addonDeps) workflow.Addon { return addon.NewComposerAllowPlugins(d.logger, d.composer) },
			"composer_normalizer":    func(d addonDeps) workflow.Addon { return addon.NewComposerNormalizer(d.logger, d.composer) },
			"composer_patches": func(d addonDeps) workflow.Addon {
				return addon.NewComposerPatches1(d.logger, d.composer, d.drupalOrg, d.drupalCode, http.DefaultClient)
			},
			"composer_diff":       func(d addonDeps) workflow.Addon { return addon.NewComposerDiff(d.logger, d.composer) },
			"update_hooks":        func(d addonDeps) workflow.Addon { return addon.NewUpdateHooks(d.logger) },
			"unsupported_modules": func(d addonDeps) workflow.Addon { return addon.NewUnsupportedModules(d.logger) },
		},
		// The description joins composer_audit's and unsupported_modules' findings into one
		// end-of-life list, so both always run.
		mandatory: []string{
			"composer_allow_plugins",
			"composer_patches",
			"composer_diff",
			"update_hooks",
			"composer_audit",
			"unsupported_modules",
		},
	}
}

// create builds the mandatory addons plus the ones the active run type lists.
func (c addonCatalog) create(cfg config.Config, deps addonDeps) ([]workflow.Addon, error) {
	deps.security = cfg.Security

	var addons []workflow.Addon
	added := map[string]bool{}
	build := func(name string) error {
		if added[name] {
			return nil
		}
		factory, ok := c.factories[name]
		if !ok {
			return fmt.Errorf("unknown addon %q", name)
		}
		addons = append(addons, factory(deps))
		added[name] = true
		return nil
	}

	for _, name := range slices.Concat(c.mandatory, cfg.ActiveRunType().Addons) {
		if err := build(name); err != nil {
			return nil, err
		}
	}
	return addons, nil
}

// validate checks both run types, so a typo under run_types.security fails a normal run too.
func (c addonCatalog) validate(cfg config.Config) error {
	for _, name := range slices.Concat(cfg.RunTypes.Normal.Addons, cfg.RunTypes.Security.Addons) {
		if _, ok := c.factories[name]; !ok {
			return fmt.Errorf("unknown addon %q (run \"drupdater addons\" to list valid names)", name)
		}
	}
	return nil
}

// configurable returns the sorted addon names, minus the mandatory ones.
func (c addonCatalog) configurable() []string {
	names := slices.Sorted(maps.Keys(c.factories))
	return slices.DeleteFunc(names, func(n string) bool { return slices.Contains(c.mandatory, n) })
}

// newAddonsCmd lists the addon names that can be set in .drupdater.yaml.
func (a app) newAddonsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "addons",
		Short: "List the addon names that can be set in .drupdater.yaml",
		Run: func(cmd *cobra.Command, _ []string) {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Addons you can set under run_types.normal.addons / run_types.security.addons in .drupdater.yaml:")
			for _, n := range a.addons.configurable() {
				fmt.Fprintf(out, "  %s\n", n)
			}
		},
	}
}

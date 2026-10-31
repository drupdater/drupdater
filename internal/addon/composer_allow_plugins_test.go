package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewDefaultAllowPlugins(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockAllowPluginsComposer(t)

	ap := NewComposerAllowPlugins(logger, composerRunner)

	assert.Equal(t, logger, ap.logger)
	assert.Equal(t, composerRunner, ap.composer)
}

// preUpdate runs the pre-update hook and returns its finding as the post-update hook reads it.
func preUpdate(t *testing.T, ap *ComposerAllowPlugins) (*workflow.Findings, error) {
	t.Helper()
	proposal, err := ap.preComposerUpdateHandler(workflow.NewPreComposerUpdateEvent(t.Context(), "/p", nil, workflow.UpdatePlan{}))
	assert.Zero(t, proposal.Plan, "allowing plugins never changes what the update touches")
	return earlierAllowPlugins(proposal.Finding), err
}

func earlierAllowPlugins(before allowPluginsBefore) *workflow.Findings {
	earlier := &workflow.Findings{}
	earlier.Record(workflow.EventPreComposerUpdate, "", before, nil)
	return earlier
}

// postUpdate runs the post-update hook after earlier and returns the plugins it denied.
func postUpdate(t *testing.T, ap *ComposerAllowPlugins, earlier *workflow.Findings) ([]string, error) {
	t.Helper()
	result, err := ap.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/p", nil), earlier)
	return result.Finding, err
}

func plugins(names ...string) map[string]any {
	installed := map[string]any{}
	for _, name := range names {
		installed[name] = nil
	}
	return installed
}

// The project's rules stay in place and keep deciding every plugin they name; only plugins none
// of them matches are denied afterwards, in name order.
func TestAllowPluginsWithRulesAddsAndRemovesATrailingWildcard(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	original := composer.AllowPlugins{Set: true, Rules: map[string]bool{"drupal/*": true, "cweagans/composer-patches": false}}
	c.EXPECT().GetAllowPlugins("/p").Return(original, nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.*", "true").Return(nil).Once()
	ap := NewComposerAllowPlugins(zap.NewNop(), c)

	earlier, err := preUpdate(t, ap)
	require.NoError(t, err)

	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil).Once()
	c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(
		"tbachert/spi", "drupal/core-composer-scaffold", "cweagans/composer-patches", "php-http/discovery",
	), nil)
	var denied []string
	c.EXPECT().SetConfig(anyCtx, "/p", mock.Anything, "false").RunAndReturn(func(_ context.Context, _, key, _ string) error {
		denied = append(denied, key)
		return nil
	})

	added, err := postUpdate(t, ap, earlier)
	require.NoError(t, err)
	assert.Equal(t, []string{"allow-plugins.php-http/discovery", "allow-plugins.tbachert/spi"}, denied)
	assert.Equal(t, []string{"php-http/discovery", "tbachert/spi"}, added)
}

// A boolean decides every plugin already, and the project's own "*" rule is the project's call.
func TestAllowPluginsLeavesASettingThatDecidesEverythingAlone(t *testing.T) {
	yes, no := true, false
	for name, original := range map[string]composer.AllowPlugins{
		"allow all": {Set: true, All: &yes},
		"deny all":  {Set: true, All: &no},
		"own *":     {Set: true, Rules: map[string]bool{"drupal/*": true, "*": false}},
	} {
		t.Run(name, func(t *testing.T) {
			c := NewMockAllowPluginsComposer(t)
			c.EXPECT().GetAllowPlugins("/p").Return(original, nil)
			c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins("drupal/core-composer-scaffold", "tbachert/spi"), nil)
			ap := NewComposerAllowPlugins(zap.NewNop(), c)

			earlier, err := preUpdate(t, ap)
			require.NoError(t, err)
			added, err := postUpdate(t, ap, earlier)
			require.NoError(t, err)

			assert.Empty(t, added)
		})
	}
}

// With no setting, the wildcard creates one; it goes again unless a new plugin needs an entry.
func TestAllowPluginsWithoutASettingRestoresItsAbsence(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{}, nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.*", "true").Return(nil)
	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil)
	c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(), nil)
	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins").Return(nil)
	ap := NewComposerAllowPlugins(zap.NewNop(), c)

	earlier, err := preUpdate(t, ap)
	require.NoError(t, err)
	_, err = postUpdate(t, ap, earlier)
	require.NoError(t, err)
}

func TestAllowPluginsWithoutASettingKeepsTheNewDenials(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{}, nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.*", "true").Return(nil)
	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil)
	c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins("tbachert/spi"), nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.tbachert/spi", "false").Return(nil)
	ap := NewComposerAllowPlugins(zap.NewNop(), c)

	earlier, err := preUpdate(t, ap)
	require.NoError(t, err)
	added, err := postUpdate(t, ap, earlier)
	require.NoError(t, err)
	assert.Equal(t, []string{"tbachert/spi"}, added)
}

// An empty object is a setting: it is left as {} rather than removed.
func TestAllowPluginsKeepsAnEmptyObject(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{Set: true, Rules: map[string]bool{}}, nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.*", "true").Return(nil)
	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil)
	c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(), nil)
	ap := NewComposerAllowPlugins(zap.NewNop(), c)

	earlier, err := preUpdate(t, ap)
	require.NoError(t, err)
	_, err = postUpdate(t, ap, earlier)
	require.NoError(t, err)
}

func TestAllowPluginsErrors(t *testing.T) {
	t.Run("reading the setting fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{}, assert.AnError)

		_, err := preUpdate(t, NewComposerAllowPlugins(zap.NewNop(), c))

		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to get composer allow plugins")
	})

	t.Run("adding the wildcard fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{}, nil)
		c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.*", "true").Return(assert.AnError)
		ap := NewComposerAllowPlugins(zap.NewNop(), c)

		proposal, err := ap.preComposerUpdateHandler(workflow.NewPreComposerUpdateEvent(t.Context(), "/p", nil, workflow.UpdatePlan{}))

		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to allow plugins for the update")
		assert.False(t, proposal.Finding.wildcard, "a wildcard that was never written is not removed later")
	})

	t.Run("removing the wildcard fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(), nil)
		c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(assert.AnError)
		ap := NewComposerAllowPlugins(zap.NewNop(), c)

		_, err := postUpdate(t, ap, earlierAllowPlugins(allowPluginsBefore{wildcard: true}))

		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to remove the temporary plugin rule")
	})

	t.Run("an absent setting drupdater never touched stays absent", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(), nil)

		_, err := postUpdate(t, NewComposerAllowPlugins(zap.NewNop(), c), &workflow.Findings{})
		require.NoError(t, err)
	})

	t.Run("listing installed plugins fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(nil, assert.AnError)

		_, err := postUpdate(t, NewComposerAllowPlugins(zap.NewNop(), c), &workflow.Findings{})
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("denying a new plugin fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins("tbachert/spi"), nil)
		c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.tbachert/spi", "false").Return(assert.AnError)

		_, err := postUpdate(t, NewComposerAllowPlugins(zap.NewNop(), c), &workflow.Findings{})

		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to deny new plugin tbachert/spi")
	})

	t.Run("removing the emptied setting fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(), nil)
		c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins").Return(assert.AnError)
		ap := NewComposerAllowPlugins(zap.NewNop(), c)

		_, err := postUpdate(t, ap, earlierAllowPlugins(allowPluginsBefore{wildcard: true}))

		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to remove the temporary plugin config")
	})
}

// Composer refuses every command while an installed plugin is undecided, so the denials have to
// be in place before the wildcard that was deciding for them goes.
func TestAllowPluginsRemovesTheWildcardLast(t *testing.T) {
	var calls []string
	record := func(call string) { calls = append(calls, call) }
	c := NewMockAllowPluginsComposer(t)
	c.EXPECT().GetInstalledPlugins(anyCtx, "/p").RunAndReturn(func(context.Context, string) (map[string]any, error) {
		record("list")
		return plugins("tbachert/spi"), nil
	})
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.tbachert/spi", "false").RunAndReturn(func(context.Context, string, string, string) error {
		record("deny")
		return nil
	})
	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").RunAndReturn(func(context.Context, string, string) error {
		record("unset wildcard")
		return nil
	})
	ap := NewComposerAllowPlugins(zap.NewNop(), c)
	earlier := earlierAllowPlugins(allowPluginsBefore{original: composer.AllowPlugins{Set: true, Rules: map[string]bool{}}, wildcard: true})

	_, err := postUpdate(t, ap, earlier)
	require.NoError(t, err)
	assert.Equal(t, []string{"list", "deny", "unset wildcard"}, calls)
}

// The post-update hook undoes what its own pre-update hook did, read from the pipeline's findings.
func TestAllowPluginsUndoesItsWildcardThroughThePipeline(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{Set: true, Rules: map[string]bool{"drupal/*": true}}, nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.*", "true").Return(nil)
	c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins("drupal/core-composer-scaffold", "tbachert/spi"), nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.tbachert/spi", "false").Return(nil)
	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil)
	p := workflow.NewPipeline([]workflow.Addon{NewComposerAllowPlugins(zap.NewNop(), c)})

	_, err := workflow.Settle(p, workflow.NewPreComposerUpdateEvent(t.Context(), "/p", nil, workflow.UpdatePlan{}))
	require.NoError(t, err)
	require.NoError(t, workflow.Fire(p, workflow.NewPostComposerUpdateEvent(t.Context(), "/p", nil)))

	assert.Equal(t, PluginAllowlist{Added: []string{"tbachert/spi"}}, reportSection(p, "composer_allow_plugins"))
}

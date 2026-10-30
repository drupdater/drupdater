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

func preUpdate(t *testing.T, ap *ComposerAllowPlugins) error {
	t.Helper()
	plan, err := ap.preComposerUpdateHandler(workflow.NewPreComposerUpdateEvent(t.Context(), "/p", nil, workflow.UpdatePlan{}))
	assert.Zero(t, plan, "allowing plugins never changes what the update touches")
	return err
}

func postUpdate(t *testing.T, ap *ComposerAllowPlugins) error {
	t.Helper()
	return ap.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/p", nil))
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

	require.NoError(t, preUpdate(t, ap))

	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil).Once()
	c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(
		"tbachert/spi", "drupal/core-composer-scaffold", "cweagans/composer-patches", "php-http/discovery",
	), nil)
	var denied []string
	c.EXPECT().SetConfig(anyCtx, "/p", mock.Anything, "false").RunAndReturn(func(_ context.Context, _, key, _ string) error {
		denied = append(denied, key)
		return nil
	})

	require.NoError(t, postUpdate(t, ap))
	assert.Equal(t, []string{"allow-plugins.php-http/discovery", "allow-plugins.tbachert/spi"}, denied)
	assert.Equal(t, []string{"php-http/discovery", "tbachert/spi"}, ap.newAllowPlugins)
	assert.False(t, ap.wildcard)
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

			require.NoError(t, preUpdate(t, ap))
			require.NoError(t, postUpdate(t, ap))

			assert.Empty(t, ap.newAllowPlugins)
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

	require.NoError(t, preUpdate(t, ap))
	require.NoError(t, postUpdate(t, ap))
}

func TestAllowPluginsWithoutASettingKeepsTheNewDenials(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{}, nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.*", "true").Return(nil)
	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil)
	c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins("tbachert/spi"), nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.tbachert/spi", "false").Return(nil)
	ap := NewComposerAllowPlugins(zap.NewNop(), c)

	require.NoError(t, preUpdate(t, ap))
	require.NoError(t, postUpdate(t, ap))
	assert.Equal(t, []string{"tbachert/spi"}, ap.newAllowPlugins)
}

// An empty object is a setting: it is left as {} rather than removed.
func TestAllowPluginsKeepsAnEmptyObject(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{Set: true, Rules: map[string]bool{}}, nil)
	c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.*", "true").Return(nil)
	c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil)
	c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(), nil)
	ap := NewComposerAllowPlugins(zap.NewNop(), c)

	require.NoError(t, preUpdate(t, ap))
	require.NoError(t, postUpdate(t, ap))
}

func TestAllowPluginsErrors(t *testing.T) {
	t.Run("reading the setting fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{}, assert.AnError)

		err := preUpdate(t, NewComposerAllowPlugins(zap.NewNop(), c))

		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to get composer allow plugins")
	})

	t.Run("adding the wildcard fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetAllowPlugins("/p").Return(composer.AllowPlugins{}, nil)
		c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.*", "true").Return(assert.AnError)
		ap := NewComposerAllowPlugins(zap.NewNop(), c)

		err := preUpdate(t, ap)

		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to allow plugins for the update")
		assert.False(t, ap.wildcard, "a wildcard that was never written is not removed later")
	})

	t.Run("removing the wildcard fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(), nil)
		c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(assert.AnError)
		ap := NewComposerAllowPlugins(zap.NewNop(), c)
		ap.wildcard = true

		err := postUpdate(t, ap)

		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to remove the temporary plugin rule")
	})

	t.Run("an absent setting drupdater never touched stays absent", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(), nil)

		require.NoError(t, postUpdate(t, NewComposerAllowPlugins(zap.NewNop(), c)))
	})

	t.Run("listing installed plugins fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(nil, assert.AnError)

		require.ErrorIs(t, postUpdate(t, NewComposerAllowPlugins(zap.NewNop(), c)), assert.AnError)
	})

	t.Run("denying a new plugin fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins("tbachert/spi"), nil)
		c.EXPECT().SetConfig(anyCtx, "/p", "allow-plugins.tbachert/spi", "false").Return(assert.AnError)

		err := postUpdate(t, NewComposerAllowPlugins(zap.NewNop(), c))

		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to deny new plugin tbachert/spi")
	})

	t.Run("removing the emptied setting fails", func(t *testing.T) {
		c := NewMockAllowPluginsComposer(t)
		c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins.*").Return(nil)
		c.EXPECT().GetInstalledPlugins(anyCtx, "/p").Return(plugins(), nil)
		c.EXPECT().UnsetConfig(anyCtx, "/p", "allow-plugins").Return(assert.AnError)
		ap := NewComposerAllowPlugins(zap.NewNop(), c)
		ap.wildcard = true

		err := postUpdate(t, ap)

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
	ap.original = composer.AllowPlugins{Set: true, Rules: map[string]bool{}}
	ap.wildcard = true

	require.NoError(t, postUpdate(t, ap))
	assert.Equal(t, []string{"list", "deny", "unset wildcard"}, calls)
}

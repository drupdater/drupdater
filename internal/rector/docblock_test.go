package rector

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// What Rector 2.6 writes for demo.module once a rule imports a class.
const misplacedDocBlock = `<?php

use Drupal\Core\Render\RendererInterface;
use Drupal\Component\Utility\Image;

/**
 * @file
 * Demo module.
 */

/**
 * Renders a build array on its own.
 */
function demo_isolated(RendererInterface $renderer, array $build) {
  return $renderer->renderInIsolation($build);
}
`

const restoredDocBlock = `<?php

/**
 * @file
 * Demo module.
 */

use Drupal\Core\Render\RendererInterface;
use Drupal\Component\Utility\Image;

/**
 * Renders a build array on its own.
 */
function demo_isolated(RendererInterface $renderer, array $build) {
  return $renderer->renderInIsolation($build);
}
`

func TestFileDocBlockFirstMovesTheDocBlockAboveTheImports(t *testing.T) {
	assert.Equal(t, restoredDocBlock, fileDocBlockFirst(misplacedDocBlock))
}

func TestFileDocBlockFirstIsIdempotent(t *testing.T) {
	assert.Equal(t, restoredDocBlock, fileDocBlockFirst(restoredDocBlock))
}

func TestFileDocBlockFirstKeepsWindowsLineEndings(t *testing.T) {
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

	assert.Equal(t, crlf(restoredDocBlock), fileDocBlockFirst(crlf(misplacedDocBlock)))
}

func TestFileDocBlockFirstLeavesOtherFilesAlone(t *testing.T) {
	for name, src := range map[string]string{
		"namespaced class":                        "<?php\n\nnamespace Drupal\\demo;\n\nuse Drupal\\Core\\Url;\n\n/**\n * A class.\n */\nclass Demo {}\n",
		"imports followed by a function docblock": "<?php\n\nuse Drupal\\Core\\Url;\n\n/**\n * Renders.\n */\nfunction demo() {}\n",
		"no imports":                              "<?php\n\n/**\n * @file\n * Demo.\n */\n\nfunction demo() {}\n",
		"imports followed by code":                "<?php\n\nuse Drupal\\Core\\Url;\n\nfunction demo() {}\n",
		"imports and nothing else":                "<?php\n\nuse Drupal\\Core\\Url;\n",
		"an unterminated docblock":                "<?php\n\nuse Drupal\\Core\\Url;\n\n/**\n * @file\n",
		"no opening tag":                          "use Drupal\\Core\\Url;\n\n/**\n * @file\n */\n",
		"opening tag with code after it":          "<?php echo 1;\nuse Drupal\\Core\\Url;\n\n/**\n * @file\n */\n",
		"empty":                                   "",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, src, fileDocBlockFirst(src))
		})
	}
}

func TestRestoreFileDocBlockErrors(t *testing.T) {
	t.Run("a file rector named is gone", func(t *testing.T) {
		cli := &CLI{logger: zap.NewNop(), fs: afero.NewMemMapFs()}

		require.ErrorContains(t, cli.restoreFileDocBlock("/p", "demo.module"), "failed to read /p/demo.module after rector")
	})

	t.Run("the file cannot be written", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/p/demo.module", []byte(misplacedDocBlock), 0o644))
		cli := &CLI{logger: zap.NewNop(), fs: afero.NewReadOnlyFs(fs)}

		require.ErrorContains(t, cli.restoreFileDocBlock("/p", "demo.module"), "failed to restore the @file docblock in /p/demo.module")
	})

	t.Run("an absolute path is used as is", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/abs/demo.module", []byte(misplacedDocBlock), 0o644))
		cli := &CLI{logger: zap.NewNop(), fs: fs}

		require.NoError(t, cli.restoreFileDocBlock("/p", "/abs/demo.module"))
		fixed, err := afero.ReadFile(fs, "/abs/demo.module")
		require.NoError(t, err)
		assert.Equal(t, restoredDocBlock, string(fixed))
	})
}

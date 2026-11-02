<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Tools;

use Drupdater\Agent\Tools\FileDocBlock;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class FileDocBlockTest extends TestCase
{
    /** What Rector 2.6 writes for demo.module once a rule imports a class. */
    private const MISPLACED = <<<'PHP'
        <?php

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

        PHP;

    private const RESTORED = <<<'PHP'
        <?php

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

        PHP;

    public function testMovesTheDocBlockAboveTheImports(): void
    {
        self::assertSame(self::RESTORED, FileDocBlock::first(self::MISPLACED));
    }

    public function testIsIdempotent(): void
    {
        self::assertSame(self::RESTORED, FileDocBlock::first(self::RESTORED));
    }

    public function testKeepsWindowsLineEndings(): void
    {
        $crlf = static fn (string $s): string => str_replace("\n", "\r\n", $s);

        self::assertSame($crlf(self::RESTORED), FileDocBlock::first($crlf(self::MISPLACED)));
    }

    /**
     * @return iterable<string, array{string}>
     */
    public static function otherFiles(): iterable
    {
        yield 'namespaced class' => ["<?php\n\nnamespace Drupal\\demo;\n\nuse Drupal\\Core\\Url;\n\n/**\n * A class.\n */\nclass Demo {}\n"];
        yield 'imports followed by a function docblock' => ["<?php\n\nuse Drupal\\Core\\Url;\n\n/**\n * Renders.\n */\nfunction demo() {}\n"];
        yield 'no imports' => ["<?php\n\n/**\n * @file\n * Demo.\n */\n\nfunction demo() {}\n"];
        yield 'imports followed by code' => ["<?php\n\nuse Drupal\\Core\\Url;\n\nfunction demo() {}\n"];
        yield 'imports and nothing else' => ["<?php\n\nuse Drupal\\Core\\Url;\n"];
        yield 'an unterminated docblock' => ["<?php\n\nuse Drupal\\Core\\Url;\n\n/**\n * @file\n"];
        yield 'no opening tag' => ["use Drupal\\Core\\Url;\n\n/**\n * @file\n */\n"];
        yield 'opening tag with code after it' => ["<?php echo 1;\nuse Drupal\\Core\\Url;\n\n/**\n * @file\n */\n"];
        yield 'empty' => [''];
    }

    #[DataProvider('otherFiles')]
    public function testLeavesOtherFilesAlone(string $src): void
    {
        self::assertSame($src, FileDocBlock::first($src));
    }
}

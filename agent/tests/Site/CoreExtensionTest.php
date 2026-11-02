<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Site;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Site\CoreExtension;
use Drupdater\Agent\Tests\Fails;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class CoreExtensionTest extends TestCase
{
    use Fails;
    use Scratch;

    private const EXPORTED = <<<'YAML'
        _core:
          default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
        module:
          node: 0
          standard: 1000
        theme:
          stark: 0
        profile: standard

        YAML;

    /**
     * @return iterable<string, array{string, bool}>
     */
    public static function sqliteEntries(): iterable
    {
        yield 'enabled at weight 0' => ["module:\n  node: 0\n  sqlite: 0\ntheme:\n  stark: 0\n", true];
        yield 'missing' => ["module:\n  node: 0\ntheme:\n  stark: 0\n", false];
        yield 'at another weight' => ["module:\n  sqlite: 1\n", false];
        yield 'a theme of that name' => ["module:\n  node: 0\ntheme:\n  sqlite: 0\n", false];
        yield 'a module with the name as a prefix' => ["module:\n  sqlite_extra: 0\n", false];
    }

    #[DataProvider('sqliteEntries')]
    public function testSqliteCountsAsEnabledOnlyAsAModuleAtWeightZero(string $yaml, bool $enabled): void
    {
        self::assertSame($enabled, CoreExtension::read($this->file($yaml))->sqliteEnabled());
    }

    public function testSqliteIsAddedAtTheEndOfTheModulesAtTheirIndentation(): void
    {
        $path = $this->file("module:\n    node: 0\n    system: 0\ntheme:\n    stark: 0\n");

        CoreExtension::read($path)->withSqlite()->write($path);

        self::assertSame("module:\n    node: 0\n    system: 0\n    sqlite: 0\ntheme:\n    stark: 0\n", file_get_contents($path));
    }

    public function testSqliteIsAddedToAnEmptyModuleSection(): void
    {
        $path = $this->file("module:\ntheme:\n  stark: 0\n");

        CoreExtension::read($path)->withSqlite()->write($path);

        self::assertSame("module:\n  sqlite: 0\ntheme:\n  stark: 0\n", file_get_contents($path));
        self::assertTrue(CoreExtension::read($path)->sqliteEnabled());
    }

    public function testAddingSqliteTwiceAddsItOnce(): void
    {
        $path = $this->file(self::EXPORTED);

        CoreExtension::read($path)->withSqlite()->withSqlite()->write($path);

        self::assertSame(1, substr_count((string) file_get_contents($path), 'sqlite: 0'));
    }

    public function testTheProfileGoesAsProfileAndAsModuleLeavingEverythingElse(): void
    {
        $path = $this->file(self::EXPORTED . "  standards: 0\n");

        CoreExtension::read($path)->withoutProfile('standard')->write($path);

        self::assertSame(<<<'YAML'
            _core:
              default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
            module:
              node: 0
            theme:
              stark: 0
              standards: 0

            YAML, file_get_contents($path));
    }

    public function testAnotherProfileIsKept(): void
    {
        $path = $this->file("module:\n  thunder: 1000\nprofile: thunder\n");

        CoreExtension::read($path)->withoutProfile('standard')->write($path);

        self::assertSame("module:\n  thunder: 1000\nprofile: thunder\n", file_get_contents($path));
    }

    public function testTheModuleSectionIsFoundAgainOnceLinesAboveItAreGone(): void
    {
        $path = $this->file("standard: 1\nmodule:\n  node: 0\ntheme:\n  stark: 0\n");

        CoreExtension::read($path)->withoutProfile('standard')->withSqlite()->write($path);

        self::assertSame("module:\n  node: 0\n  sqlite: 0\ntheme:\n  stark: 0\n", file_get_contents($path));
    }

    public function testAFileWithoutAModuleSectionIsRefused(): void
    {
        $path = $this->file("theme:\n  stark: 0\n");

        self::assertFails(Failure::FAILED, sprintf('core extension file %s has no module section', $path), static fn () => CoreExtension::read($path));
    }

    public function testAMissingFileIsRefused(): void
    {
        self::assertFails(Failure::FAILED, 'failed to read core extension file /nonexistent/core.extension.yml', static fn () => CoreExtension::read('/nonexistent/core.extension.yml'));
    }

    public function testAnUnwritableFileIsAFailure(): void
    {
        $path = $this->file("module:\n  node: 0\n");
        $extension = CoreExtension::read($path);
        unlink($path);
        mkdir($path);

        self::assertFails(Failure::FAILED, sprintf('failed to write core extension file %s', $path), static fn () => $extension->write($path));
    }

    private function file(string $yaml): string
    {
        $dir = $this->scratchDir();
        file_put_contents($dir . '/core.extension.yml', $yaml);
        return $dir . '/core.extension.yml';
    }
}

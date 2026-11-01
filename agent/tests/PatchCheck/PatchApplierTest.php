<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\PatchCheck;

use Composer\IO\NullIO;
use Composer\Util\ProcessExecutor;
use Drupdater\Agent\PatchCheck\PatchApplier;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class PatchApplierTest extends TestCase
{
    use Scratch;

    /**
     * @return iterable<string, array{bool}>
     */
    public static function installKinds(): iterable
    {
        yield 'dist install, patched with patch' => [false];
        yield 'source install, patched with git apply' => [true];
    }

    #[DataProvider('installKinds')]
    public function testAppliesAPatchThatFits(bool $source): void
    {
        $dir = $this->package($source);

        self::assertTrue($this->applier()->apply($dir, $this->patch(self::GREETING_PATCH), PatchApplier::DEFAULT_LEVELS));
        self::assertSame("hello world\n", file_get_contents($dir . '/src/greeting.txt'));
    }

    #[DataProvider('installKinds')]
    public function testRejectsAPatchThatDoesNotFit(bool $source): void
    {
        $dir = $this->package($source);
        file_put_contents($dir . '/src/greeting.txt', "bonjour\n");

        self::assertFalse($this->applier()->apply($dir, $this->patch(self::GREETING_PATCH), PatchApplier::DEFAULT_LEVELS));
    }

    #[DataProvider('installKinds')]
    public function testRejectsAnAlreadyAppliedPatchWithoutPrompting(bool $source): void
    {
        $dir = $this->package($source);
        file_put_contents($dir . '/src/greeting.txt', "hello world\n");

        self::assertFalse($this->applier()->apply($dir, $this->patch(self::GREETING_PATCH), PatchApplier::DEFAULT_LEVELS));
        self::assertSame("hello world\n", file_get_contents($dir . '/src/greeting.txt'));
    }

    public function testFindsAPatchWrittenAtAnotherLevel(): void
    {
        $dir = $this->package(false);
        $p0 = str_replace(['a/src/', 'b/src/'], 'src/', self::GREETING_PATCH);

        self::assertTrue($this->applier()->apply($dir, $this->patch($p0), PatchApplier::DEFAULT_LEVELS));
        self::assertSame("hello world\n", file_get_contents($dir . '/src/greeting.txt'));
    }

    public function testTriesOnlyTheLevelsItIsGiven(): void
    {
        $dir = $this->package(false);
        $p0 = str_replace(['a/src/', 'b/src/'], 'src/', self::GREETING_PATCH);

        self::assertFalse($this->applier()->apply($dir, $this->patch($p0), ['-p1']));
    }

    public function testARejectedPatchLeavesNoHunkBehind(): void
    {
        $dir = $this->package(false);
        file_put_contents($dir . '/src/farewell.txt', "adieu\n");

        self::assertFalse($this->applier()->apply($dir, $this->patch(self::GREETING_PATCH . self::FAREWELL_PATCH), PatchApplier::DEFAULT_LEVELS));
        self::assertSame("hello\n", file_get_contents($dir . '/src/greeting.txt'));
    }

    private function applier(): PatchApplier
    {
        return new PatchApplier(new ProcessExecutor(new NullIO()));
    }

    private function package(bool $source): string
    {
        $dir = $this->scratchDir() . '/package';
        $this->writeFiles($dir, self::PACKAGE_FILES);
        if ($source) {
            exec('git -C ' . escapeshellarg($dir) . ' init -q 2>&1', $output, $status);
            self::assertSame(0, $status, implode("\n", $output));
        }
        return $dir;
    }

    private function patch(string $content): string
    {
        $file = $this->scratchDir() . '/change.patch';
        file_put_contents($file, $content);
        return $file;
    }
}

<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\PatchCheck;

use Composer\IO\NullIO;
use Composer\Util\HttpDownloader;
use Composer\Util\ProcessExecutor;
use Drupdater\Agent\Failure;
use Drupdater\Agent\PatchCheck\PackageFetcher;
use Drupdater\Agent\PatchCheck\PatchApplier;
use Drupdater\Agent\PatchCheck\PatchChecker;
use Drupdater\Agent\PatchCheck\PatchFiles;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\TestCase;

final class PatchCheckerTest extends TestCase
{
    use Scratch;

    private ?string $fetchedInto = null;

    public function testPatchesApplyInOrder(): void
    {
        $dir = $this->scratchDir();
        $this->writeFiles($dir, [
            'first.patch' => self::GREETING_PATCH,
            'second.patch' => str_replace(['hello world', 'hello'], ['hello there', 'hello world'], self::GREETING_PATCH),
        ]);

        self::assertSame(['outcome' => 'applies'], $this->checker()->check('fixture/greeting', '1.0.0', '', [$dir . '/first.patch', $dir . '/second.patch']));
    }

    public function testNamesTheFirstPatchThatDoesNotApply(): void
    {
        $dir = $this->scratchDir();
        $this->writeFiles($dir, ['first.patch' => self::GREETING_PATCH, 'again.patch' => self::GREETING_PATCH, 'third.patch' => self::FAREWELL_PATCH]);

        $result = $this->checker()->check('fixture/greeting', '1.0.0', '', [$dir . '/first.patch', $dir . '/again.patch', $dir . '/third.patch']);

        self::assertSame(['outcome' => 'rejected', 'patch' => $dir . '/again.patch'], $result);
    }

    public function testUsesThePackagesOwnPatchLevel(): void
    {
        $dir = $this->scratchDir();
        $this->writeFiles($dir, ['p0.patch' => str_replace(['a/src/', 'b/src/'], 'src/', self::GREETING_PATCH)]);

        self::assertSame('applies', $this->checker()->check('fixture/greeting', '1.0.0', '', [$dir . '/p0.patch'])['outcome']);
        self::assertSame('rejected', $this->checker(['fixture/greeting' => '-p1'])->check('fixture/greeting', '1.0.0', '', [$dir . '/p0.patch'])['outcome']);
        self::assertSame('applies', $this->checker(['fixture/greeting' => '', 'other' => '-p1'])->check('fixture/greeting', '1.0.0', '', [$dir . '/p0.patch'])['outcome']);
    }

    public function testLeavesNoScratchDirectoryBehind(): void
    {
        $dir = $this->scratchDir();
        $this->writeFiles($dir, ['first.patch' => self::GREETING_PATCH]);

        $this->checker()->check('fixture/greeting', '1.0.0', '', [$dir . '/first.patch']);
        self::assertNotNull($this->fetchedInto);
        self::assertDirectoryDoesNotExist(dirname($this->fetchedInto));

        try {
            $this->checker()->check('fixture/greeting', '1.0.0', '', [$dir . '/missing.patch']);
            self::fail('expected a Failure');
        }
        catch (Failure $failure) {
            self::assertSame(PatchFiles::NOT_FOUND, $failure->reason);
        }
        self::assertDirectoryDoesNotExist(dirname($this->fetchedInto));
    }

    /**
     * @param array<string, string> $levels
     */
    private function checker(array $levels = []): PatchChecker
    {
        $fetcher = $this->createMock(PackageFetcher::class);
        $fetcher->method('fetch')->with('fixture/greeting', '1.0.0', '')
            ->willReturnCallback(function (string $n, string $v, string $r, string $target) {
                $this->fetchedInto = $target;
                $this->writeFiles($target, self::PACKAGE_FILES);
            });
        return new PatchChecker(
            $fetcher,
            new PatchFiles($this->createMock(HttpDownloader::class)),
            new PatchApplier(new ProcessExecutor(new NullIO())),
            $levels,
        );
    }
}

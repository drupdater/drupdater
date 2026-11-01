<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\PatchCheck;

use Composer\Downloader\TransportException;
use Composer\Util\HttpDownloader;
use Drupdater\Agent\Failure;
use Drupdater\Agent\PatchCheck\PatchFiles;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\TestCase;

final class PatchFilesTest extends TestCase
{
    use Scratch;

    public function testALocalPatchIsUsedWhereItIs(): void
    {
        $dir = $this->scratchDir();
        $this->writeFiles($dir, ['patches/a.patch' => self::GREETING_PATCH]);
        $downloader = $this->createMock(HttpDownloader::class);
        $downloader->expects(self::never())->method('copy');

        self::assertSame($dir . '/patches/a.patch', (new PatchFiles($downloader))->local($dir . '/patches/a.patch', $dir . '/scratch'));
    }

    public function testAMissingLocalPatchIsUnobtainable(): void
    {
        $dir = $this->scratchDir();
        $failure = $this->failure(fn () => (new PatchFiles($this->createMock(HttpDownloader::class)))->local($dir . '/gone.patch', $dir));

        self::assertSame(Failure::UNOBTAINABLE, $failure->kind);
        self::assertSame(PatchFiles::NOT_FOUND, $failure->reason);
        self::assertSame($dir . '/gone.patch', $failure->getMessage());
    }

    public function testARemotePatchIsDownloadedIntoTheScratchDirectory(): void
    {
        $scratch = $this->scratchDir() . '/patches';
        $downloader = $this->createMock(HttpDownloader::class);
        $downloader->expects(self::once())->method('copy')
            ->with('https://www.drupal.org/files/issues/1.patch', self::stringStartsWith($scratch . '/'))
            ->willReturnCallback(fn (string $url, string $to) => file_put_contents($to, self::GREETING_PATCH));

        $file = (new PatchFiles($downloader))->local('https://www.drupal.org/files/issues/1.patch', $scratch);

        self::assertStringEqualsFile($file, self::GREETING_PATCH);
    }

    public function testATransientDownloadFailureIsRetriedOnce(): void
    {
        $scratch = $this->scratchDir();
        $downloader = $this->createMock(HttpDownloader::class);
        $calls = 0;
        $downloader->expects(self::exactly(2))->method('copy')
            ->willReturnCallback(function (string $url, string $to) use (&$calls) {
                if (++$calls === 1) {
                    throw new TransportException('connection reset');
                }
                return file_put_contents($to, self::GREETING_PATCH);
            });

        $file = (new PatchFiles($downloader))->local('HTTPS://example.com/1.patch', $scratch);

        self::assertStringEqualsFile($file, self::GREETING_PATCH);
    }

    public function testARemotePatchThatFailsTwiceIsUnobtainable(): void
    {
        $downloader = $this->createMock(HttpDownloader::class);
        $downloader->expects(self::exactly(2))->method('copy')->willThrowException(new TransportException('HTTP 404'));

        $failure = $this->failure(fn () => (new PatchFiles($downloader))->local('http://example.com/1.patch', $this->scratchDir()));

        self::assertSame(Failure::UNOBTAINABLE, $failure->kind);
        self::assertSame(PatchFiles::NOT_DOWNLOADABLE, $failure->reason);
        self::assertSame('HTTP 404', $failure->getMessage());
    }

    private function failure(\Closure $fn): Failure
    {
        try {
            $fn();
        }
        catch (Failure $failure) {
            return $failure;
        }
        self::fail('expected a Failure');
    }
}

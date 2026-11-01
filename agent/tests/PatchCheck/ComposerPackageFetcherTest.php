<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\PatchCheck;

use Composer\Downloader\TransportException;
use Composer\Factory;
use Composer\IO\NullIO;
use Drupdater\Agent\Failure;
use Drupdater\Agent\PatchCheck\ComposerPackageFetcher;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\TestCase;

final class ComposerPackageFetcherTest extends TestCase
{
    use Scratch {
        tearDown as scratchTearDown;
    }

    private string|false $composerHome;

    protected function setUp(): void
    {
        $this->composerHome = getenv('COMPOSER_HOME');
        putenv('COMPOSER_HOME=' . $this->scratchDir());
    }

    protected function tearDown(): void
    {
        putenv($this->composerHome === false ? 'COMPOSER_HOME' : 'COMPOSER_HOME=' . $this->composerHome);
        $this->scratchTearDown();
    }

    public function testFetchesThePackageFromTheProjectsOwnRepositories(): void
    {
        $target = $this->scratchDir() . '/package';

        $this->fetcher()->fetch('fixture/greeting', '1.0.0', '', $target);

        self::assertStringEqualsFile($target . '/src/greeting.txt', "hello\n");
    }

    public function testAPackageNoRepositoryHasIsUnobtainable(): void
    {
        $failure = $this->failure(fn () => $this->fetcher()->fetch('fixture/greeting', '9.9.9', '', $this->scratchDir() . '/package'));

        self::assertSame(Failure::UNOBTAINABLE, $failure->kind);
        self::assertSame(ComposerPackageFetcher::NOT_AVAILABLE, $failure->reason);
        self::assertSame('fixture/greeting 9.9.9', $failure->getMessage());
    }

    public function testAnAuthenticationFailureIsToldApart(): void
    {
        foreach ([401, 403] as $status) {
            $e = new TransportException('denied', $status);
            $e->setStatusCode($status);

            $failure = ComposerPackageFetcher::unobtainable($e);

            self::assertSame(Failure::UNOBTAINABLE, $failure->kind);
            self::assertSame(ComposerPackageFetcher::AUTH_FAILED, $failure->reason, (string) $status);
            self::assertSame('denied', $failure->getMessage());
            self::assertSame($e, $failure->getPrevious());
        }
    }

    public function testAnyOtherFailureIsAFailedDownload(): void
    {
        $notFound = new TransportException('gone', 404);
        $notFound->setStatusCode(404);

        foreach ([$notFound, new TransportException('reset'), new \RuntimeException('git clone failed')] as $e) {
            self::assertSame(ComposerPackageFetcher::DOWNLOAD_FAILED, ComposerPackageFetcher::unobtainable($e)->reason);
        }
    }

    private function fetcher(): ComposerPackageFetcher
    {
        $package = $this->scratchDir() . '/greeting';
        $this->writeFiles($package, self::PACKAGE_FILES + [
            'composer.json' => json_encode(['name' => 'fixture/greeting', 'version' => '1.0.0']),
        ]);
        $project = $this->scratchDir();
        $this->writeFiles($project, ['composer.json' => json_encode([
            'name' => 'fixture/project',
            'repositories' => [
                ['type' => 'path', 'url' => $package, 'options' => ['symlink' => false]],
                ['packagist.org' => false],
            ],
        ])]);

        $cwd = getcwd();
        chdir($project);
        try {
            return new ComposerPackageFetcher((new Factory())->createComposer(new NullIO(), $project . '/composer.json', true, $project));
        }
        finally {
            chdir((string) $cwd);
        }
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

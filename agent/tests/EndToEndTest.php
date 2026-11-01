<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Composer\Util\Filesystem;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Process\Process;

/**
 * Installs the agent as a global plugin, as the image does, and asks a real Composer.
 */
final class EndToEndTest extends TestCase
{
    use Scratch;

    private static string $home;

    public static function setUpBeforeClass(): void
    {
        self::$home = sys_get_temp_dir() . '/drupdater-agent-home-' . bin2hex(random_bytes(6));
        mkdir(self::$home);
        file_put_contents(self::$home . '/composer.json', json_encode([
            'repositories' => [
                ['type' => 'path', 'url' => dirname(__DIR__), 'options' => ['symlink' => true, 'versions' => ['drupdater/agent' => '1.0.0']]],
                ['packagist.org' => false],
            ],
            'require' => ['drupdater/agent' => '1.0.0'],
            'config' => ['allow-plugins' => ['drupdater/agent' => true]],
        ]));
        $install = self::composer(['global', 'update', '--no-interaction', '--no-progress']);
        self::assertSame(0, $install->getExitCode(), $install->getErrorOutput());
    }

    public static function tearDownAfterClass(): void
    {
        (new Filesystem())->removeDirectory(self::$home);
    }

    public function testHandshake(): void
    {
        $reply = $this->ask('drupdater:handshake', $this->project(), ['protocol' => 2, 'request' => []]);

        self::assertEquals(self::fixture('handshake/response.json'), $reply);
    }

    public function testProjectAnswersTheFixture(): void
    {
        $project = $this->scratchDir();
        (new Filesystem())->copy(__DIR__ . '/fixtures/project', $project);

        $reply = $this->ask('drupdater:project', $project, self::fixture('project/request.json'));

        self::assertEquals(self::fixture('project/response.json'), $reply);
    }

    public function testResolveReportsTheUpdateAndPutsTheFilesBack(): void
    {
        $project = $this->lockedProject();
        $files = [file_get_contents($project . '/composer.json'), file_get_contents($project . '/composer.lock')];

        $reply = $this->ask('drupdater:resolve', $project, self::fixture('resolve/request.json'));

        self::assertEquals(self::fixture('resolve/response.json'), $reply);
        self::assertSame($files, [file_get_contents($project . '/composer.json'), file_get_contents($project . '/composer.lock')]);
    }

    public function testResolveWithWriteKeepsTheUpdateAndBumpsTheConstraints(): void
    {
        $project = $this->lockedProject();

        $reply = $this->ask('drupdater:resolve', $project, ['protocol' => 2, 'request' => ['packages' => [], 'keep' => [], 'minimal_changes' => false, 'write' => true]]);

        self::assertCount(2, $reply['result']['changes']);
        $lock = json_decode((string) file_get_contents($project . '/composer.lock'), true);
        self::assertSame(['1.1.0', '2.1.0'], array_column($lock['packages'], 'version'));
        $manifest = json_decode((string) file_get_contents($project . '/composer.json'), true);
        self::assertSame(['fixture/a' => '^1.1', 'fixture/b' => '^2.1'], $manifest['require']);
    }

    public function testResolveKeepsAPinnedPackageAndUpdatesOnlyWhatItIsAskedTo(): void
    {
        $project = $this->lockedProject();

        $pinned = $this->ask('drupdater:resolve', $project, ['protocol' => 2, 'request' => [
            'packages' => [], 'keep' => [['package' => 'fixture/a', 'version' => '1.0.0']], 'minimal_changes' => false, 'write' => false,
        ]]);
        $only = $this->ask('drupdater:resolve', $project, ['protocol' => 2, 'request' => [
            'packages' => ['fixture/a'], 'keep' => [], 'minimal_changes' => false, 'write' => false,
        ]]);

        self::assertSame(['fixture/b'], array_column($pinned['result']['changes'], 'package'));
        self::assertSame(['fixture/a'], array_column($only['result']['changes'], 'package'));
    }

    public function testResolveReportsAnUnresolvableUpdateAndPutsTheFilesBack(): void
    {
        $project = $this->lockedProject();
        $lock = file_get_contents($project . '/composer.lock');

        [$reply, $stderr] = $this->askWithErrors('drupdater:resolve', $project, ['protocol' => 2, 'request' => [
            'packages' => [], 'keep' => [['package' => 'fixture/a', 'version' => '9.9.9']], 'minimal_changes' => false, 'write' => false,
        ]]);

        self::assertFalse($reply['ok']);
        self::assertSame(['failed', 'composer update failed'], [$reply['error']['kind'], $reply['error']['reason']]);
        self::assertStringContainsString('fixture/a', $stderr, "Composer's own explanation reaches stderr");
        self::assertSame($lock, file_get_contents($project . '/composer.lock'));
    }

    public function testResolveRefusesAProjectWithoutALock(): void
    {
        $project = $this->lockedProject();
        unlink($project . '/composer.lock');

        $reply = $this->ask('drupdater:resolve', $project, self::fixture('resolve/request.json'));

        self::assertSame(['failed', 'the project has no composer.lock'], [$reply['error']['kind'], $reply['error']['reason']]);
    }

    public function testAuditOfACleanLockFindsNothing(): void
    {
        $reply = $this->ask('drupdater:audit', $this->lockedProject(), self::fixture('audit/request.json'));

        self::assertSame(['advisories' => [], 'abandoned' => []], $reply['result']);
    }

    public function testPlatformCheckFindsThisPhpMeetsALockWithoutRequirements(): void
    {
        $reply = $this->ask('drupdater:platform-check', $this->lockedProject(), self::fixture('platform-check/request.json'));

        self::assertSame(['unmet' => []], $reply['result']);
    }

    public function testWritePatchesLeavesALockComposerAgreesIsUpToDate(): void
    {
        $project = $this->lockedProject();
        $lock = (string) file_get_contents($project . '/composer.lock');

        $reply = $this->askVerbatim('drupdater:write-patches', $project, 'write-patches/request.json');

        self::assertTrue($reply['ok']);
        $validate = self::composer(['--working-dir=' . $project, 'validate', '--no-check-publish', '--no-check-all']);
        self::assertSame(0, $validate->getExitCode(), $validate->getOutput() . $validate->getErrorOutput());
        $hash = json_decode((string) file_get_contents($project . '/composer.lock'), true)['content-hash'];
        $before = json_decode($lock, true)['content-hash'];
        self::assertSame(str_replace($before, $hash, $lock), file_get_contents($project . '/composer.lock'));
    }

    public function testAllowPluginsOpenedAndClosedLeavesComposerJsonAsItWas(): void
    {
        $project = $this->lockedProject();
        // Composer ends every file it writes with a newline; so does a project's own.
        file_put_contents($project . '/composer.json', file_get_contents($project . '/composer.json') . "\n");
        $manifest = (string) file_get_contents($project . '/composer.json');

        $opened = $this->ask('drupdater:allow-plugins', $project, self::fixture('allow-plugins/open-request.json'));
        self::assertSame(['original' => null, 'opened' => true], $opened['result']);
        $closed = $this->ask('drupdater:allow-plugins', $project, ['protocol' => 2, 'request' => ['step' => 'close'] + $opened['result']]);

        self::assertSame(['denied' => []], $closed['result']);
        self::assertSame($manifest, file_get_contents($project . '/composer.json'));
    }

    public function testAPatchThatFitsApplies(): void
    {
        $project = $this->project();
        $this->writeFiles($project, ['patches/greeting.patch' => self::GREETING_PATCH]);

        $reply = $this->ask('drupdater:patch-check', $project, $this->request([$project . '/patches/greeting.patch']));

        self::assertSame(['outcome' => 'applies'], $reply['result']);
    }

    public function testAPatchThatDoesNotFitIsRejected(): void
    {
        $project = $this->project();
        $this->writeFiles($project, ['patches/greeting.patch' => str_replace('hello', 'bonjour', self::GREETING_PATCH)]);

        $reply = $this->ask('drupdater:patch-check', $project, $this->request([$project . '/patches/greeting.patch']));

        self::assertSame(['outcome' => 'rejected', 'patch' => $project . '/patches/greeting.patch'], $reply['result']);
    }

    public function testAVersionNoRepositoryHasIsUnobtainable(): void
    {
        $project = $this->project();
        $this->writeFiles($project, ['patches/greeting.patch' => self::GREETING_PATCH]);
        $request = $this->request([$project . '/patches/greeting.patch']);
        $request['request']['version'] = '2.0.0';

        $reply = $this->ask('drupdater:patch-check', $project, $request);

        self::assertFalse($reply['ok']);
        self::assertSame(['unobtainable', 'not available from any configured repository'], [$reply['error']['kind'], $reply['error']['reason']]);
    }

    /**
     * @param array<string, mixed> $request
     * @return array<string, mixed>
     */
    private function ask(string $command, string $project, array $request): array
    {
        return $this->askWithErrors($command, $project, $request)[0];
    }

    /**
     * @return array<string, mixed>
     */
    private function askVerbatim(string $command, string $project, string $fixture): array
    {
        $process = self::composer(
            ['--working-dir=' . $project, $command, '--no-interaction', '--no-ansi'],
            (string) file_get_contents(dirname(__DIR__) . '/protocol/' . $fixture),
        );
        $lines = explode("\n", trim($process->getOutput()));
        return json_decode(end($lines), true, 512, JSON_THROW_ON_ERROR);
    }

    /**
     * @param array<string, mixed> $request
     * @return array{array<string, mixed>, string}
     */
    private function askWithErrors(string $command, string $project, array $request): array
    {
        $process = self::composer(['--working-dir=' . $project, $command, '--no-interaction', '--no-ansi'], json_encode($request));
        $lines = explode("\n", trim($process->getOutput()));
        return [json_decode(end($lines), true, 512, JSON_THROW_ON_ERROR), $process->getErrorOutput()];
    }

    /**
     * A project locked at the older of two versions of each of its two packages, all offline.
     */
    private function lockedProject(): string
    {
        $project = $this->scratchDir();
        $versions = static fn (string $name, string ...$versions): array => array_map(
            static fn (string $version): array => ['name' => $name, 'version' => $version, 'type' => 'library'],
            $versions,
        );
        $this->writeFiles($project, ['composer.json' => json_encode([
            'name' => 'fixture/site',
            'repositories' => [
                ['type' => 'package', 'package' => [...$versions('fixture/a', '1.0.0', '1.1.0'), ...$versions('fixture/b', '2.0.0', '2.1.0')]],
                ['packagist.org' => false],
            ],
            'require' => ['fixture/a' => '^1.0', 'fixture/b' => '^2.0'],
        ], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES)]);
        $lock = self::composer(['--working-dir=' . $project, 'update', '--no-install', '--no-interaction', '--with=fixture/a:1.0.0', '--with=fixture/b:2.0.0']);
        self::assertSame(0, $lock->getExitCode(), $lock->getErrorOutput());
        return $project;
    }

    /**
     * @param list<string> $patches
     * @return array<string, mixed>
     */
    private function request(array $patches): array
    {
        return ['protocol' => 2, 'request' => ['package' => 'fixture/greeting', 'version' => '1.0.0', 'reference' => '', 'patches' => $patches]];
    }

    private function project(): string
    {
        $package = $this->scratchDir();
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
        return $project;
    }

    /**
     * @param list<string> $args
     */
    private static function composer(array $args, ?string $stdin = null): Process
    {
        $process = new Process(
            [PHP_BINARY, dirname(__DIR__) . '/vendor/bin/composer', ...$args],
            null,
            ['COMPOSER_HOME' => self::$home, 'COMPOSER_ALLOW_SUPERUSER' => '1', 'COMPOSER_CACHE_DIR' => self::$home . '/cache'],
            $stdin,
            120,
        );
        $process->run();
        return $process;
    }
}

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
                // Offline: the agent's one dependency comes from its own vendor/.
                ['type' => 'path', 'url' => dirname(__DIR__) . '/vendor/ion-bazan/composer-diff', 'options' => [
                    'symlink' => true, 'versions' => ['ion-bazan/composer-diff' => self::lockedVersion('ion-bazan/composer-diff')],
                ]],
                ['packagist.org' => false],
            ],
            'require' => ['drupdater/agent' => '1.0.0'],
            // As in the image: the agent only uses composer-diff's classes, never its command.
            'config' => ['allow-plugins' => ['drupdater/agent' => true, 'ion-bazan/composer-diff' => false]],
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
        $reply = $this->ask('drupdater:handshake', $this->scratchDir(), ['protocol' => 3, 'request' => []]);

        self::assertEquals(self::fixture('handshake/response.json'), $reply);
    }

    public function testProjectAnswersTheFixture(): void
    {
        $project = $this->scratchDir();
        (new Filesystem())->copy(__DIR__ . '/fixtures/project', $project);

        $reply = $this->ask('drupdater:project', $project, self::fixture('project/request.json'));

        self::assertEquals(self::fixture('project/response.json'), $reply);
    }

    public function testResolveWritesTheUpdateAndBumpsTheConstraints(): void
    {
        $project = $this->lockedProject();

        $reply = $this->ask('drupdater:resolve', $project, self::fixture('resolve/request.json'));

        self::assertEquals(self::fixture('resolve/response.json'), $reply);
        $lock = json_decode((string) file_get_contents($project . '/composer.lock'), true);
        self::assertSame(['1.1.0', '2.1.0'], array_column($lock['packages'], 'version'));
        $manifest = json_decode((string) file_get_contents($project . '/composer.json'), true);
        self::assertSame(['fixture/a' => '^1.1', 'fixture/b' => '^2.1'], $manifest['require']);
    }

    public function testResolveKeepsAPinnedPackageAndUpdatesOnlyWhatItIsAskedTo(): void
    {
        $project = $this->lockedProject();

        $pinned = $this->ask('drupdater:resolve', $project, ['protocol' => 3, 'request' => [
            'packages' => [], 'keep' => [['package' => 'fixture/a', 'version' => '1.0.0']], 'minimal_changes' => false,
        ]]);
        $only = $this->ask('drupdater:resolve', $this->lockedProject(), ['protocol' => 3, 'request' => [
            'packages' => ['fixture/a'], 'keep' => [], 'minimal_changes' => false,
        ]]);

        self::assertSame(['fixture/b'], array_column($pinned['result']['changes'], 'package'));
        self::assertSame(['fixture/a'], array_column($only['result']['changes'], 'package'));
    }

    public function testResolveReportsAnUnresolvableUpdateAndLeavesTheLock(): void
    {
        $project = $this->lockedProject();
        $lock = file_get_contents($project . '/composer.lock');

        [$reply, $stderr] = $this->askWithErrors('drupdater:resolve', $project, ['protocol' => 3, 'request' => [
            'packages' => [], 'keep' => [['package' => 'fixture/a', 'version' => '9.9.9']], 'minimal_changes' => false,
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

    public function testAddonsListsTheCatalogueWithoutAProject(): void
    {
        $reply = $this->ask('drupdater:addons', $this->scratchDir(), self::fixture('addons/request.json'));

        self::assertEquals(self::fixture('addons/response.json'), $reply);
    }

    public function testComposerDiffReportsWhatTheUpdateChangedSinceTheBaseLock(): void
    {
        $project = $this->lockedProject();
        $base = json_decode((string) file_get_contents($project . '/composer.lock'), true);
        $update = self::composer(['--working-dir=' . $project, 'update', '--no-install', '--no-interaction', 'fixture/a']);
        self::assertSame(0, $update->getExitCode(), $update->getErrorOutput());

        $reply = $this->hook($project, 'composer_diff', ['name' => 'post-composer-update', 'base_lock' => $base]);

        self::assertSame(
            [['package' => 'fixture/a', 'operation' => 'upgrade', 'from' => '1.0.0', 'to' => '1.1.0', 'dev' => false, 'direct' => true]],
            $reply['result']['finding'],
        );
        self::assertSame('info', $reply['log'][0]['level'], 'the agent logs through its envelope, never its own output');
    }

    public function testComposerAuditOfACleanLockFindsNothing(): void
    {
        $reply = $this->hook($this->lockedProject(), 'composer_audit', ['name' => 'pre-composer-update', 'plan' => []]);

        self::assertSame(['advisories' => [], 'abandoned' => []], $reply['result']['finding']);
    }

    public function testASecurityRunWithNothingToFixEndsWithoutAFailure(): void
    {
        $reply = $this->hook($this->lockedProject(), 'composer_audit', ['name' => 'pre-composer-update', 'plan' => []], security: true);

        self::assertSame('No security advisories found', $reply['result']['abort']);
    }

    public function testPlatformCheckFindsThisPhpMeetsALockWithoutRequirements(): void
    {
        $reply = $this->ask('drupdater:platform-check', $this->lockedProject(), self::fixture('platform-check/request.json'));

        self::assertSame(['unmet' => []], $reply['result']);
    }

    public function testComposerAllowPluginsOpenedAndClosedLeavesComposerJsonAsItWas(): void
    {
        $project = $this->lockedProject();
        // Composer ends every file it writes with a newline; so does a project's own.
        file_put_contents($project . '/composer.json', file_get_contents($project . '/composer.json') . "\n");
        $manifest = (string) file_get_contents($project . '/composer.json');

        $opened = $this->hook($project, 'composer_allow_plugins', ['name' => 'pre-composer-update', 'plan' => []]);
        self::assertSame(['original' => null, 'opened' => true], $opened['result']['finding']);
        $closed = $this->hook($project, 'composer_allow_plugins', ['name' => 'post-composer-update', 'base_lock' => []], [
            ['event' => 'pre-composer-update', 'site' => '', 'finding' => $opened['result']['finding'], 'committed' => []],
        ]);

        self::assertSame([], $closed['result']['finding']);
        self::assertSame($manifest, file_get_contents($project . '/composer.json'));
    }

    public function testComposerPatchesLeavesAPatchThatStillAppliesAndTheFilesAsTheyWere(): void
    {
        $project = $this->patchedProject(self::GREETING_PATCH);
        $files = [file_get_contents($project . '/composer.json'), file_get_contents($project . '/composer.lock')];

        $reply = $this->hook($project, 'composer_patches', ['name' => 'pre-composer-update', 'plan' => []]);

        self::assertSame(['removed' => [], 'updated' => [], 'conflicts' => []], $reply['result']['finding']);
        self::assertSame([], $reply['result']['commits']);
        self::assertSame($files, [file_get_contents($project . '/composer.json'), file_get_contents($project . '/composer.lock')], 'the dry run is put back');
    }

    public function testComposerPatchesHoldsBackAPackageWhosePatchNoLongerApplies(): void
    {
        $project = $this->patchedProject(str_replace('hello', 'bonjour', self::GREETING_PATCH));

        $reply = $this->hook($project, 'composer_patches', ['name' => 'pre-composer-update', 'plan' => []]);

        self::assertSame([[
            'package' => 'fixture/greeting',
            'fixed_version' => '1.0.0',
            'patch_path' => 'patches/greeting.patch',
            'patch_description' => 'Greet the world',
            'new_version' => '1.1.0',
        ]], $reply['result']['finding']['conflicts']);
        self::assertSame([['package' => 'fixture/greeting', 'version' => '1.0.0']], $reply['result']['plan']['keep']);
    }

    public function testComposerPatchesLeavesALockComposerAgreesIsUpToDate(): void
    {
        $project = $this->patchedProject(self::GREETING_PATCH, ['fixture/gone' => ['Gone' => 'https://example.com/gone.patch']]);

        $reply = $this->hook($project, 'composer_patches', ['name' => 'pre-composer-update', 'plan' => []]);

        self::assertSame(['fixture/gone'], array_column($reply['result']['finding']['removed'], 'package'));
        self::assertSame([['message' => 'Update patches', 'paths' => ['composer.json', 'composer.lock']]], $reply['result']['commits']);
        self::assertObjectNotHasProperty('fixture/gone', $this->manifestOf($project)->extra->patches);
        $validate = self::composer(['--working-dir=' . $project, 'validate', '--no-check-publish', '--no-check-all']);
        self::assertSame(0, $validate->getExitCode(), $validate->getOutput() . $validate->getErrorOutput());
    }

    public function testReportBuildsTheSectionsWithoutAProject(): void
    {
        $reply = $this->ask('drupdater:report', $this->scratchDir(), self::fixture('report/request.json'));

        self::assertEquals(self::fixture('report/response.json'), $reply);
    }

    /**
     * @param array<string, mixed> $event
     * @param list<array<string, mixed>> $earlier
     * @return array<string, mixed>
     */
    private function hook(string $project, string $addon, array $event, array $earlier = [], bool $security = false): array
    {
        $reply = $this->ask('drupdater:hook', $project, ['protocol' => 3, 'request' => [
            'addon' => $addon, 'event' => $event, 'earlier' => $earlier, 'run' => ['security' => $security],
        ]]);
        self::assertTrue($reply['ok'], json_encode($reply));
        return $reply;
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
     * A project locked at fixture/greeting 1.0.0, which 1.1.0 updates, with one patch for it. Both
     * versions come from path repositories, so the patch is checked against a real download.
     *
     * @param array<string, array<string, string>> $morePatches
     */
    private function patchedProject(string $patch, array $morePatches = []): string
    {
        $repositories = [];
        foreach (['1.0.0', '1.1.0'] as $version) {
            $package = $this->scratchDir();
            $this->writeFiles($package, self::PACKAGE_FILES + ['composer.json' => json_encode(['name' => 'fixture/greeting', 'version' => $version])]);
            $repositories[] = ['type' => 'path', 'url' => $package, 'canonical' => false, 'options' => ['symlink' => false]];
        }
        $project = $this->scratchDir();
        $this->writeFiles($project, [
            'composer.json' => json_encode([
                'name' => 'fixture/project',
                'repositories' => [...$repositories, ['packagist.org' => false]],
                'require' => ['fixture/greeting' => '^1.0'],
                'extra' => ['patches' => ['fixture/greeting' => ['Greet the world' => 'patches/greeting.patch']] + $morePatches],
            ], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES),
            'patches/greeting.patch' => $patch,
        ]);
        $lock = self::composer(['--working-dir=' . $project, 'update', '--no-install', '--no-interaction', '--with=fixture/greeting:1.0.0']);
        self::assertSame(0, $lock->getExitCode(), $lock->getErrorOutput());
        return $project;
    }

    private function manifestOf(string $project): \stdClass
    {
        return json_decode((string) file_get_contents($project . '/composer.json'), false, 512, JSON_THROW_ON_ERROR);
    }

    private static function lockedVersion(string $package): string
    {
        $lock = json_decode((string) file_get_contents(dirname(__DIR__) . '/composer.lock'), true, 512, JSON_THROW_ON_ERROR);
        foreach ($lock['packages'] as $locked) {
            if ($locked['name'] === $package) {
                return $locked['version'];
            }
        }
        throw new \LogicException($package . ' is not in the agent\'s composer.lock');
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

<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Resolve;

use Composer\Console\Application;
use Drupdater\Agent\Addon\Pin;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Resolve\Resolver;
use Drupdater\Agent\Tests\InProject;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Output\BufferedOutput;

/**
 * The dry run composer_patches checks patches against: Composer's own update, run in this process
 * as the agent runs it, then put back.
 */
final class ResolverTest extends TestCase
{
    use InProject;

    public function testADryRunReportsTheUpdateAndPutsTheFilesBack(): void
    {
        $project = $this->lockedProject();
        $files = [file_get_contents($project . '/composer.json'), file_get_contents($project . '/composer.lock')];

        $changes = $this->resolver($project)->resolve([], [], false, false);

        self::assertSame([
            ['action' => 'Upgrade', 'package' => 'fixture/a', 'from' => '1.0.0', 'to' => '1.1.0'],
            ['action' => 'Upgrade', 'package' => 'fixture/b', 'from' => '2.0.0', 'to' => '2.1.0'],
        ], $changes);
        self::assertSame($files, [file_get_contents($project . '/composer.json'), file_get_contents($project . '/composer.lock')]);
    }

    public function testADryRunKeepsAPinAndUpdatesOnlyWhatItIsAskedTo(): void
    {
        self::assertSame(['fixture/b'], array_column($this->resolver($this->lockedProject())->resolve([], [new Pin('fixture/a', '1.0.0')], false, false), 'package'));
        self::assertSame(['fixture/a'], array_column($this->resolver($this->lockedProject())->resolve(['fixture/a'], [], false, false), 'package'));
    }

    public function testAnUnresolvableDryRunFailsAndPutsTheFilesBack(): void
    {
        $project = $this->lockedProject();
        $lock = file_get_contents($project . '/composer.lock');

        try {
            $this->resolver($project)->resolve([], [new Pin('fixture/a', '9.9.9')], false, false);
            self::fail('an unresolvable update resolved');
        }
        catch (Failure $failure) {
            self::assertSame([Failure::FAILED, 'composer update failed'], [$failure->kind, $failure->reason]);
        }
        self::assertSame($lock, file_get_contents($project . '/composer.lock'));
    }

    public function testRefusesAProjectWithoutALock(): void
    {
        $project = $this->lockedProject();
        unlink($project . '/composer.lock');

        $this->expectExceptionObject(new Failure(Failure::FAILED, 'the project has no composer.lock'));
        $this->resolver($project)->resolve([], [], false, false);
    }

    private function resolver(string $project): Resolver
    {
        $application = new Application();
        $application->setAutoExit(false);
        $this->composer($project);
        return new Resolver($application->find('update'), new BufferedOutput());
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
        $this->lockAtTheOlderVersions($project);
        return $project;
    }

    private function lockAtTheOlderVersions(string $project): void
    {
        $application = new Application();
        $application->setAutoExit(false);
        $this->composer($project);
        $input = new ArrayInput(['--no-install' => true, '--with' => ['fixture/a:1.0.0', 'fixture/b:2.0.0']]);
        $input->setInteractive(false);
        self::assertSame(0, $application->find('update')->run($input, new BufferedOutput()));
    }
}

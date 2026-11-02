<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Composer\Util\Filesystem;
use Drupdater\Agent\Command\InstallCommand;
use Drupdater\Agent\Json;
use Drupdater\Agent\Tests\Decoded;
use Drupdater\Agent\Tests\InProject;
use PHPUnit\Framework\Attributes\WithoutErrorHandler;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Output\NullOutput;
use Symfony\Component\Console\Tester\CommandTester;

/**
 * drupdater:install, running Composer's own install in this process against an offline project.
 * PHPUnit's error handler is off: it reports the warnings Composer silences while it dumps the
 * autoloader.
 */
final class InstallCommandTest extends TestCase
{
    use InProject;

    #[WithoutErrorHandler]
    public function testInstallBringsVendorToTheLockAndAnswersTheFixture(): void
    {
        [$project] = $this->lockedProject();

        [$status, $reply] = $this->install($project);

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('install/response.json'), $reply);
        self::assertFileExists($project . '/vendor/fixture/lib/src/greeting.txt');
    }

    #[WithoutErrorHandler]
    public function testAnInstallComposerThrowsOutOfFails(): void
    {
        [$project, $package] = $this->lockedProject();
        (new Filesystem())->removeDirectory($package);

        [$status, $reply] = $this->install($project);

        self::assertSame(1, $status);
        self::assertSame(['failed', 'composer install failed'], [Json::at($reply, 'error', 'kind'), Json::at($reply, 'error', 'reason')]);
        self::assertStringContainsString($package, Json::string(Json::at($reply, 'error', 'message')));
    }

    #[WithoutErrorHandler]
    public function testALockWithoutACompatibleSetFailsWithComposersExitCode(): void
    {
        [$project] = $this->lockedProject();
        $lock = Decoded::envelope((string) file_get_contents($project . '/composer.lock'));
        $packages = Json::maps($lock['packages'] ?? null);
        $packages[0]['require'] = ['fixture/missing' => '^1.0'];
        $lock['packages'] = $packages;
        file_put_contents($project . '/composer.lock', json_encode($lock, JSON_THROW_ON_ERROR));

        [$status, $reply] = $this->install($project);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'failed', 'reason' => 'composer install failed', 'message' => 'exit code 2'], $reply['error']);
    }

    /**
     * A project locked to a package from a path repository, copied rather than linked.
     *
     * @return array{string, string} the project and the package
     */
    private function lockedProject(): array
    {
        $package = $this->scratchDir();
        $this->writeFiles($package, self::PACKAGE_FILES + ['composer.json' => json_encode(['name' => 'fixture/lib', 'version' => '1.0.0'], JSON_THROW_ON_ERROR)]);
        $project = $this->scratchDir();
        $this->writeFiles($project, ['composer.json' => json_encode([
            'name' => 'fixture/site',
            'repositories' => [['type' => 'path', 'url' => $package, 'options' => ['symlink' => false]], ['packagist.org' => false]],
            'require' => ['fixture/lib' => '1.0.0'],
        ], JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR)]);
        $application = new Application();
        $application->setAutoExit(false);
        self::assertSame(0, $application->run(new ArrayInput(['command' => 'update', '--no-install' => true, '--working-dir' => $project]), new NullOutput()));
        return [$project, $package];
    }

    /**
     * @return array{int, array<string, mixed>}
     */
    private function install(string $project): array
    {
        $command = new InstallCommand();
        $command->setComposer($this->composer($project));
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        $tester = new CommandTester($command);
        $tester->setInputs([json_encode(self::fixture('install/request.json'), JSON_THROW_ON_ERROR)]);
        $status = $tester->execute([]);
        $lines = explode("\n", trim($tester->getDisplay()));
        return [$status, Decoded::envelope((string) end($lines))];
    }
}

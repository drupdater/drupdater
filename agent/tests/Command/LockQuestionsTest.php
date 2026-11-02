<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Composer\Factory;
use Composer\IO\NullIO;
use Composer\Repository\PlatformRepository;
use Drupdater\Agent\Command\AgentCommand;
use Drupdater\Agent\Command\PlatformCheckCommand;
use Drupdater\Agent\Platform\PlatformCheck;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

/**
 * drupdater:platform-check, against an offline project.
 */
final class LockQuestionsTest extends TestCase
{
    use Scratch {
        tearDown as scratchTearDown;
    }

    private string|false $composerHome;
    private string $cwd;

    protected function setUp(): void
    {
        $this->composerHome = getenv('COMPOSER_HOME');
        putenv('COMPOSER_HOME=' . $this->scratchDir());
        $this->cwd = (string) getcwd();
    }

    protected function tearDown(): void
    {
        chdir($this->cwd);
        putenv($this->composerHome === false ? 'COMPOSER_HOME' : 'COMPOSER_HOME=' . $this->composerHome);
        $this->scratchTearDown();
    }

    public function testPlatformCheckAnswersTheFixture(): void
    {
        [$status, $reply] = $this->ask($this->platformCheck('8.1.0'), $this->project(), self::fixture('platform-check/request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('platform-check/response.json'), $reply);
    }

    public function testPlatformCheckCountsAPhpThatMeetsEveryConstraintAsMet(): void
    {
        [, $reply] = $this->ask($this->platformCheck('8.3.2'), $this->project(), self::fixture('platform-check/request.json'));

        self::assertSame(['ext-drupdaterfixture'], array_column($reply['result']['unmet'], 'name'));
    }

    public function testPlatformCheckReportsTheConstraintAPhpMisses(): void
    {
        [, $reply] = $this->ask($this->platformCheck('9.0.0'), $this->project(), self::fixture('platform-check/request.json'));

        self::assertSame(
            ['name' => 'php', 'version' => '9.0.0', 'constraint' => '>=7.4 <9', 'required_by' => 'fixture/safe'],
            $reply['result']['unmet'][1],
        );
    }

    public function testPlatformCheckAcceptsARequirementAnotherPackageProvides(): void
    {
        $project = $this->project(packages: [['name' => 'fixture/polyfill', 'version' => '1.0.0', 'provide' => ['ext-drupdaterfixture' => '1.0.0']]]);

        [, $reply] = $this->ask($this->platformCheck('8.3.2'), $project, self::fixture('platform-check/request.json'));

        self::assertSame([], $reply['result']['unmet']);
    }

    /**
     * @return iterable<string, array{AgentCommand}>
     */
    public static function lockCommands(): iterable
    {
        yield 'platform-check' => [new PlatformCheckCommand()];
    }

    #[\PHPUnit\Framework\Attributes\DataProvider('lockCommands')]
    public function testRefusesAProjectWithoutALock(AgentCommand $command): void
    {
        $project = $this->project();
        unlink($project . '/composer.lock');

        [$status, $reply] = $this->ask($command, $project, ['protocol' => 3, 'request' => []]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'failed', 'reason' => 'the project has no composer.lock', 'message' => ''], $reply['error']);
    }

    private function platformCheck(string $php): PlatformCheckCommand
    {
        return new PlatformCheckCommand(new PlatformCheck(new PlatformRepository([], ['php' => $php])));
    }

    /**
     * @param array<string, mixed> $config
     * @param list<array<string, mixed>> $packages extra locked packages
     */
    private function project(array $config = [], array $packages = []): string
    {
        $locked = [
            ['name' => 'fixture/a', 'version' => '1.0.0', 'type' => 'library', 'require' => ['ext-drupdaterfixture' => '*']],
            ['name' => 'fixture/old', 'version' => '1.0.0', 'type' => 'library', 'abandoned' => 'fixture/new'],
            ['name' => 'fixture/gone', 'version' => '1.0.0', 'type' => 'library', 'abandoned' => true],
            ['name' => 'fixture/safe', 'version' => '2.0.0', 'type' => 'library', 'require' => ['php' => '>=7.4 <9']],
            ...$packages,
        ];
        $project = $this->scratchDir();
        $this->writeFiles($project, [
            'composer.json' => json_encode([
                'name' => 'fixture/site',
                'repositories' => [['type' => 'package', 'package' => $locked], ['packagist.org' => false]],
                'require' => ['php' => '>=8.3'] + array_fill_keys(array_column($locked, 'name'), '*'),
                'config' => (object) $config,
            ], JSON_UNESCAPED_SLASHES),
            'composer.lock' => json_encode(['packages' => $locked, 'packages-dev' => []], JSON_UNESCAPED_SLASHES),
        ]);
        return $project;
    }

    /**
     * @param array<string, mixed> $request
     * @return array{int, array<string, mixed>}
     */
    private function ask(AgentCommand $command, string $project, array $request): array
    {
        chdir($project);
        $command->setComposer((new Factory())->createComposer(new NullIO(), $project . '/composer.json', true, $project));
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        $tester = new CommandTester($command);
        $tester->setInputs([json_encode($request, JSON_THROW_ON_ERROR)]);
        $status = $tester->execute([]);
        return [$status, json_decode($tester->getDisplay(), true, 512, JSON_THROW_ON_ERROR)];
    }
}

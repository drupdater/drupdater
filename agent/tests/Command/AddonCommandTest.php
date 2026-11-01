<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Command;

use Composer\Console\Application;
use Composer\Factory;
use Composer\IO\NullIO;
use Drupdater\Agent\Command\AddonCommand;
use Drupdater\Agent\Tests\Scratch;
use PHPUnit\Framework\TestCase;
use Symfony\Component\Console\Tester\CommandTester;

/**
 * drupdater:addon, and composer_diff behind it, against a project whose lock the update changed.
 */
final class AddonCommandTest extends TestCase
{
    use Scratch {
        tearDown as scratchTearDown;
    }

    private const LOCKED = [
        'packages' => [
            ['name' => 'fixture/a', 'version' => '1.1.0', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://github.com/fixture/a.git', 'reference' => '1.1.0']],
            ['name' => 'fixture/new', 'version' => '3.0.0', 'type' => 'library'],
        ],
        'packages-dev' => [
            ['name' => 'fixture/tool', 'version' => '2.0.0', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://github.com/fixture/tool.git', 'reference' => '2.0.0']],
        ],
    ];

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

    public function testComposerDiffAnswersTheFixture(): void
    {
        [$status, $reply] = $this->ask($this->project(self::LOCKED), self::fixture('addon/request.json'));

        self::assertSame(0, $status);
        self::assertEquals(self::fixture('addon/response.json'), $reply);
    }

    public function testComposerDiffLogsOneLinePerPackageWithoutLinks(): void
    {
        [, $reply] = $this->ask($this->project(self::LOCKED), self::fixture('addon/request.json'));

        self::assertSame([
            "dependency diff\n"
            . "upgrade fixture/a: 1.0.0 → 1.1.0\n"
            . "remove fixture/gone: 1.0.0 → -\n"
            . "install fixture/new: - → 3.0.0\n"
            . "downgrade fixture/tool (dev): 2.1.0 → 2.0.0\n",
        ], $reply['result']['log']);
    }

    public function testComposerDiffOfAnUnchangedLockFindsAndLogsNothing(): void
    {
        $request = self::fixture('addon/request.json');
        $project = $this->project($request['request']['event']['base_lock']);

        [$status, $reply] = $this->ask($project, $request);

        self::assertSame(0, $status);
        self::assertSame(['commits' => [], 'finding' => [], 'log' => []], $reply['result']);
    }

    public function testComposerDiffCountsANewCommitOfOneVersionAsAChange(): void
    {
        $base = ['packages' => [['name' => 'fixture/a', 'version' => 'dev-main', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://example.com/a.git', 'reference' => 'aaaaaaa']]]];
        $locked = ['packages' => [['name' => 'fixture/a', 'version' => 'dev-main', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://example.com/a.git', 'reference' => 'bbbbbbb']]]];

        [, $reply] = $this->ask($this->project($locked), self::request(['name' => 'post-composer-update', 'base_lock' => $base]));

        self::assertSame(['change'], array_column($reply['result']['finding'], 'operation'));
    }

    public function testComposerDiffSortsEachHalfByName(): void
    {
        $locked = ['packages' => [
            ['name' => 'fixture/zeta', 'version' => '1.0.0', 'type' => 'library'],
            ['name' => 'fixture/alpha', 'version' => '1.0.0', 'type' => 'library'],
        ], 'packages-dev' => [['name' => 'fixture/beta', 'version' => '1.0.0', 'type' => 'library']]];

        [, $reply] = $this->ask($this->project($locked), self::request(['name' => 'post-composer-update', 'base_lock' => ['packages' => []]]));

        self::assertSame(['fixture/alpha', 'fixture/zeta', 'fixture/beta'], array_column($reply['result']['finding'], 'package'));
    }

    /**
     * @return iterable<string, array{array<string, mixed>, string, string}>
     */
    public static function invalidRequests(): iterable
    {
        yield 'an unknown addon' => [['addon' => 'nope', 'event' => []], 'invalid_request', 'no addon "nope" runs in the agent'];
        yield 'no event' => [['addon' => 'composer_diff'], 'invalid_request', 'event must be an object'];
        yield 'an event composer_diff has no hook for' => [
            ['addon' => 'composer_diff', 'event' => ['name' => 'post-code-update']],
            'invalid_request',
            'composer_diff has no hook for "post-code-update"',
        ];
        yield 'no base lock' => [
            ['addon' => 'composer_diff', 'event' => ['name' => 'post-composer-update']],
            'invalid_request',
            'base_lock must be the composer.lock the update started from',
        ];
    }

    /**
     * @param array<string, mixed> $request
     */
    #[\PHPUnit\Framework\Attributes\DataProvider('invalidRequests')]
    public function testRefuses(array $request, string $kind, string $reason): void
    {
        [$status, $reply] = $this->ask($this->project(self::LOCKED), ['protocol' => 2, 'request' => $request]);

        self::assertSame(1, $status);
        self::assertSame(['kind' => $kind, 'reason' => $reason, 'message' => ''], $reply['error']);
    }

    public function testComposerDiffRefusesAProjectWithoutALock(): void
    {
        $project = $this->project(self::LOCKED);
        unlink($project . '/composer.lock');

        [$status, $reply] = $this->ask($project, self::fixture('addon/request.json'));

        self::assertSame(1, $status);
        self::assertSame(['kind' => 'failed', 'reason' => 'the project has no composer.lock', 'message' => ''], $reply['error']);
    }

    /**
     * @param array<string, mixed> $event
     * @return array<string, mixed>
     */
    private static function request(array $event): array
    {
        return ['protocol' => 2, 'request' => ['addon' => 'composer_diff', 'event' => $event]];
    }

    /**
     * @param array<string, mixed> $lock
     */
    private function project(array $lock): string
    {
        $project = $this->scratchDir();
        $this->writeFiles($project, [
            'composer.json' => json_encode([
                'name' => 'fixture/site',
                'repositories' => [['packagist.org' => false]],
                'require' => ['fixture/a' => '^1.0'],
                'require-dev' => ['fixture/tool' => '^2.0'],
            ], JSON_UNESCAPED_SLASHES),
            'composer.lock' => json_encode($lock + ['packages' => [], 'packages-dev' => []], JSON_UNESCAPED_SLASHES),
        ]);
        return $project;
    }

    /**
     * @param array<string, mixed> $request
     * @return array{int, array<string, mixed>}
     */
    private function ask(string $project, array $request): array
    {
        chdir($project);
        $command = new AddonCommand();
        $command->setComposer((new Factory())->createComposer(new NullIO(), $project . '/composer.json', true, $project));
        $application = new Application();
        $application->setAutoExit(false);
        $application->add($command);
        $tester = new CommandTester($command);
        $tester->setInputs([json_encode($request, JSON_THROW_ON_ERROR)]);
        $status = $tester->execute([]);
        return [$status, json_decode(trim($tester->getDisplay()), true, 512, JSON_THROW_ON_ERROR)];
    }
}

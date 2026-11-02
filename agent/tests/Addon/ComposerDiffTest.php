<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\ComposerDiff;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\Tests\InProject;
use PHPUnit\Framework\TestCase;

final class ComposerDiffTest extends TestCase
{
    use InProject;

    private const BASE = [
        'packages' => [
            ['name' => 'fixture/a', 'version' => '1.0.0', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://github.com/fixture/a.git', 'reference' => '1.0.0']],
            ['name' => 'fixture/gone', 'version' => '1.0.0', 'type' => 'library'],
        ],
        'packages-dev' => [
            ['name' => 'fixture/tool', 'version' => '2.1.0', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://github.com/fixture/tool.git', 'reference' => '2.1.0']],
        ],
    ];

    private const LOCKED = [
        'packages' => [
            ['name' => 'fixture/a', 'version' => '1.1.0', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://github.com/fixture/a.git', 'reference' => '1.1.0']],
            ['name' => 'fixture/new', 'version' => '3.0.0', 'type' => 'library'],
        ],
        'packages-dev' => [
            ['name' => 'fixture/tool', 'version' => '2.0.0', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://github.com/fixture/tool.git', 'reference' => '2.0.0']],
        ],
    ];

    public function testDiffsTheLockAgainstTheBaseWithLinksProductionFirst(): void
    {
        $result = $this->diff(self::LOCKED, self::BASE);

        self::assertSame([
            [
                'package' => 'fixture/a', 'operation' => 'upgrade', 'from' => '1.0.0', 'to' => '1.1.0', 'dev' => false, 'direct' => true,
                'compare_url' => 'https://github.com/fixture/a/compare/1.0.0...1.1.0', 'project_url' => 'https://github.com/fixture/a',
            ],
            ['package' => 'fixture/gone', 'operation' => 'remove', 'from' => '1.0.0', 'dev' => false, 'direct' => false],
            ['package' => 'fixture/new', 'operation' => 'install', 'to' => '3.0.0', 'dev' => false, 'direct' => false],
            [
                'package' => 'fixture/tool', 'operation' => 'downgrade', 'from' => '2.1.0', 'to' => '2.0.0', 'dev' => true, 'direct' => true,
                'compare_url' => 'https://github.com/fixture/tool/compare/2.1.0...2.0.0', 'project_url' => 'https://github.com/fixture/tool',
            ],
        ], $result->finding);
    }

    public function testLogsOneLinePerPackageWithoutLinks(): void
    {
        $log = new Log();

        $this->diff(self::LOCKED, self::BASE, $log);

        self::assertSame([[
            'level' => 'info',
            'message' => "dependency diff\n"
                . "upgrade fixture/a: 1.0.0 → 1.1.0\n"
                . "remove fixture/gone: 1.0.0 → -\n"
                . "install fixture/new: - → 3.0.0\n"
                . "downgrade fixture/tool (dev): 2.1.0 → 2.0.0\n",
            'context' => [],
        ]], $log->entries());
    }

    public function testAnUnchangedLockFindsAndLogsNothing(): void
    {
        $log = new Log();

        self::assertSame([], $this->diff(self::BASE, self::BASE, $log)->finding);
        self::assertSame([], $log->entries());
    }

    public function testANewCommitOfOneVersionIsAChange(): void
    {
        $at = static fn (string $reference): array => ['packages' => [['name' => 'fixture/a', 'version' => 'dev-main', 'type' => 'library', 'source' => ['type' => 'git', 'url' => 'https://example.com/a.git', 'reference' => $reference]]]];

        self::assertSame(['change'], array_column($this->diff($at('bbbbbbb'), $at('aaaaaaa'))->finding, 'operation'));
    }

    public function testSortsEachHalfByName(): void
    {
        $locked = ['packages' => [
            ['name' => 'fixture/zeta', 'version' => '1.0.0', 'type' => 'library'],
            ['name' => 'fixture/alpha', 'version' => '1.0.0', 'type' => 'library'],
        ], 'packages-dev' => [['name' => 'fixture/beta', 'version' => '1.0.0', 'type' => 'library']]];

        self::assertSame(['fixture/alpha', 'fixture/zeta', 'fixture/beta'], array_column($this->diff($locked, ['packages' => []])->finding, 'package'));
    }

    public function testRefusesAProjectWithoutALock(): void
    {
        $project = $this->project(self::LOCKED);
        unlink($project . '/composer.lock');

        $this->expectExceptionObject(new Failure(Failure::FAILED, 'the project has no composer.lock'));
        (new ComposerDiff(new Log(), $this->composer($project)))->handle(Event::postComposerUpdate($project, self::BASE));
    }

    public function testTheReportIsTheRowsAsFound(): void
    {
        $rows = [['package' => 'a/b', 'operation' => 'install', 'to' => '1.0.0', 'dev' => false, 'direct' => true]];

        self::assertSame($rows, ComposerDiff::report(Records::findings([Records::record('post-composer-update', $rows)])));
        self::assertNull(ComposerDiff::report(Records::findings([Records::record('post-composer-update', [])])));
        self::assertNull(ComposerDiff::report(Records::findings()));
    }

    /**
     * @param array<string, mixed> $locked
     * @param array<string, mixed> $base
     */
    private function diff(array $locked, array $base, Log $log = new Log()): \Drupdater\Agent\Addon\HookResult
    {
        $project = $this->project($locked);
        return (new ComposerDiff($log, $this->composer($project)))->handle(Event::postComposerUpdate($project, $base));
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
}

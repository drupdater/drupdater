<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Resolve;

use Drupdater\Agent\Resolve\LockDiff;
use PHPUnit\Framework\TestCase;

final class LockDiffTest extends TestCase
{
    public function testReportsEveryKindOfChangeInActionThenNameOrder(): void
    {
        $before = self::lock([
            self::pkg('z/up', '1.0.0'), self::pkg('a/up', '2.0.0'), self::pkg('b/down', '3.1.0'),
            self::pkg('c/gone', '1.2.3'), self::pkg('d/same', '4.0.0'),
        ]);
        $after = self::lock([
            self::pkg('z/up', '1.1.0'), self::pkg('a/up', '2.0.1'), self::pkg('b/down', '3.0.9'),
            self::pkg('e/new', '0.1.0'), self::pkg('d/same', '4.0.0'),
        ]);

        self::assertSame([
            ['action' => 'Upgrade', 'package' => 'a/up', 'from' => '2.0.0', 'to' => '2.0.1'],
            ['action' => 'Upgrade', 'package' => 'z/up', 'from' => '1.0.0', 'to' => '1.1.0'],
            ['action' => 'Downgrade', 'package' => 'b/down', 'from' => '3.1.0', 'to' => '3.0.9'],
            ['action' => 'Remove', 'package' => 'c/gone', 'from' => '1.2.3', 'to' => ''],
            ['action' => 'Install', 'package' => 'e/new', 'from' => '', 'to' => '0.1.0'],
        ], LockDiff::between($before, $after));
    }

    public function testANewCommitOfABranchIsAnUpgradeNamedByItsCommit(): void
    {
        $before = self::lock([self::pkg('drupal/foo', '1.x-dev', '0424dff9a1b2c3d4e5f60718293a4b5c6d7e8f90')]);
        $after = self::lock([self::pkg('drupal/foo', '1.x-dev', '141046a9a1b2c3d4e5f60718293a4b5c6d7e8f90')]);

        self::assertSame([
            ['action' => 'Upgrade', 'package' => 'drupal/foo', 'from' => '1.x-dev 0424dff', 'to' => '1.x-dev 141046a'],
        ], LockDiff::between($before, $after));
    }

    public function testADefaultBranchCountsAsTheNewest(): void
    {
        $before = self::lock([self::pkg('a/b', '2.0.0')]);
        $after = self::lock([self::pkg('a/b', 'dev-main', 'abcdef0123456789abcdef0123456789abcdef01')]);

        self::assertSame('Upgrade', LockDiff::between($before, $after)[0]['action']);
        self::assertSame('Upgrade', LockDiff::between($after, $before)[0]['action'], 'a branch has no order');
    }

    public function testAPackageThatOnlyMovedBetweenRequireAndRequireDevDidNotChange(): void
    {
        $before = ['packages' => [self::pkg('a/moved', '1.0.0'), self::pkg('b/moved', '1.0.0')], 'packages-dev' => []];
        $after = ['packages' => [], 'packages-dev' => [self::pkg('a/moved', '1.0.0'), self::pkg('b/moved', '1.1.0')]];

        self::assertSame([
            ['action' => 'Upgrade', 'package' => 'b/moved', 'from' => '1.0.0', 'to' => '1.1.0'],
        ], LockDiff::between($before, $after));
    }

    public function testADistReferenceAloneIsAChange(): void
    {
        $before = self::lock([['name' => 'a/b', 'version' => '1.0.0', 'dist' => ['type' => 'zip', 'url' => 'x', 'reference' => 'one']]]);
        $after = self::lock([['name' => 'a/b', 'version' => '1.0.0', 'dist' => ['type' => 'zip', 'url' => 'x', 'reference' => 'two']]]);

        self::assertCount(1, LockDiff::between($before, $after));
    }

    public function testNothingChangedIsAnEmptyList(): void
    {
        self::assertSame([], LockDiff::between(self::lock([self::pkg('a/b', '1.0.0')]), self::lock([self::pkg('a/b', '1.0.0')])));
        self::assertSame([], LockDiff::between([], []));
    }

    /**
     * @param list<array<string, mixed>> $packages
     * @return array<string, mixed>
     */
    private static function lock(array $packages): array
    {
        return ['packages' => $packages, 'packages-dev' => []];
    }

    /**
     * @return array<string, mixed>
     */
    private static function pkg(string $name, string $version, ?string $reference = null): array
    {
        $package = ['name' => $name, 'version' => $version];
        if ($reference !== null) {
            $package['source'] = ['type' => 'git', 'url' => 'https://example.com/' . $name, 'reference' => $reference];
        }
        return $package;
    }
}

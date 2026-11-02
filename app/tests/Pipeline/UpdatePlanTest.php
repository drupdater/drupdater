<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Pipeline;

use Drupdater\App\Pipeline\Pin;
use Drupdater\App\Pipeline\PlanConflict;
use Drupdater\App\Pipeline\UpdatePlan;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class UpdatePlanTest extends TestCase
{
    public function testMergeIntersectsRestrictions(): void
    {
        $merged = (new UpdatePlan(['c/c', 'a/a', 'b/b']))->merge(new UpdatePlan(['b/b', 'd/d', 'c/c']));

        self::assertSame(['b/b', 'c/c'], $merged->only);
    }

    public function testMergeKeepsTheOnlyRestrictionSortedAndDeduplicated(): void
    {
        self::assertSame(['a/a', 'b/b'], (new UpdatePlan(['b/b', 'a/a', 'b/b']))->merge(new UpdatePlan())->only);
        self::assertSame(['a/a', 'b/b'], (new UpdatePlan())->merge(new UpdatePlan(['b/b', 'a/a', 'a/a']))->only);
    }

    public function testMergeRefusesDisjointRestrictions(): void
    {
        $this->expectException(PlanConflict::class);
        $this->expectExceptionMessage('the update is restricted to disjoint package sets: a/a, b/b and c/c');

        (new UpdatePlan(['b/b', 'a/a']))->merge(new UpdatePlan(['c/c']));
    }

    public function testMergeAccumulatesPinsInPackageOrder(): void
    {
        $merged = (new UpdatePlan(keep: [new Pin('z/z', '1.0.0'), new Pin('a/a', '2.0.0')]))
            ->merge(new UpdatePlan(keep: [new Pin('m/m', '3.0.0'), new Pin('a/a', '2.0.0')]));

        self::assertEquals([new Pin('a/a', '2.0.0'), new Pin('m/m', '3.0.0'), new Pin('z/z', '1.0.0')], $merged->keep);
    }

    public function testMergeRefusesAPackagePinnedTwice(): void
    {
        $this->expectException(PlanConflict::class);
        $this->expectExceptionMessage('drupal/core is pinned at both 10.2.0 and 10.3.0');

        (new UpdatePlan(keep: [new Pin('drupal/core', '10.3.0')]))->merge(new UpdatePlan(keep: [new Pin('drupal/core', '10.2.0')]));
    }

    /**
     * @return iterable<string, array{bool, bool, bool}>
     */
    public static function minimal(): iterable
    {
        yield 'neither' => [false, false, false];
        yield 'the first' => [true, false, true];
        yield 'the second' => [false, true, true];
        yield 'both' => [true, true, true];
    }

    #[DataProvider('minimal')]
    public function testMergeAsksForMinimalChangesIfEitherDoes(bool $a, bool $b, bool $want): void
    {
        self::assertSame($want, (new UpdatePlan(minimalChanges: $a))->merge(new UpdatePlan(minimalChanges: $b))->minimalChanges);
    }

    public function testMergingNothingLeavesNothing(): void
    {
        self::assertEquals(new UpdatePlan(), (new UpdatePlan())->merge(new UpdatePlan()));
    }

    /**
     * @return list<UpdatePlan>
     */
    private static function plans(): array
    {
        return [
            new UpdatePlan(),
            new UpdatePlan(['a/a', 'b/b']),
            new UpdatePlan(['b/b', 'c/c'], [new Pin('x/x', '1')]),
            new UpdatePlan(keep: [new Pin('y/y', '2'), new Pin('x/x', '1')], minimalChanges: true),
            new UpdatePlan(['b/b']),
        ];
    }

    public function testMergeIsCommutativeAndAssociative(): void
    {
        foreach (self::plans() as $p) {
            foreach (self::plans() as $q) {
                self::assertEquals($p->merge($q), $q->merge($p));
                foreach (self::plans() as $r) {
                    self::assertEquals($p->merge($q)->merge($r), $p->merge($q->merge($r)));
                }
            }
            self::assertEquals($p->merge($p), $p->merge($p)->merge($p), 'merging a plan with itself changes nothing more');
        }
    }

    public function testFromJsonReadsTheWireShapeAndToJsonWritesIt(): void
    {
        $json = ['only' => ['a/a'], 'keep' => [['package' => 'b/b', 'version' => '1.0.0']], 'minimal_changes' => true];

        $plan = UpdatePlan::fromJson(json_decode((string) json_encode($json), false));

        self::assertEquals(new UpdatePlan(['a/a'], [new Pin('b/b', '1.0.0')], true), $plan);
        self::assertSame($json, $plan->toJson());
    }

    public function testFromJsonReadsAnythingButTrueAsNoMinimalChanges(): void
    {
        self::assertFalse(UpdatePlan::fromJson(['minimal_changes' => 1])->minimalChanges);
        self::assertEquals(new UpdatePlan(), UpdatePlan::fromJson(null));
    }
}

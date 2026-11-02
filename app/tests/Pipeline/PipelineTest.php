<?php

declare(strict_types=1);

namespace Drupdater\App\Tests\Pipeline;

use Drupdater\App\Agent\AgentError;
use Drupdater\App\Pipeline\Addon;
use Drupdater\App\Pipeline\Commit;
use Drupdater\App\Pipeline\Event;
use Drupdater\App\Pipeline\EventName;
use Drupdater\App\Pipeline\Finding;
use Drupdater\App\Pipeline\HookReply;
use Drupdater\App\Pipeline\Pin;
use Drupdater\App\Pipeline\Pipeline;
use Drupdater\App\Pipeline\Recorded;
use Drupdater\App\Pipeline\Stage;
use Drupdater\App\Pipeline\UpdatePlan;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class PipelineTest extends TestCase
{
    public function testHooksRunByStageThenAddonOrder(): void
    {
        $hooks = new ScriptedHooks();

        $fired = (new Pipeline($hooks, new RecordingCommitter()))->fire('/project', new Event(EventName::PostCodeUpdate), [
            self::addon('audit', [EventName::PostCodeUpdate->value => Stage::Inspect]),
            self::addon('phpcbf', [EventName::PostCodeUpdate->value => Stage::Format]),
            self::addon('bystander', [EventName::PreSiteUpdate->value => Stage::Change]),
            self::addon('rector', [EventName::PostCodeUpdate->value => Stage::Change]),
            self::addon('first', [EventName::PostCodeUpdate->value => Stage::Prepare]),
            self::addon('second change', [EventName::PostCodeUpdate->value => Stage::Change]),
        ], false);

        self::assertSame('', $fired->failure);
        self::assertSame(['first', 'rector', 'second change', 'phpcbf', 'audit'], $hooks->called());
        self::assertSame('/project', $hooks->calls[0]['dir']);
    }

    public function testHooksStopAtTheFirstFailureKeepingWhatEarlierHooksFound(): void
    {
        $hooks = new ScriptedHooks([
            'first' => static fn (): HookReply => new HookReply('found'),
            'broken' => static fn (): HookReply => throw new AgentError('drupdater:hook', 'failed', 'r', 'm'),
        ]);

        $fired = (new Pipeline($hooks, new RecordingCommitter()))->fire('/p', new Event(EventName::PreSiteUpdate, 'default'), [
            self::addon('first', [EventName::PreSiteUpdate->value => Stage::Prepare]),
            self::addon('broken', [EventName::PreSiteUpdate->value => Stage::Change]),
            self::addon('later', [EventName::PreSiteUpdate->value => Stage::Inspect]),
        ], false);

        self::assertSame('pre-site-update hook failed: broken: drupdater:hook: failed (r): m', $fired->failure);
        self::assertSame(['first', 'broken'], $hooks->called());
        self::assertEquals([new Recorded('first', new Finding(EventName::PreSiteUpdate, 'default', 'found', []))], $fired->recorded);
        self::assertNull($fired->plan);
    }

    /**
     * @param array<string, mixed> $want
     */
    #[DataProvider('payloads')]
    public function testEachEventHandsItsHooksItsOwnPayload(Event $event, array $want): void
    {
        $hooks = new ScriptedHooks();

        (new Pipeline($hooks, new RecordingCommitter()))->fire('/p', $event, [self::addon('a', [$event->name->value => Stage::Change])], true);

        self::assertCount(1, $hooks->calls);
        self::assertSame(json_encode($want), json_encode($hooks->calls[0]['event']));
        self::assertTrue($hooks->calls[0]['security']);
    }

    /**
     * @return iterable<string, array{Event, array<string, mixed>}>
     */
    public static function payloads(): iterable
    {
        $facts = (object) ['modules' => ['locale_deploy']];
        yield 'pre-composer-update' => [
            new Event(EventName::PreComposerUpdate, plan: new UpdatePlan(['a/b'], [new Pin('c/d', '1.x-dev abc')])),
            ['name' => 'pre-composer-update', 'plan' => ['only' => ['a/b'], 'keep' => [['package' => 'c/d', 'version' => '1.x-dev abc']], 'minimal_changes' => false]],
        ];
        yield 'pre-composer-update with an empty plan' => [
            new Event(EventName::PreComposerUpdate),
            ['name' => 'pre-composer-update', 'plan' => ['only' => [], 'keep' => [], 'minimal_changes' => false]],
        ];
        yield 'post-composer-update' => [
            new Event(EventName::PostComposerUpdate, baseLock: (object) ['packages' => []]),
            ['name' => 'post-composer-update', 'base_lock' => ['packages' => []]],
        ];
        yield 'post-code-update' => [new Event(EventName::PostCodeUpdate), ['name' => 'post-code-update']];
        yield 'pre-site-update' => [
            new Event(EventName::PreSiteUpdate, 'default', facts: $facts),
            ['name' => 'pre-site-update', 'site' => 'default', 'facts' => ['modules' => ['locale_deploy']]],
        ];
        yield 'post-site-update' => [
            new Event(EventName::PostSiteUpdate, 'second', facts: $facts),
            ['name' => 'post-site-update', 'site' => 'second', 'facts' => ['modules' => ['locale_deploy']]],
        ];
    }

    public function testPreComposerUpdateMergesEveryProposal(): void
    {
        $hooks = new ScriptedHooks([
            'audit' => self::proposing(new UpdatePlan(['drupal/token', 'drupal/core'], minimalChanges: true)),
            'patches' => self::proposing(new UpdatePlan(keep: [new Pin('drupal/token', '1.13.0')])),
        ]);

        $fired = (new Pipeline($hooks, new RecordingCommitter()))->fire('/p', new Event(EventName::PreComposerUpdate), [
            self::addon('audit', [EventName::PreComposerUpdate->value => Stage::Prepare]),
            self::addon('patches', [EventName::PreComposerUpdate->value => Stage::Change]),
            self::addon('bystander', [EventName::PreComposerUpdate->value => Stage::Change]),
        ], false);

        self::assertEquals(new UpdatePlan(['drupal/core', 'drupal/token'], [new Pin('drupal/token', '1.13.0')], true), $fired->plan);
    }

    public function testPreComposerUpdateWithoutProposalsReturnsTheEventsPlan(): void
    {
        $plan = new UpdatePlan(['x/y']);

        $fired = (new Pipeline(new ScriptedHooks(), new RecordingCommitter()))->fire('/p', new Event(EventName::PreComposerUpdate, plan: $plan), [], false);

        self::assertSame($plan, $fired->plan);
    }

    public function testPreComposerUpdateHooksSeeOnlyEarlierStages(): void
    {
        $hooks = new ScriptedHooks([
            'narrow' => self::proposing(new UpdatePlan(['drupal/core'])),
            'first change' => self::proposing(new UpdatePlan(keep: [new Pin('drupal/core', '10.3.0')])),
        ]);

        (new Pipeline($hooks, new RecordingCommitter()))->fire('/p', new Event(EventName::PreComposerUpdate), [
            self::addon('first change', [EventName::PreComposerUpdate->value => Stage::Change]),
            self::addon('inspect', [EventName::PreComposerUpdate->value => Stage::Inspect]),
            self::addon('narrow', [EventName::PreComposerUpdate->value => Stage::Prepare]),
            self::addon('second change', [EventName::PreComposerUpdate->value => Stage::Change]),
        ], false);

        $narrowed = ['only' => ['drupal/core'], 'keep' => [], 'minimal_changes' => false];
        self::assertSame(['only' => [], 'keep' => [], 'minimal_changes' => false], $hooks->eventOf('narrow')['plan']);
        self::assertSame($narrowed, $hooks->eventOf('first change')['plan']);
        self::assertSame($narrowed, $hooks->eventOf('second change')['plan'], 'a stage-mate\'s proposal is not yet settled');
        self::assertSame(['only' => ['drupal/core'], 'keep' => [['package' => 'drupal/core', 'version' => '10.3.0']], 'minimal_changes' => false], $hooks->eventOf('inspect')['plan']);
    }

    public function testPreComposerUpdateFailsWhenProposalsDisagree(): void
    {
        $hooks = new ScriptedHooks([
            'a' => self::proposing(new UpdatePlan(keep: [new Pin('drupal/core', '10.2.0')])),
            'b' => self::proposing(new UpdatePlan(keep: [new Pin('drupal/core', '10.3.0')])),
        ]);

        $fired = (new Pipeline($hooks, new RecordingCommitter()))->fire('/p', new Event(EventName::PreComposerUpdate), [
            self::addon('a', [EventName::PreComposerUpdate->value => Stage::Change]),
            self::addon('b', [EventName::PreComposerUpdate->value => Stage::Change]),
        ], false);

        self::assertSame('pre-composer-update hooks disagree: drupal/core is pinned at both 10.2.0 and 10.3.0', $fired->failure);
        self::assertNull($fired->plan);
        self::assertSame(['a', 'b'], array_map(static fn (Recorded $r): string => $r->addon, $fired->recorded));
    }

    public function testAPlanOnAnyOtherEventIsRefused(): void
    {
        $hooks = new ScriptedHooks(['a' => self::proposing(new UpdatePlan(['x/y']))]);

        $fired = (new Pipeline($hooks, new RecordingCommitter()))->fire('/p', new Event(EventName::PostCodeUpdate), [
            self::addon('a', [EventName::PostCodeUpdate->value => Stage::Change]),
        ], false);

        self::assertSame('post-code-update hook failed: a proposed a plan, which only pre-composer-update reads', $fired->failure);
        self::assertSame([], $fired->recorded);
    }

    public function testOnlyPreComposerUpdateAnswersWithAPlan(): void
    {
        $fired = (new Pipeline(new ScriptedHooks(), new RecordingCommitter()))->fire('/p', new Event(EventName::PostCodeUpdate), [], false);

        self::assertNull($fired->plan);
        self::assertSame(['recorded' => [], 'plan' => null, 'abort' => '', 'failure' => ''], $fired->toJson());
    }

    public function testAnAbortEndsTheEventWithoutFailingItOrRunningLaterHooks(): void
    {
        $hooks = new ScriptedHooks([
            'audit' => static fn (): HookReply => new HookReply(abort: 'No security advisories found'),
        ]);

        $fired = (new Pipeline($hooks, new RecordingCommitter()))->fire('/p', new Event(EventName::PreComposerUpdate), [
            self::addon('audit', [EventName::PreComposerUpdate->value => Stage::Prepare]),
            self::addon('later', [EventName::PreComposerUpdate->value => Stage::Change]),
        ], true);

        self::assertSame('No security advisories found', $fired->abort);
        self::assertSame('', $fired->failure);
        self::assertSame(['audit'], $hooks->called());
        self::assertSame([], $fired->recorded, 'an abort records nothing');
    }

    public function testCommitsAreMadeInOrderBeforeTheNextHookAndRecordedWhenMade(): void
    {
        $committer = new RecordingCommitter(['Update coding styles']);
        $sawCommits = [];
        $hooks = new ScriptedHooks([
            'beautifier' => static fn (): HookReply => new HookReply((object) ['fixable' => 1], [
                new Commit('Add PHPCS config', ['phpcs.xml']),
                new Commit('Update coding styles', ['a.php']),
            ]),
            'inspector' => static function () use ($committer, &$sawCommits): HookReply {
                $sawCommits = $committer->messages();
                return new HookReply();
            },
        ]);

        $fired = (new Pipeline($hooks, $committer))->fire('/p', new Event(EventName::PostCodeUpdate), [
            self::addon('beautifier', [EventName::PostCodeUpdate->value => Stage::Format]),
            self::addon('inspector', [EventName::PostCodeUpdate->value => Stage::Inspect]),
        ], false);

        self::assertSame(['Add PHPCS config', 'Update coding styles'], $sawCommits, 'both are committed, in order, before the next stage runs');
        self::assertSame([['message' => 'Add PHPCS config', 'paths' => ['phpcs.xml']], ['message' => 'Update coding styles', 'paths' => ['a.php']]], $committer->commits);
        self::assertEquals(new Recorded('beautifier', new Finding(EventName::PostCodeUpdate, '', (object) ['fixable' => 1], ['Update coding styles'])), $fired->recorded[0]);
    }

    public function testEveryCommitMadeIsRecorded(): void
    {
        $hooks = new ScriptedHooks([
            'a' => static fn (): HookReply => new HookReply(null, [new Commit('one', ['1']), new Commit('two', ['2'])]),
        ]);

        $fired = (new Pipeline($hooks, new RecordingCommitter(['one', 'two'])))->fire('/p', new Event(EventName::PostCodeUpdate), [
            self::addon('a', [EventName::PostCodeUpdate->value => Stage::Change]),
        ], false);

        self::assertSame(['one', 'two'], $fired->recorded[0]->finding->committed);
    }

    public function testAFailedCommitStopsTheEventAndRecordsNothingOfThatHook(): void
    {
        $hooks = new ScriptedHooks([
            'a' => static fn (): HookReply => new HookReply('x', [new Commit('m', ['x'])]),
        ]);

        $fired = (new Pipeline($hooks, new RecordingCommitter(fail: 'm')))->fire('/p', new Event(EventName::PostSiteUpdate, 'default'), [
            self::addon('a', [EventName::PostSiteUpdate->value => Stage::Change]),
            self::addon('b', [EventName::PostSiteUpdate->value => Stage::Inspect]),
        ], false);

        self::assertSame('post-site-update hook failed: a: failed to commit "m": boom', $fired->failure);
        self::assertSame(['a'], $hooks->called());
        self::assertSame([], $fired->recorded);
    }

    public function testAHookReadsBackOnlyItsOwnAddonsEarlierFindingsInEventAndSiteOrder(): void
    {
        $hooks = new ScriptedHooks();
        $mine = [
            new Finding(EventName::PreSiteUpdate, 'zeta', 'z', []),
            new Finding(EventName::PostCodeUpdate, '', 'c', []),
            new Finding(EventName::PreSiteUpdate, 'alpha', 'a', ['m']),
            new Finding(EventName::PreComposerUpdate, '', 'p', []),
        ];

        (new Pipeline($hooks, new RecordingCommitter()))->fire('/p', new Event(EventName::PostSiteUpdate, 'alpha'), [
            new Addon('mine', [EventName::PostSiteUpdate->value => Stage::Change], $mine),
            new Addon('other', [EventName::PostSiteUpdate->value => Stage::Prepare], [new Finding(EventName::PreComposerUpdate, '', 'theirs', [])]),
        ], false);

        self::assertSame([$mine[3], $mine[1], $mine[2], $mine[0]], $hooks->calls[1]['earlier']);
    }

    /**
     * @param array<string, Stage> $hooks
     */
    private static function addon(string $name, array $hooks): Addon
    {
        return new Addon($name, $hooks);
    }

    /**
     * @return \Closure(): HookReply
     */
    private static function proposing(UpdatePlan $plan): \Closure
    {
        return static fn (): HookReply => new HookReply(plan: $plan);
    }
}

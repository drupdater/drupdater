<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\Catalogue;
use Drupdater\Agent\Addon\CatalogueEntry;
use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\EventName;
use Drupdater\Agent\Addon\Findings;
use Drupdater\Agent\Addon\Paths;
use Drupdater\Agent\Addon\Plan;
use Drupdater\Agent\Addon\SiteFacts;
use Drupdater\Agent\Addon\Stage;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\Protocol;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

/**
 * What every addon builds on: the events, findings, plans and catalogue, and the log the
 * envelope carries.
 */
final class FrameworkTest extends TestCase
{
    public function testFindingsSeparateSharedFromPerSiteAndSortTheSites(): void
    {
        $findings = self::findings([
            ['event' => 'pre-site-update', 'site' => 'zeta', 'finding' => 'z', 'committed' => []],
            ['event' => 'pre-site-update', 'site' => 'alpha', 'finding' => 'a', 'committed' => ['done']],
            ['event' => 'post-code-update', 'site' => '', 'finding' => ['x' => 1], 'committed' => []],
        ]);

        self::assertSame(['alpha', 'zeta'], array_keys($findings->sites(EventName::PreSiteUpdate)));
        self::assertTrue($findings->sites(EventName::PreSiteUpdate)['alpha']->committed('done'));
        self::assertFalse($findings->sites(EventName::PreSiteUpdate)['zeta']->committed('done'));
        self::assertSame(['x' => 1], $findings->shared(EventName::PostCodeUpdate)?->finding());
        self::assertNull($findings->shared(EventName::PreSiteUpdate), 'a per-site finding is not a shared one');
        self::assertNull($findings->shared(EventName::PreComposerUpdate), 'a hook that never ran found nothing');
        self::assertSame([], $findings->sites(EventName::PostSiteUpdate));
    }

    public function testAFindingReadsBackExactlyAsItsHookReturnedIt(): void
    {
        $findings = self::findings([['event' => 'post-code-update', 'site' => '', 'finding' => ['set' => new \stdClass(), 'list' => [], '0' => 'zero'], 'committed' => []]]);

        $finding = $findings->shared(EventName::PostCodeUpdate)?->finding(false);
        self::assertEquals((object) ['set' => new \stdClass(), 'list' => [], '0' => 'zero'], $finding);
    }

    public function testNoFindingsAreNone(): void
    {
        self::assertNull(Findings::fromJson(null)->shared(EventName::PostCodeUpdate));
    }

    /**
     * @return iterable<string, array{mixed}>
     */
    public static function malformedFindings(): iterable
    {
        yield 'not a list' => [(object) ['a' => 1]];
        yield 'a record that is no object' => [['x']];
        yield 'an unknown event' => [[(object) ['event' => 'later', 'site' => '', 'finding' => null, 'committed' => []]]];
        yield 'a site that is no string' => [[(object) ['event' => 'post-code-update', 'site' => 1, 'finding' => null, 'committed' => []]]];
        yield 'commits that are no strings' => [[(object) ['event' => 'post-code-update', 'site' => '', 'finding' => null, 'committed' => [1]]]];
        yield 'no finding at all' => [[(object) ['event' => 'post-code-update', 'site' => '', 'committed' => []]]];
    }

    #[DataProvider('malformedFindings')]
    public function testMalformedFindingsAreAnInvalidRequest(mixed $records): void
    {
        $this->expectExceptionObject(new Failure(Failure::INVALID_REQUEST, is_array($records) ? 'each finding must have a known event, a site, the finding and what it committed' : 'findings must be a list'));
        Findings::fromJson($records);
    }

    public function testAPlanRoundTrips(): void
    {
        $plan = ['only' => ['a/b'], 'keep' => [['package' => 'c/d', 'version' => '1.x-dev abc']], 'minimal_changes' => true];

        self::assertSame($plan, Plan::fromArray($plan)->toArray());
        self::assertSame(['only' => [], 'keep' => [], 'minimal_changes' => false], Plan::fromArray([])->toArray());
    }

    /**
     * @return iterable<string, array{mixed, string}>
     */
    public static function malformedPlans(): iterable
    {
        yield 'not an object' => ['x', 'plan must be an object'];
        yield 'only not strings' => [['only' => [1]], 'plan.only must be a list of strings'];
        yield 'keep not a list' => [['keep' => ['a' => []]], 'plan.keep must be a list'];
        yield 'a pin without a version' => [['keep' => [['package' => 'a/b']]], 'each plan.keep must have a package and a version'];
    }

    #[DataProvider('malformedPlans')]
    public function testAMalformedPlanIsAnInvalidRequest(mixed $plan, string $reason): void
    {
        $this->expectExceptionObject(new Failure(Failure::INVALID_REQUEST, $reason));
        Plan::fromArray($plan);
    }

    public function testEachEventCarriesItsOwnPayloadOnly(): void
    {
        $pre = Event::fromRequest(['name' => 'pre-composer-update', 'plan' => ['only' => ['a/b']]], '/p', true, new Findings());
        $post = Event::fromRequest(['name' => 'post-composer-update', 'base_lock' => ['packages' => []]], '/p', false, new Findings());
        $site = Event::fromRequest(['name' => 'post-site-update', 'site' => 'default', 'facts' => ['modules' => ['node']]], '/p', false, new Findings());

        self::assertSame(['a/b'], $pre->plan()->only);
        self::assertTrue($pre->security);
        self::assertSame(['packages' => []], $post->baseLock());
        self::assertSame('default', $site->site());
        self::assertTrue($site->facts()->moduleEnabled('node'));
        self::assertFalse($site->facts()->moduleEnabled('locale'));

        foreach ([static fn () => $post->plan(), static fn () => $pre->baseLock(), static fn () => $pre->site(), static fn () => $post->facts()] as $wrong) {
            try {
                $wrong();
                self::fail('an event handed out a payload it does not carry');
            }
            catch (\LogicException) {
                self::addToAssertionCount(1);
            }
        }
    }

    /**
     * @return iterable<string, array{array<string, mixed>, string}>
     */
    public static function malformedEvents(): iterable
    {
        yield 'post-composer-update without its lock' => [['name' => 'post-composer-update'], 'base_lock must be the composer.lock the update started from'];
        yield 'a site event without its site' => [['name' => 'pre-site-update', 'facts' => []], 'a per-site event must name its site'];
        yield 'a site event without its facts' => [['name' => 'pre-site-update', 'site' => 'default'], 'facts must be an object'];
        yield 'facts with a path that is no string' => [['name' => 'pre-site-update', 'site' => 'default', 'facts' => ['translation_path' => 1]], 'facts.translation_path must be a string'];
        yield 'facts with modules that are no strings' => [['name' => 'pre-site-update', 'site' => 'default', 'facts' => ['modules' => [1]]], 'facts.modules, facts.update_hooks and facts.unsupported_modules are malformed'];
    }

    /**
     * @param array<string, mixed> $event
     */
    #[DataProvider('malformedEvents')]
    public function testAMalformedEventIsAnInvalidRequest(array $event, string $reason): void
    {
        $this->expectExceptionObject(new Failure(Failure::INVALID_REQUEST, $reason));
        Event::fromRequest($event, '/p', false, new Findings());
    }

    public function testASiteUpdateNeedsAPerSiteEvent(): void
    {
        $this->expectException(\LogicException::class);
        Event::siteUpdate(EventName::PostCodeUpdate, '/p', 'default', new SiteFacts());
    }

    public function testTheBuiltinCatalogueHasEveryAddonOnceAndFindsThemByName(): void
    {
        $catalogue = Catalogue::builtin();
        $names = array_map(static fn (CatalogueEntry $entry): string => $entry->name(), $catalogue->entries());

        self::assertCount(10, array_unique($names));
        foreach ($names as $name) {
            self::assertSame($name, $catalogue->find($name)?->name());
        }
        self::assertNull($catalogue->find('nope'));
    }

    public function testEveryHookOfTheCatalogueIsOfAKnownEventAtAStage(): void
    {
        foreach (Catalogue::builtin()->entries() as $entry) {
            self::assertNotSame([], $entry->addon::hooks(), $entry->name());
            foreach ($entry->addon::hooks() as $event => $stage) {
                self::assertNotNull(EventName::tryFrom($event), $entry->name());
                self::assertInstanceOf(Stage::class, $stage);
            }
        }
    }

    public function testACatalogueRefusesTwoAddonsOfOneName(): void
    {
        $this->expectException(\LogicException::class);
        new Catalogue([new CatalogueEntry(ScriptedAddon::class), new CatalogueEntry(ScriptedAddon::class)]);
    }

    public function testAMandatoryAddonIsNoDefault(): void
    {
        $this->expectException(\LogicException::class);
        new CatalogueEntry(ScriptedAddon::class, mandatory: true, enabledByDefault: true);
    }

    public function testTheLogAddsItsContextUnderEachEntrysOwn(): void
    {
        $log = new Log(['addon' => 'a', 'site' => 'shared']);
        $log->debug('d');
        $log->info('i', ['site' => 'own']);
        $log->warning('w');
        $log->error('e');

        self::assertSame([
            ['level' => 'debug', 'message' => 'd', 'context' => ['addon' => 'a', 'site' => 'shared']],
            ['level' => 'info', 'message' => 'i', 'context' => ['site' => 'own', 'addon' => 'a']],
            ['level' => 'warning', 'message' => 'w', 'context' => ['addon' => 'a', 'site' => 'shared']],
            ['level' => 'error', 'message' => 'e', 'context' => ['addon' => 'a', 'site' => 'shared']],
        ], $log->entries());
    }

    public function testTheEnvelopeCarriesTheLogOnlyWhenThereIsOne(): void
    {
        self::assertSame('{"protocol":3,"command":"c","ok":true,"result":{}}', Protocol::success('c', []));
        self::assertSame(
            '{"protocol":3,"command":"c","ok":true,"result":{},"log":[{"level":"info","message":"é → ü","context":{}}]}',
            Protocol::success('c', [], [['level' => 'info', 'message' => 'é → ü', 'context' => []]]),
            'unescaped, as Go writes JSON, and an empty context still an object',
        );
    }

    /**
     * @return iterable<string, array{string, string}>
     */
    public static function drushPaths(): iterable
    {
        yield 'inside the project' => ['/project/web/sites/default/files/translations', 'web/sites/default/files/translations'];
        yield 'the project itself' => ['/project', '.'];
        yield 'a sibling with the same prefix' => ['/project-backup/translations', '/project-backup/translations'];
        yield 'already relative' => ['translations', 'translations'];
        yield 'outside the project' => ['/elsewhere/translations', '/elsewhere/translations'];
    }

    #[DataProvider('drushPaths')]
    public function testPathsAreMadeRelativeToTheProjectOnlyWhenInsideIt(string $path, string $relative): void
    {
        self::assertSame($relative, Paths::relativeTo('/project/', $path));
    }

    /**
     * @param list<array<string, mixed>> $records
     */
    private static function findings(array $records): Findings
    {
        return Findings::fromJson(json_decode((string) json_encode($records), false));
    }
}

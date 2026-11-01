<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\Event;
use Drupdater\Agent\Addon\EventName;
use Drupdater\Agent\Addon\SiteFacts;
use Drupdater\Agent\Addon\TranslationsUpdater;
use Drupdater\Agent\Addon\UnsupportedModules;
use Drupdater\Agent\Addon\UpdateHooks;
use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\Tests\Scratch;
use Drupdater\Agent\Tools\Drush;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

/**
 * The addons that read a site's facts: update_hooks, unsupported_modules, translations_updater.
 */
final class SiteAddonsTest extends TestCase
{
    use Scratch;

    private const HOOK = ['module' => 'token', 'update_id' => 8002, 'description' => 'Rebuild the token cache', 'type' => 'hook_update_n'];

    public function testUpdateHooksFindsTheSitesPendingHooksAndCountsThem(): void
    {
        $log = new Log();

        $result = (new UpdateHooks($log))->handle(self::preSite('default', new SiteFacts(updateHooks: ['token_update_8002' => self::HOOK])));

        self::assertEquals((object) ['token_update_8002' => self::HOOK], $result->finding);
        self::assertSame(['level' => 'info', 'message' => 'update hooks found', 'context' => ['site' => 'default', 'count' => 1]], $log->entries()[1]);
    }

    public function testUpdateHooksOfASiteWithoutAnyIsAnEmptyObject(): void
    {
        $result = (new UpdateHooks(new Log()))->handle(self::preSite('default', new SiteFacts()));

        self::assertSame('{}', json_encode($result->finding));
    }

    public function testUpdateHooksReportsBySiteAndHookLeavingOutSitesWithNone(): void
    {
        $section = UpdateHooks::report(Records::findings([
            Records::record('pre-site-update', ['z_update_1' => self::HOOK, 'a_update_1' => self::HOOK], 'second'),
            Records::record('pre-site-update', new \stdClass(), 'empty'),
            Records::record('pre-site-update', ['token_update_8002' => self::HOOK], 'default'),
        ]));

        self::assertSame(
            '{"default":{"token_update_8002":' . json_encode(self::HOOK) . '},"second":{"a_update_1":' . json_encode(self::HOOK) . ',"z_update_1":' . json_encode(self::HOOK) . '}}',
            json_encode($section),
        );
        self::assertNull(UpdateHooks::report(Records::findings([Records::record('pre-site-update', new \stdClass(), 'default')])));
    }

    public function testUnsupportedModulesFindsTheSitesModules(): void
    {
        $modules = [['name' => 'old', 'installed_version' => '1.0.0', 'recommended_version' => 'None']];
        $log = new Log();

        $result = (new UnsupportedModules($log))->handle(self::preSite('default', new SiteFacts(unsupportedModules: $modules)));

        self::assertSame($modules, $result->finding);
        self::assertSame([['level' => 'info', 'message' => 'unsupported modules found', 'context' => ['site' => 'default', 'count' => 1]]], $log->entries());
    }

    public function testUnsupportedModulesWarnsAboutAnUnreachableStatusServiceAndFindsNothing(): void
    {
        $log = new Log();

        $result = (new UnsupportedModules($log))->handle(self::preSite('default', new SiteFacts(unsupportedModulesError: 'timed out')));

        self::assertSame([], $result->finding);
        self::assertSame([['level' => 'warning', 'message' => 'failed to check for unsupported modules', 'context' => ['site' => 'default', 'error' => 'timed out']]], $log->entries());
    }

    public function testUnsupportedModulesReportsEachModuleOnceSortedByName(): void
    {
        $legacy = ['name' => 'legacy_feature', 'installed_version' => '1.2.0', 'recommended_version' => 'None'];
        $widget = ['name' => 'old_widget', 'installed_version' => '2.0.0', 'recommended_version' => '3.1.0'];

        self::assertSame([$legacy, $widget], UnsupportedModules::report(Records::findings([
            Records::record('pre-site-update', [$legacy], 'default'),
            Records::record('pre-site-update', [$widget, $legacy], 'second'),
        ])));
        self::assertNull(UnsupportedModules::report(Records::findings([Records::record('pre-site-update', [], 'default')])));
    }

    public function testUnsupportedModulesTakesAModuleFromTheLastSiteByName(): void
    {
        $section = UnsupportedModules::report(Records::findings([
            Records::record('pre-site-update', [['name' => 'm', 'installed_version' => '2.0', 'recommended_version' => 'None']], 'zeta'),
            Records::record('pre-site-update', [['name' => 'm', 'installed_version' => '1.0', 'recommended_version' => 'None']], 'alpha'),
        ]));

        self::assertSame('2.0', $section[0]['installed_version'], 'the same answer whichever order the sites finished in');
    }

    public function testTranslationsSkipASiteWithoutLocaleDeployAndSaySo(): void
    {
        $drush = $this->createMock(Drush::class);
        $drush->expects(self::never())->method('localizeTranslations');

        $result = (new TranslationsUpdater(new Log(), $drush))->handle(self::postSite('/p', new SiteFacts(modules: ['locale'])));

        self::assertSame(['skipped' => 'locale_deploy not enabled'], $result->finding);
        self::assertSame([], $result->commits);
    }

    public function testTranslationsImportAndCommitTheTranslationDirectory(): void
    {
        $project = $this->scratchDir();
        mkdir($project . '/translations');
        $drush = $this->createMock(Drush::class);
        $drush->expects(self::once())->method('localizeTranslations')->with('example.com');

        $result = (new TranslationsUpdater(new Log(), $drush))->handle(self::postSite($project, new SiteFacts(translationPath: $project . '/translations', modules: ['locale_deploy']), 'example.com'));

        self::assertSame(['path' => 'translations'], $result->finding);
        self::assertSame([['message' => 'Update translations', 'paths' => ['translations']]], array_map(static fn ($c) => $c->toArray(), $result->commits));
    }

    /**
     * @return iterable<string, array{string}>
     */
    public static function unavailablePaths(): iterable
    {
        yield 'not configured' => [''];
        yield 'never created' => ['/does/not/exist'];
        yield 'not a directory' => ['/dev/null'];
    }

    /**
     * A soft skip, never a commit: an empty path would commit the whole working tree.
     */
    #[DataProvider('unavailablePaths')]
    public function testTranslationsSkipAnUnavailableTranslationPath(string $path): void
    {
        $drush = $this->createMock(Drush::class);
        $drush->expects(self::once())->method('localizeTranslations');

        $result = (new TranslationsUpdater(new Log(), $drush))->handle(self::postSite('/p', new SiteFacts(translationPath: $path, modules: ['locale_deploy'])));

        self::assertSame([], $result->commits);
        self::assertStringStartsWith('translation path not available: ', $result->finding['skipped']);
    }

    public function testAFailedImportFailsTheHook(): void
    {
        $drush = $this->createMock(Drush::class);
        $drush->method('localizeTranslations')->willThrowException(new Failure(Failure::FAILED, 'drush failed'));

        $this->expectExceptionObject(new Failure(Failure::FAILED, 'drush failed'));
        (new TranslationsUpdater(new Log(), $drush))->handle(self::postSite('/p', new SiteFacts(modules: ['locale_deploy'])));
    }

    public function testTranslationsReportUpdatedOnlyWhereTheCommitWasMade(): void
    {
        self::assertSame('{"default":{"path":"translations","updated":true},"nothing":{"path":"translations","updated":false},"second":{"updated":false,"skipped":"locale_deploy not enabled"}}', json_encode(TranslationsUpdater::report(Records::findings([
            Records::record('post-site-update', ['skipped' => 'locale_deploy not enabled'], 'second'),
            Records::record('post-site-update', ['path' => 'translations'], 'default', ['Update translations']),
            Records::record('post-site-update', ['path' => 'translations'], 'nothing'),
        ]))));
        self::assertNull(TranslationsUpdater::report(Records::findings()));
    }

    private static function preSite(string $site, SiteFacts $facts): Event
    {
        return Event::siteUpdate(EventName::PreSiteUpdate, '/p', $site, $facts);
    }

    private static function postSite(string $project, SiteFacts $facts, string $site = 'default'): Event
    {
        return Event::siteUpdate(EventName::PostSiteUpdate, $project, $site, $facts);
    }
}

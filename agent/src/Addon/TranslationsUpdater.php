<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Log;
use Drupdater\Agent\Tools\Drush;

/**
 * translations_updater: imports each site's translations through locale_deploy and commits the
 * translation directory.
 */
final class TranslationsUpdater implements Addon
{
    public const COMMIT_MESSAGE = 'Update translations';

    public function __construct(private readonly Log $log, private readonly Drush $drush)
    {
    }

    public static function name(): string
    {
        return 'translations_updater';
    }

    public static function hooks(): array
    {
        return [EventName::PostSiteUpdate->value => Stage::Change];
    }

    public static function create(Services $services): static
    {
        return new self($services->log, $services->drush());
    }

    /**
     * A skipped site says why: omitted, it would read the same as one that found nothing new.
     * Whether the translations changed is up to the commit, so the report reads it from there.
     */
    public function handle(Event $event): HookResult
    {
        $facts = $event->facts();
        if (!$facts->moduleEnabled('locale_deploy')) {
            $this->log->info('locale_deploy not enabled, skipping translations update', ['site' => $event->site()]);
            return self::skipped('locale_deploy not enabled');
        }

        $this->log->info('updating translations');
        $this->drush->localizeTranslations($event->site());

        $unavailable = self::unavailable($facts->translationPath);
        if ($unavailable !== null) {
            $this->log->info('translation path not available, skipping translations update', ['site' => $event->site(), 'error' => $unavailable]);
            return self::skipped('translation path not available: ' . $unavailable);
        }
        $path = Paths::relativeTo($event->projectDir, $facts->translationPath);
        return new HookResult(['path' => $path], [new Commit(self::COMMIT_MESSAGE, [$path])]);
    }

    public static function report(Findings $findings): mixed
    {
        $sites = [];
        foreach ($findings->sites(EventName::PostSiteUpdate) as $site => $recorded) {
            $finding = (array) $recorded->finding();
            $result = [];
            if (($finding['path'] ?? '') !== '') {
                $result['path'] = $finding['path'];
            }
            $result['updated'] = $recorded->committed(self::COMMIT_MESSAGE);
            if (($finding['skipped'] ?? '') !== '') {
                $result['skipped'] = $finding['skipped'];
            }
            $sites[$site] = $result;
        }
        return $sites === [] ? null : (object) $sites;
    }

    private static function skipped(string $reason): HookResult
    {
        return new HookResult(['skipped' => $reason]);
    }

    // An empty path would reach git as the whole working tree.
    private static function unavailable(string $path): ?string
    {
        if ($path === '') {
            return 'no translation path is configured';
        }
        if (!is_dir($path)) {
            return sprintf('%s is not an existing directory', $path);
        }
        return null;
    }
}

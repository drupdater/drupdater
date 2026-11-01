<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Log;

/**
 * unsupported_modules: the modules Drupal's update status service reports as NOT_SUPPORTED, so
 * no fix is coming for them. The merge request lists them with composer_audit's abandoned packages.
 */
final class UnsupportedModules implements Addon
{
    public function __construct(private readonly Log $log)
    {
    }

    public static function name(): string
    {
        return 'unsupported_modules';
    }

    public static function hooks(): array
    {
        return [EventName::PreSiteUpdate->value => Stage::Inspect];
    }

    public static function create(Services $services): static
    {
        return new self($services->log);
    }

    /**
     * Best-effort: an unreachable status service is logged, never an error.
     */
    public function handle(Event $event): HookResult
    {
        $facts = $event->facts();
        if ($facts->unsupportedModulesError !== '') {
            $this->log->warning('failed to check for unsupported modules', ['site' => $event->site(), 'error' => $facts->unsupportedModulesError]);
            return new HookResult([]);
        }
        if ($facts->unsupportedModules !== []) {
            $this->log->info('unsupported modules found', ['site' => $event->site(), 'count' => count($facts->unsupportedModules)]);
        }
        return new HookResult($facts->unsupportedModules);
    }

    /**
     * One entry per module however many sites have it, the last site in name order winning, and
     * sorted by name so an unchanged project reports byte-identically.
     */
    public static function report(Findings $findings): mixed
    {
        $byName = [];
        foreach ($findings->sites(EventName::PreSiteUpdate) as $recorded) {
            foreach ((array) $recorded->finding() as $module) {
                $byName[(string) $module['name']] = [
                    'name' => (string) $module['name'],
                    'installed_version' => (string) ($module['installed_version'] ?? ''),
                    'recommended_version' => (string) ($module['recommended_version'] ?? ''),
                ];
            }
        }
        if ($byName === []) {
            return null;
        }
        ksort($byName, SORT_STRING);
        return array_values($byName);
    }
}

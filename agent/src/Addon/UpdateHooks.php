<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Log;

/**
 * update_hooks: the database updates each site has pending before the update runs them, which
 * the reviewer needs to know about as much as about the code.
 */
final class UpdateHooks implements Addon
{
    public function __construct(private readonly Log $log)
    {
    }

    public static function name(): string
    {
        return 'update_hooks';
    }

    public static function hooks(): array
    {
        return [EventName::PreSiteUpdate->value => Stage::Inspect];
    }

    public static function create(Services $services): static
    {
        return new self($services->log);
    }

    public function handle(Event $event): HookResult
    {
        $hooks = $event->facts()->updateHooks;
        $this->log->debug('update hooks', ['hooks' => $hooks]);
        if ($hooks !== []) {
            $this->log->info('update hooks found', ['site' => $event->site(), 'count' => count($hooks)]);
        }
        return new HookResult((object) $hooks);
    }

    /**
     * By site, then by hook: in a multisite the same module can have different hooks per site.
     */
    public static function report(Findings $findings): mixed
    {
        $sites = [];
        foreach ($findings->sites(EventName::PreSiteUpdate) as $site => $recorded) {
            $hooks = (array) $recorded->finding();
            if ($hooks === []) {
                continue;
            }
            ksort($hooks, SORT_STRING);
            $sites[$site] = (object) $hooks;
        }
        return $sites === [] ? null : (object) $sites;
    }
}

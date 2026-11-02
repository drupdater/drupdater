<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

use Drupdater\App\Agent\Agent;

/**
 * Each hook as its own `composer drupdater:hook` process: an addon keeps no state between hooks,
 * and two hooks never share a process.
 */
final class AgentHooks implements Hooks
{
    public function __construct(private readonly Agent $agent)
    {
    }

    public function run(string $dir, string $addon, array $event, array $earlier, bool $security): HookReply
    {
        return HookReply::fromJson($this->agent->call($dir, 'drupdater:hook', [
            'addon' => $addon,
            'event' => $event,
            'earlier' => array_map(static fn (Finding $finding): array => $finding->toJson(), $earlier),
            'run' => ['security' => $security],
        ]));
    }
}

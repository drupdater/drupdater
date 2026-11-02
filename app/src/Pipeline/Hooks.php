<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

use Drupdater\App\Agent\AgentError;

/**
 * Runs one addon's hook for one event in the project in dir.
 */
interface Hooks
{
    /**
     * @param array<string, mixed> $event the event's payload
     * @param list<Finding> $earlier what the addon's own hooks found earlier in the run
     * @throws AgentError
     */
    public function run(string $dir, string $addon, array $event, array $earlier, bool $security): HookReply;
}

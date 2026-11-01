<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * What one hook returns: drupdater commits the commits in order, records the finding for the
 * addon's report section, and logs each log line at info level.
 */
final class HookResult
{
    /**
     * @param list<array{message: string, paths: list<string>}> $commits
     * @param list<string> $log
     */
    public function __construct(
        public readonly mixed $finding = null,
        public readonly array $commits = [],
        public readonly array $log = [],
    ) {
    }

    /**
     * @return array{commits: list<array{message: string, paths: list<string>}>, finding: mixed, log: list<string>}
     */
    public function toArray(): array
    {
        return ['commits' => $this->commits, 'finding' => $this->finding, 'log' => $this->log];
    }
}

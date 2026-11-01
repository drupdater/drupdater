<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * What one hook returns: what it found, for its addon's report section and later hooks; the
 * commits it asks for; and, on pre-composer-update only, its proposal for the update.
 */
final class HookResult
{
    /**
     * @param mixed $finding anything JSON can hold; null when the hook found nothing
     * @param list<Commit> $commits
     */
    public function __construct(
        public readonly mixed $finding = null,
        public readonly array $commits = [],
        public readonly ?Plan $plan = null,
    ) {
    }

    /**
     * @return array{finding: mixed, commits: list<array{message: string, paths: list<string>}>, plan: array<string, mixed>|null}
     */
    public function toArray(): array
    {
        return [
            'finding' => $this->finding,
            'commits' => array_map(static fn (Commit $commit): array => $commit->toArray(), $this->commits),
            'plan' => $this->plan?->toArray(),
        ];
    }
}

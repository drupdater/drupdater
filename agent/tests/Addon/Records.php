<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests\Addon;

use Drupdater\Agent\Addon\Findings;

/**
 * Findings as drupdater would send them back, built from what the hooks returned.
 */
final class Records
{
    /**
     * @param list<string> $committed
     * @return array{event: string, site: string, finding: mixed, committed: list<string>}
     */
    public static function record(string $event, mixed $finding, string $site = '', array $committed = []): array
    {
        return ['event' => $event, 'site' => $site, 'finding' => $finding, 'committed' => $committed];
    }

    /**
     * @param list<array{event: string, site: string, finding: mixed, committed: list<string>}> $records
     */
    public static function findings(array $records = []): Findings
    {
        return Findings::fromJson(json_decode((string) json_encode($records, JSON_THROW_ON_ERROR), false));
    }

    /**
     * A finding as it round-trips through drupdater: JSON-encoded, then decoded as arrays.
     */
    public static function roundTrip(mixed $finding): mixed
    {
        return json_decode((string) json_encode($finding, JSON_THROW_ON_ERROR), true);
    }
}

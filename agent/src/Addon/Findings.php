<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Failure;

/**
 * What one addon's hooks returned over a run, as drupdater sends it back: a hook reads its own
 * addon's earlier findings, never another's.
 */
final class Findings
{
    /**
     * @param list<Recorded> $recorded
     */
    public function __construct(private readonly array $recorded = [])
    {
    }

    /**
     * Decoded as objects: a finding must read back exactly as its hook returned it.
     *
     * @throws Failure when the list is malformed
     */
    public static function fromJson(mixed $records): self
    {
        if ($records === null) {
            return new self();
        }
        if (!is_array($records) || !array_is_list($records)) {
            throw new Failure(Failure::INVALID_REQUEST, 'findings must be a list');
        }
        $recorded = [];
        foreach ($records as $record) {
            $event = $record instanceof \stdClass && is_string($record->event ?? null) ? EventName::tryFrom($record->event) : null;
            $site = $record instanceof \stdClass ? ($record->site ?? '') : null;
            $committed = $record instanceof \stdClass ? ($record->committed ?? []) : null;
            if ($event === null || !is_string($site) || !is_array($committed) || array_filter($committed, 'is_string') !== $committed
                || !property_exists($record, 'finding')) {
                throw new Failure(Failure::INVALID_REQUEST, 'each finding must have a known event, a site, the finding and what it committed');
            }
            $recorded[] = new Recorded($event, $site, json_encode($record->finding, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE), array_values($committed));
        }
        return new self($recorded);
    }

    /**
     * The finding of an event that is not per site, or null when its hook never ran.
     */
    public function shared(EventName $event): ?Recorded
    {
        foreach ($this->recorded as $recorded) {
            if ($recorded->event === $event && $recorded->site === '') {
                return $recorded;
            }
        }
        return null;
    }

    /**
     * Every site's finding of a per-site event, by site name, sorted.
     *
     * @return array<string, Recorded>
     */
    public function sites(EventName $event): array
    {
        $sites = [];
        foreach ($this->recorded as $recorded) {
            if ($recorded->event === $event && $recorded->site !== '') {
                $sites[$recorded->site] = $recorded;
            }
        }
        ksort($sites, SORT_STRING);
        return $sites;
    }
}

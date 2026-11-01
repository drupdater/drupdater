<?php

declare(strict_types=1);

namespace Drupdater\Agent\DrupalOrg;

/**
 * A drupal.org issue, as its node API describes it.
 */
final class Issue
{
    /** Fixed, Closed (fixed) and Patch (to be ported): the fix has landed upstream. */
    private const FIXED_STATUSES = ['2', '7', '15'];

    public function __construct(
        public readonly string $id,
        public readonly string $title,
        public readonly string $status,
        public readonly string $url,
        public readonly string $project,
    ) {
    }

    public function fixed(): bool
    {
        return in_array($this->status, self::FIXED_STATUSES, true);
    }
}

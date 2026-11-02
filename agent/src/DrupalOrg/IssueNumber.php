<?php

declare(strict_types=1);

namespace Drupdater\Agent\DrupalOrg;

/**
 * Finds a drupal.org issue number in a patch's description or file name.
 */
final class IssueNumber
{
    /** The first run of six or more digits: shorter ones are comment numbers and versions. */
    public static function find(string $text): ?string
    {
        return preg_match('/\d{6,}/', $text, $match) === 1 ? $match[0] : null;
    }
}

<?php

declare(strict_types=1);

namespace Drupdater\Agent\DrupalOrg;

/**
 * A merge request into a project from an issue's fork.
 */
final class ForkMergeRequest
{
    public function __construct(public readonly string $sha, public readonly string $webUrl)
    {
    }
}

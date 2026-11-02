<?php

declare(strict_types=1);

namespace Drupdater\App\Git;

/**
 * Who drupdater's commits are by.
 */
final class Identity
{
    public function __construct(public readonly string $name, public readonly string $email)
    {
    }
}

<?php

declare(strict_types=1);

namespace Drupdater\App\Pipeline;

/**
 * Holds a package at a version as the lock prints it.
 */
final class Pin
{
    public function __construct(public readonly string $package, public readonly string $version)
    {
    }

    /**
     * @return array{package: string, version: string}
     */
    public function toJson(): array
    {
        return ['package' => $this->package, 'version' => $this->version];
    }
}

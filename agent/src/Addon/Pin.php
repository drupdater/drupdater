<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

/**
 * Holds a package at a version, as the lock prints it, whatever else the update does.
 */
final class Pin
{
    public function __construct(public readonly string $package, public readonly string $version)
    {
    }

    /**
     * @return array{package: string, version: string}
     */
    public function toArray(): array
    {
        return ['package' => $this->package, 'version' => $this->version];
    }
}

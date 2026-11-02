<?php

declare(strict_types=1);

namespace Drupdater\Agent\Project;

/**
 * The project as ProjectFacts read it. A snapshot: read it again once anything rewrote
 * composer.json or the lock.
 */
final class Project
{
    /**
     * @param list<string> $customCodeDirectories relative to the project
     * @param array<string, string> $packages every locked package, dev ones included, by name: its version
     * @param mixed $patches extra.patches as written, key order and empty objects intact; null when absent
     * @param array<string, list<string>> $dependencyPatches the patch files locked dependencies declare, by target package
     * @param ?string $lockHash the lock's content-hash; null without a lock
     */
    public function __construct(
        public readonly ?string $webRoot = null,
        public readonly array $customCodeDirectories = [],
        public readonly array $packages = [],
        public readonly mixed $patches = null,
        public readonly array $dependencyPatches = [],
        public readonly ?string $lockHash = null,
    ) {
    }

    public function installed(string $package): bool
    {
        return array_key_exists($package, $this->packages);
    }

    /** The locked version, "" when the package is not locked. */
    public function version(string $package): string
    {
        return $this->packages[$package] ?? '';
    }
}

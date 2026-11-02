<?php

declare(strict_types=1);

namespace Drupdater\Agent\Patches;

/**
 * What composer_patches did to the patches over one update, in its report section's own names.
 */
final class PatchUpdates
{
    /** @var list<array{package: string, patch_description: string, patch_path: string, reason: string}> */
    public array $removed = [];
    /** @var list<array{package: string, patch_description: string, previous_patch_path: string, new_patch_path: string}> */
    public array $updated = [];
    /** @var list<array{package: string, fixed_version: string, patch_path: string, patch_description: string, new_version: string}> */
    public array $conflicts = [];

    public function remove(string $package, string $description, string $path, string $reason): void
    {
        $this->removed[] = ['package' => $package, 'patch_description' => $description, 'patch_path' => $path, 'reason' => $reason];
    }

    public function update(string $package, string $description, string $previousPath, string $newPath): void
    {
        $this->updated[] = ['package' => $package, 'patch_description' => $description, 'previous_patch_path' => $previousPath, 'new_patch_path' => $newPath];
    }

    /** A package held back at its current version because a patch no longer applies. */
    public function conflict(string $package, string $fixedVersion, string $newVersion, string $path, string $description): void
    {
        $this->conflicts[] = ['package' => $package, 'fixed_version' => $fixedVersion, 'patch_path' => $path, 'patch_description' => $description, 'new_version' => $newVersion];
    }

    public function changes(): bool
    {
        return $this->removed !== [] || $this->updated !== [] || $this->conflicts !== [];
    }

    /**
     * The given paths, plus every local patch file removed or added.
     *
     * @param list<string> $paths
     * @param \Closure(string): bool $remote
     * @return list<string>
     */
    public function changedPaths(array $paths, \Closure $remote): array
    {
        foreach ($this->removed as $removed) {
            $paths[] = $removed['patch_path'];
        }
        foreach ($this->updated as $updated) {
            $paths[] = $updated['previous_patch_path'];
            $paths[] = $updated['new_patch_path'];
        }
        return array_values(array_filter($paths, static fn (string $path): bool => !$remote($path)));
    }

    /**
     * @return array{removed: list<array<string, string>>, updated: list<array<string, string>>, conflicts: list<array<string, string>>}
     */
    public function toArray(): array
    {
        return ['removed' => $this->removed, 'updated' => $this->updated, 'conflicts' => $this->conflicts];
    }
}

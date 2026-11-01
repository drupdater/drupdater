<?php

declare(strict_types=1);

namespace Drupdater\Agent\Patches;

use Drupdater\Agent\Failure;

/**
 * extra.patches in declaration order: composer-patches 1.x applies a package's patches in that
 * order, so neither checking nor writing them back may reorder them. A package keeps its place
 * with no patches left only until it is removed.
 */
final class DeclaredPatches
{
    /**
     * @param array<string, list<array{description: string, path: string}>> $sets by package, in order
     */
    private function __construct(private array $sets)
    {
    }

    /**
     * @param mixed $patches extra.patches as composer.json writes it; null when absent
     *
     * @throws Failure when it is not an object of objects of paths
     */
    public static function fromJson(mixed $patches): self
    {
        if ($patches === null) {
            return new self([]);
        }
        if (!$patches instanceof \stdClass) {
            throw new Failure(Failure::FAILED, 'extra.patches is not an object');
        }
        $sets = [];
        foreach (get_object_vars($patches) as $package => $set) {
            if (!$set instanceof \stdClass) {
                throw new Failure(Failure::FAILED, sprintf('the patches of %s are not an object', $package));
            }
            $sets[(string) $package] = [];
            foreach (get_object_vars($set) as $description => $path) {
                if (!is_string($path)) {
                    throw new Failure(Failure::FAILED, sprintf('a patch of %s is not a path', $package));
                }
                $sets[(string) $package][] = ['description' => (string) $description, 'path' => $path];
            }
        }
        return new self($sets);
    }

    /**
     * @return list<string>
     */
    public function packages(): array
    {
        return array_map('strval', array_keys($this->sets));
    }

    /**
     * @return list<array{description: string, path: string}>
     */
    public function of(string $package): array
    {
        return $this->sets[$package] ?? [];
    }

    /**
     * Gives a patch a new description in place. A description another patch of the package
     * already has is refused: JSON keys are unique, so one of the two would be lost.
     */
    public function rename(string $package, string $from, string $to): bool
    {
        foreach ($this->of($package) as $patch) {
            if ($patch['description'] === $to) {
                return false;
            }
        }
        foreach ($this->of($package) as $i => $patch) {
            if ($patch['description'] === $from) {
                $this->sets[$package][$i]['description'] = $to;
                return true;
            }
        }
        return false;
    }

    public function setPath(string $package, string $description, string $path): void
    {
        foreach ($this->of($package) as $i => $patch) {
            if ($patch['description'] === $description) {
                $this->sets[$package][$i]['path'] = $path;
            }
        }
    }

    /** Drops one patch, and the package with it once it has none left. */
    public function remove(string $package, string $description): void
    {
        $this->sets[$package] = array_values(array_filter($this->of($package), static fn (array $patch): bool => $patch['description'] !== $description));
        if ($this->sets[$package] === []) {
            $this->removePackage($package);
        }
    }

    public function removePackage(string $package): void
    {
        unset($this->sets[$package]);
    }

    /** As composer.json writes it: objects throughout, an empty one included. */
    public function toJson(): \stdClass
    {
        $patches = new \stdClass();
        foreach ($this->sets as $package => $set) {
            $object = new \stdClass();
            foreach ($set as $patch) {
                $object->{$patch['description']} = $patch['path'];
            }
            $patches->{$package} = $object;
        }
        return $patches;
    }
}

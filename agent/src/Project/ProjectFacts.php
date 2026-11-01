<?php

declare(strict_types=1);

namespace Drupdater\Agent\Project;

use Composer\Composer;
use Composer\Package\AliasPackage;
use Composer\Package\PackageInterface;
use Drupdater\Agent\Failure;

/**
 * Reads a project: composer.json as written, and the lock as Composer reads it.
 */
final class ProjectFacts
{
    private const CUSTOM_CODE_KINDS = ['modules', 'themes', 'profiles'];

    public function __construct(private readonly Composer $composer, private readonly string $composerFile)
    {
    }

    public function project(): Project
    {
        $manifest = $this->manifest();
        $webRoot = $manifest->extra->{'drupal-scaffold'}->locations->{'web-root'} ?? null;
        // A single trailing slash only, as drupal-scaffold itself joins paths.
        $webRoot = is_string($webRoot) && $webRoot !== '' ? (string) preg_replace('{/$}', '', $webRoot) : null;
        $customCode = $webRoot === null ? [] : $this->customCodeDirectories($webRoot);
        // As written, keys and nesting intact: composer_patches rewrites them.
        $patches = $manifest->extra->patches ?? null;

        $locker = $this->composer->getLocker();
        if (!$locker->isLocked()) {
            return new Project($webRoot, $customCode, patches: $patches);
        }

        $packages = [];
        $dependencyPatches = [];
        foreach ($locker->getLockedRepository(true)->getPackages() as $package) {
            if ($package instanceof AliasPackage) {
                continue;
            }
            $packages[$package->getName()] = $package->getPrettyVersion();
            foreach (self::declaredPatches($package) as $target => $files) {
                $dependencyPatches[$target] = [...($dependencyPatches[$target] ?? []), ...$files];
            }
        }
        ksort($packages);
        ksort($dependencyPatches);
        $hash = $locker->getLockData()['content-hash'] ?? null;
        return new Project(
            $webRoot,
            $customCode,
            $packages,
            $patches,
            array_map(static fn (array $files): array => array_values(array_unique($files)), $dependencyPatches),
            is_string($hash) ? $hash : null,
        );
    }

    private function manifest(): \stdClass
    {
        $contents = @file_get_contents($this->composerFile);
        if ($contents === false) {
            throw new Failure(Failure::INTERNAL, 'composer.json could not be read', $this->composerFile);
        }
        try {
            // Objects, not arrays: an empty {} must come back as {}, not [].
            $manifest = json_decode($contents, false, 512, JSON_THROW_ON_ERROR);
        }
        catch (\JsonException $e) {
            throw new Failure(Failure::INTERNAL, 'composer.json is not JSON', $e->getMessage(), $e);
        }
        if (!$manifest instanceof \stdClass) {
            throw new Failure(Failure::INTERNAL, 'composer.json is not a JSON object');
        }
        return $manifest;
    }

    /**
     * @return list<string>
     */
    private function customCodeDirectories(string $webRoot): array
    {
        $root = dirname($this->composerFile);
        $found = [];
        foreach (self::CUSTOM_CODE_KINDS as $kind) {
            $candidate = $webRoot . '/' . $kind . '/custom';
            if (file_exists($root . '/' . $candidate)) {
                $found[] = $candidate;
            }
        }
        return $found;
    }

    /**
     * A dependency's own extra.patches, which composer-patches 1.x applies on top of the root's.
     *
     * @return array<string, list<string>>
     */
    private static function declaredPatches(PackageInterface $package): array
    {
        $patches = $package->getExtra()['patches'] ?? null;
        if (!is_array($patches)) {
            return [];
        }
        $declared = [];
        foreach ($patches as $target => $byDescription) {
            if (!is_array($byDescription)) {
                continue;
            }
            $files = array_values(array_filter($byDescription, 'is_string'));
            if ($files !== []) {
                $declared[(string) $target] = $files;
            }
        }
        return $declared;
    }
}

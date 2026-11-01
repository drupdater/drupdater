<?php

declare(strict_types=1);

namespace Drupdater\Agent\Project;

use Composer\Composer;
use Composer\Package\AliasPackage;
use Composer\Package\PackageInterface;
use Drupdater\Agent\Failure;

/**
 * What drupdater reads about a project: composer.json as written, and the lock as Composer
 * reads it. A snapshot: drupdater asks again whenever the files may have changed.
 */
final class ProjectFacts
{
    private const CUSTOM_CODE_KINDS = ['modules', 'themes', 'profiles'];

    public function __construct(private readonly Composer $composer, private readonly string $composerFile)
    {
    }

    /**
     * @return array<string, mixed>
     */
    public function toArray(): array
    {
        $manifest = $this->manifest();
        $webRoot = $manifest->extra->{'drupal-scaffold'}->locations->{'web-root'} ?? null;
        // A single trailing slash only, as drupal-scaffold itself joins paths.
        $webRoot = is_string($webRoot) && $webRoot !== '' ? preg_replace('{/$}', '', $webRoot) : null;

        $facts = [
            'web_root' => $webRoot,
            'custom_code_directories' => $webRoot === null ? [] : $this->customCodeDirectories($webRoot),
            'lock_hash' => null,
            'packages' => new \stdClass(),
            'dependency_patches' => new \stdClass(),
        ];
        // As written, keys and nesting intact: drupdater rewrites them.
        if (isset($manifest->extra->patches)) {
            $facts['patches'] = $manifest->extra->patches;
        }

        $locker = $this->composer->getLocker();
        if (!$locker->isLocked()) {
            return $facts;
        }
        $facts['lock_hash'] = $locker->getLockData()['content-hash'] ?? null;

        $packages = [];
        $dependencyPatches = [];
        foreach ($locker->getLockedRepository(true)->getPackages() as $package) {
            if ($package instanceof AliasPackage) {
                continue;
            }
            $packages[$package->getName()] = ['version' => $package->getPrettyVersion()];
            foreach (self::declaredPatches($package) as $target => $files) {
                $dependencyPatches[$target] = [...($dependencyPatches[$target] ?? []), ...$files];
            }
        }
        ksort($packages);
        ksort($dependencyPatches);
        $facts['packages'] = (object) $packages;
        $facts['dependency_patches'] = (object) array_map(
            static fn (array $files): array => array_values(array_unique($files)),
            $dependencyPatches,
        );
        return $facts;
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

<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Composer\Util\Filesystem;

/**
 * Copies of tests/fixtures/project, the Drupal-shaped project most tests work on.
 */
trait FixtureProject
{
    /**
     * The fixture project, its config.allow-plugins replaced when given (null removes it).
     *
     * @param array{allow-plugins?: mixed} $config
     */
    private function fixtureProject(array $config = [], bool $withoutPlugins = false): string
    {
        $project = $this->scratchDir();
        (new Filesystem())->copy(__DIR__ . '/fixtures/project', $project);
        if (array_key_exists('allow-plugins', $config)) {
            $manifest = json_decode((string) file_get_contents($project . '/composer.json'), true);
            unset($manifest['config']['allow-plugins']);
            if ($config['allow-plugins'] !== null) {
                $manifest['config']['allow-plugins'] = $config['allow-plugins'];
            }
            $manifest['config'] = (object) $manifest['config'];
            file_put_contents($project . '/composer.json', json_encode($manifest, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES) . "\n");
        }
        if ($withoutPlugins) {
            $lock = json_decode((string) file_get_contents($project . '/composer.lock'), true);
            foreach (['packages', 'packages-dev'] as $key) {
                $lock[$key] = array_values(array_filter(
                    $lock[$key],
                    static fn (array $package): bool => !in_array($package['type'], ['composer-plugin', 'composer-installer'], true),
                ));
            }
            file_put_contents($project . '/composer.lock', json_encode($lock, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES));
        }
        return $project;
    }

    private function manifest(string $project): \stdClass
    {
        return json_decode((string) file_get_contents($project . '/composer.json'), false, 512, JSON_THROW_ON_ERROR);
    }
}

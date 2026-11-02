<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Composer\Util\Filesystem;
use Drupdater\Agent\Json;

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
            $manifest = Json::map(json_decode((string) file_get_contents($project . '/composer.json'), true, 512, JSON_THROW_ON_ERROR));
            $settings = Json::map($manifest['config'] ?? null);
            unset($settings['allow-plugins']);
            if ($config['allow-plugins'] !== null) {
                $settings['allow-plugins'] = $config['allow-plugins'];
            }
            $manifest['config'] = (object) $settings;
            file_put_contents($project . '/composer.json', json_encode($manifest, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR) . "\n");
        }
        if ($withoutPlugins) {
            $lock = Json::map(json_decode((string) file_get_contents($project . '/composer.lock'), true, 512, JSON_THROW_ON_ERROR));
            foreach (['packages', 'packages-dev'] as $key) {
                $lock[$key] = array_values(array_filter(
                    Json::maps($lock[$key] ?? null),
                    static fn (array $package): bool => !in_array($package['type'] ?? null, ['composer-plugin', 'composer-installer'], true),
                ));
            }
            file_put_contents($project . '/composer.lock', json_encode($lock, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR));
        }
        return $project;
    }

    /**
     * The project's composer.json, objects as objects.
     */
    private function manifest(string $project): \stdClass
    {
        $manifest = json_decode((string) file_get_contents($project . '/composer.json'), false, 512, JSON_THROW_ON_ERROR);
        self::assertInstanceOf(\stdClass::class, $manifest);
        return $manifest;
    }
}

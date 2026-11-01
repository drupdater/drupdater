<?php

declare(strict_types=1);

namespace Drupdater\Agent\Plugins;

use Composer\Composer;
use Composer\Config\JsonConfigSource;
use Composer\Json\JsonFile;
use Composer\Package\AliasPackage;
use Composer\Package\BasePackage;

/**
 * Lets an unattended update install plugins, then denies each new one the project's own rules
 * do not decide. Every write is the one `composer config allow-plugins.<name>` would make.
 */
final class AllowPlugins
{
    /** The package types Composer's PluginManager checks against allow-plugins. */
    public const PLUGIN_TYPES = ['composer-plugin', 'composer-installer'];

    private readonly JsonConfigSource $source;

    public function __construct(private readonly Composer $composer, private readonly string $composerFile)
    {
        $this->source = new JsonConfigSource(new JsonFile($composerFile));
    }

    /**
     * Appends a "*": true rule unless the setting already decides every plugin. Composer applies
     * the first matching rule, so the project's own rules, denials included, still win.
     *
     * @return array{original: \stdClass|bool|null, opened: bool}
     */
    public function open(): array
    {
        $original = $this->current();
        if (is_bool($original) || ($original !== null && property_exists($original, '*'))) {
            return ['original' => $original, 'opened' => false];
        }
        $this->source->addConfigSetting('allow-plugins.*', true);
        return ['original' => $original, 'opened' => true];
    }

    /**
     * Denies each locked plugin no rule of original decides, then removes the wildcard open
     * added, leaving no allow-plugins at all where the project had none and nothing was denied.
     *
     * @return list<string> the plugins denied, sorted
     */
    public function close(\stdClass|bool|null $original, bool $opened): array
    {
        $denied = [];
        if (!is_bool($original)) {
            $patterns = array_map(BasePackage::packageNameToRegexp(...), array_keys((array) $original));
            foreach ($this->lockedPlugins() as $plugin) {
                if (!self::matchesAny($patterns, $plugin)) {
                    $denied[] = $plugin;
                }
            }
        }
        sort($denied);

        foreach ($denied as $plugin) {
            $this->source->addConfigSetting('allow-plugins.' . $plugin, false);
        }
        if ($opened) {
            $this->source->removeConfigSetting('allow-plugins.*');
            if ($original === null && $denied === []) {
                $this->source->removeConfigSetting('allow-plugins');
                // open may have had to create config to hold the wildcard.
                if ((array) ($this->manifest()->config ?? null) === []) {
                    $this->source->removeProperty('config');
                }
            }
        }
        return $denied;
    }

    private function current(): \stdClass|bool|null
    {
        $value = $this->manifest()->config->{'allow-plugins'} ?? null;
        return $value instanceof \stdClass || is_bool($value) ? $value : null;
    }

    private function manifest(): \stdClass
    {
        return json_decode((string) file_get_contents($this->composerFile), false, 512, JSON_THROW_ON_ERROR);
    }

    /**
     * @return list<string>
     */
    private function lockedPlugins(): array
    {
        $locker = $this->composer->getLocker();
        if (!$locker->isLocked()) {
            return [];
        }
        $plugins = [];
        foreach ($locker->getLockedRepository(true)->getPackages() as $package) {
            if (!$package instanceof AliasPackage && in_array($package->getType(), self::PLUGIN_TYPES, true)) {
                $plugins[] = $package->getName();
            }
        }
        return $plugins;
    }

    /**
     * @param list<non-empty-string> $patterns
     */
    private static function matchesAny(array $patterns, string $plugin): bool
    {
        foreach ($patterns as $pattern) {
            if (preg_match($pattern, $plugin) === 1) {
                return true;
            }
        }
        return false;
    }
}

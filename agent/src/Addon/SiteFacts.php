<?php

declare(strict_types=1);

namespace Drupdater\Agent\Addon;

use Drupdater\Agent\Failure;

/**
 * What drupdater read from a site in one bootstrap, through drush's drupdater:facts. Paths are
 * absolute and may not exist yet.
 */
final class SiteFacts
{
    /**
     * @param list<string> $modules
     * @param array<string, array<string, mixed>> $updateHooks pending update hooks, by hook name
     * @param list<array<string, mixed>> $unsupportedModules
     * @param string $unsupportedModulesError why the update status service could not be asked
     */
    public function __construct(
        public readonly string $configSyncDirectory = '',
        public readonly string $translationPath = '',
        public readonly array $modules = [],
        public readonly array $updateHooks = [],
        public readonly array $unsupportedModules = [],
        public readonly string $unsupportedModulesError = '',
    ) {
    }

    /**
     * @throws Failure when the facts are malformed
     */
    public static function fromArray(mixed $facts): self
    {
        if (!is_array($facts)) {
            throw new Failure(Failure::INVALID_REQUEST, 'facts must be an object');
        }
        $modules = $facts['modules'] ?? [];
        $updateHooks = $facts['update_hooks'] ?? [];
        $unsupported = $facts['unsupported_modules'] ?? [];
        if (!is_array($modules) || array_filter($modules, 'is_string') !== $modules || !is_array($updateHooks)
            || !is_array($unsupported) || !array_is_list($unsupported)) {
            throw new Failure(Failure::INVALID_REQUEST, 'facts.modules, facts.update_hooks and facts.unsupported_modules are malformed');
        }
        return new self(
            self::string($facts, 'config_sync_directory'),
            self::string($facts, 'translation_path'),
            array_values($modules),
            $updateHooks,
            $unsupported,
            self::string($facts, 'unsupported_modules_error'),
        );
    }

    public function moduleEnabled(string $module): bool
    {
        return in_array($module, $this->modules, true);
    }

    /**
     * @param array<mixed> $facts
     */
    private static function string(array $facts, string $key): string
    {
        $value = $facts[$key] ?? '';
        if (!is_string($value)) {
            throw new Failure(Failure::INVALID_REQUEST, sprintf('facts.%s must be a string', $key));
        }
        return $value;
    }
}

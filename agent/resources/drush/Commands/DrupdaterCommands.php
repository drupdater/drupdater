<?php

declare(strict_types=1);

namespace Drush\Commands;

use Drupal\Core\Site\Settings;
use Drupal\update\UpdateManagerInterface;
use Drush\Attributes as CLI;
use Drush\Boot\DrupalBootLevels;

/**
 * Drupdater's queries and actions on a site, loaded with `drush --include`.
 */
final class DrupdaterCommands extends DrushCommands
{
    /**
     * Prints, as JSON, everything drupdater reads from a site.
     */
    // The update kernel, as updatedb:status uses: the code is already updated, the schema is not.
    #[CLI\Command(name: 'drupdater:facts')]
    #[CLI\Option(name: 'skip-update-status', description: 'Do not ask drupal.org which modules are unsupported.')]
    #[CLI\Bootstrap(level: DrupalBootLevels::FULL)]
    #[CLI\Kernel(name: 'update')]
    public function facts(array $options = ['skip-update-status' => false]): string
    {
        [$unsupported, $unsupportedError] = $options['skip-update-status'] ? [[], ''] : $this->unsupportedModules();
        return json_encode([
            'config_sync_directory' => $this->absolute((string) Settings::get('config_sync_directory')),
            'translation_path' => $this->absolute((string) \Drupal::config('locale.settings')->get('translation.path')),
            'modules' => array_keys(\Drupal::moduleHandler()->getModuleList()),
            'update_hooks' => (object) $this->updateHooks(),
            'unsupported_modules' => $unsupported,
            'unsupported_modules_error' => $unsupportedError,
        ], JSON_THROW_ON_ERROR);
    }

    /**
     * Saves every configuration object again, so the export carries the update's new defaults.
     */
    #[CLI\Command(name: 'drupdater:config-resave')]
    #[CLI\Bootstrap(level: DrupalBootLevels::FULL)]
    public function configResave(): void
    {
        $factory = \Drupal::configFactory();
        foreach ($factory->listAll() as $name) {
            $factory->getEditable($name)->save();
        }
    }

    // Not realpath(): the directory may not exist until config:export or the translation import.
    private function absolute(string $path): string
    {
        if ($path === '' || str_starts_with($path, '/')) {
            return $path;
        }
        return DRUPAL_ROOT . '/' . $path;
    }

    // The same rows and keys as `drush updatedb:status --format=json`.
    private function updateHooks(): array
    {
        require_once DRUPAL_ROOT . '/core/includes/install.inc';
        require_once DRUPAL_ROOT . '/core/includes/update.inc';
        drupal_load_updates();

        $hooks = [];
        foreach (update_get_update_list() as $module => $updates) {
            foreach ($updates['pending'] ?? [] as $id => $description) {
                $hooks[$module . '_update_' . $id] = [
                    'module' => $module,
                    'update_id' => $id,
                    'description' => str_replace($id . ' -   ', '', (string) $description),
                    'type' => 'hook_update_n',
                ];
            }
        }
        foreach (\Drupal::service('update.post_update_registry')->getPendingUpdateInformation() as $module => $updates) {
            foreach ($updates['pending'] ?? [] as $id => $description) {
                $hooks[$module . '-post-' . $id] = [
                    'module' => $module,
                    'update_id' => $id,
                    'description' => trim((string) $description),
                    'type' => 'post-update',
                ];
            }
        }
        return $hooks;
    }

    // Best-effort: an unreachable status service is reported, never fatal to the other facts.
    private function unsupportedModules(): array
    {
        try {
            $handler = \Drupal::moduleHandler();
            if (!$handler->moduleExists('update')) {
                return [[], ''];
            }
            $handler->loadInclude('update', 'inc', 'update.compare');
            $handler->loadInclude('update', 'inc', 'update.fetch');
            $data = update_calculate_project_data(update_get_available(TRUE));
        }
        catch (\Throwable $e) {
            return [[], $e->getMessage()];
        }

        $unsupported = [];
        foreach ($data as $name => $project) {
            // Not the procedural UPDATE_* constant: removed in Drupal 11, where referencing it is fatal.
            if (($project['status'] ?? NULL) !== UpdateManagerInterface::NOT_SUPPORTED) {
                continue;
            }
            $unsupported[] = [
                'name' => $name,
                'installed_version' => $project['existing_version'] ?? 'unknown',
                'recommended_version' => $project['recommended'] ?? 'None',
            ];
        }
        return [$unsupported, ''];
    }
}

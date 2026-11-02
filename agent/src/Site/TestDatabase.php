<?php

declare(strict_types=1);

namespace Drupdater\Agent\Site;

use Drupdater\Agent\Failure;
use Drupdater\Agent\Log;
use Drupdater\Agent\Tools\Drush;

/**
 * Points a site at a throwaway SQLite database under a data directory drupdater owns, by
 * appending a block to its settings.php, and takes the block off again.
 *
 * The block is decided once per site and kept in the data directory: by a later configure() the
 * sqlite module drupdater added is in core.extension.yml, and would read as the project's own.
 */
final class TestDatabase
{
    /** Opens the appended block, so restore() can cut it off, also one a killed run left behind. */
    public const MARKER = '// Added by drupdater: test database settings.';

    private const PROFILE = 'standard';

    public function __construct(
        private readonly Drush $drush,
        private readonly Log $log,
        private readonly string $projectDir,
        private readonly ?string $webRoot,
        private readonly string $dataDir,
    ) {
    }

    /**
     * Installs the site from its exported configuration, leaving settings.php as it found it:
     * only the database under the data directory remains.
     *
     * @throws Failure
     */
    public function install(string $site): void
    {
        $this->log->info('installing site', ['site' => $site]);
        $coreExtension = $this->coreExtensionPath($site);
        $settings = $this->configureAt($site, $coreExtension);
        try {
            CoreExtension::read($coreExtension)->withoutProfile(self::PROFILE)->write($coreExtension);
            $this->drush->installSite($site);
        }
        catch (Failure $failure) {
            $this->restoreAfter($failure, $site, $settings);
            throw $failure;
        }
        $this->restoreFile($site, $settings);
    }

    /**
     * Pair it with restore(): settings.php is tracked, and nothing may commit it with the block.
     *
     * @throws Failure
     */
    public function configure(string $site): void
    {
        $this->configureAt($site, $this->coreExtensionPath($site));
    }

    /**
     * Takes the block off, leaving settings.php byte for byte as the project has it. A file
     * without the block is left alone.
     *
     * @throws Failure
     */
    public function restore(string $site): void
    {
        $this->restoreFile($site, $this->settingsPath($site));
    }

    private function configureAt(string $site, string $coreExtension): string
    {
        $this->log->debug('configuring database', ['site' => $site]);
        $settings = $this->settingsPath($site);

        // Every time: core.extension.yml comes back from git without the entry whenever a run is
        // restored, and Drupal refuses to install on SQLite without it.
        $extension = CoreExtension::read($coreExtension);
        $projectUsesSqlite = $extension->sqliteEnabled();
        if (!$projectUsesSqlite) {
            $this->log->debug('enabling sqlite module', ['path' => $coreExtension]);
            $extension->withSqlite()->write($coreExtension);
        }

        $block = $this->block($site, $projectUsesSqlite);
        // Never created: the block alone is not a valid settings.php.
        $content = self::read($settings);
        self::write($settings, self::strip($content) . $block);
        return $settings;
    }

    private function block(string $site, bool $projectUsesSqlite): string
    {
        $kept = $this->dataDir . '/' . $site . '.settings.php';
        $block = is_file($kept) ? @file_get_contents($kept) : false;
        if (is_string($block)) {
            return $block;
        }

        $database = self::quote($this->dataDir . '/' . $site . '.sqlite');
        $private = self::quote($this->dataDir . '/private/' . $site);
        $block = "\n" . self::MARKER . "\n" . <<<PHP
            \$databases['default']['default'] = [
            	'database' => {$database},
            	'prefix' => '',
            	'driver' => 'sqlite',
            	'namespace' => 'Drupal\\\\sqlite\\\\Driver\\\\Database\\\\sqlite',
            	'autoload' => 'core/modules/sqlite/src/Driver/Database/sqlite/',
            ];
            \$settings['skip_permissions_hardening'] = TRUE;
            \$settings['file_private_path'] = {$private};
            \$settings['hash_salt'] = 'changeme';

            PHP;
        // Only when the project does not use sqlite itself, or the export would drop a real module.
        if (!$projectUsesSqlite) {
            $block .= <<<'PHP'

                if (isset($settings['config_exclude_modules'])) {
                	$settings['config_exclude_modules'][] = 'sqlite';
                } else {
                	$settings['config_exclude_modules'] = ['sqlite'];
                }

                PHP;
        }
        self::write($kept, $block);
        return $block;
    }

    private function restoreAfter(Failure $failure, string $site, string $settings): void
    {
        try {
            $this->restoreFile($site, $settings);
        }
        catch (Failure $restore) {
            throw new Failure($failure->kind, $failure->reason, trim($failure->getMessage() . '; then ' . $restore->reason), $failure);
        }
    }

    private function restoreFile(string $site, string $settings): void
    {
        $content = self::read($settings);
        $stripped = self::strip($content);
        if ($stripped === $content) {
            return;
        }
        $this->log->debug('restoring settings', ['site' => $site, 'path' => $settings]);
        self::write($settings, $stripped);
    }

    private function settingsPath(string $site): string
    {
        if ($this->webRoot === null) {
            throw new Failure(Failure::FAILED, 'failed to get Drupal web dir: extra.drupal-scaffold.locations.web-root is not defined');
        }
        return $this->projectDir . '/' . $this->webRoot . '/sites/' . $site . '/settings.php';
    }

    private function coreExtensionPath(string $site): string
    {
        return $this->drush->configSyncDir($site) . '/core.extension.yml';
    }

    /** Cut at the marker, together with the line break the block starts with. */
    private static function strip(string $content): string
    {
        $at = strpos($content, self::MARKER);
        if ($at === false) {
            return $content;
        }
        if ($at > 0 && $content[$at - 1] === "\n") {
            $at--;
        }
        return substr($content, 0, $at);
    }

    private static function quote(string $value): string
    {
        return "'" . addcslashes($value, "'\\") . "'";
    }

    private static function read(string $path): string
    {
        $content = is_file($path) ? @file_get_contents($path) : false;
        if ($content === false) {
            throw new Failure(Failure::FAILED, sprintf('failed to read settings file %s', $path));
        }
        return $content;
    }

    // Truncated in place, not replaced, so the file keeps its mode and owner.
    private static function write(string $path, string $content): void
    {
        if (@file_put_contents($path, $content) === false) {
            throw new Failure(Failure::FAILED, sprintf('failed to write %s', $path));
        }
    }
}

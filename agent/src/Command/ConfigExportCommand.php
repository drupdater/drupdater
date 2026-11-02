<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

use Drupdater\Agent\Addon\Paths;

/**
 * Exports a site's configuration into its config sync directory.
 *
 * Request: {site}. Result: {path}, the directory relative to the project, for drupdater to commit.
 */
final class ConfigExportCommand extends SiteCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:config-export')
            ->setDescription('Exports a site\'s configuration and names the directory it wrote.')
            ->setHidden(true);
    }

    protected function handleSite(string $site, array $request): array
    {
        $drush = $this->drush();
        $this->log->info('exporting configuration', ['site' => $site]);
        $drush->exportConfig($site);
        return ['path' => Paths::relativeTo($this->projectDir(), $drush->configSyncDir($site))];
    }
}

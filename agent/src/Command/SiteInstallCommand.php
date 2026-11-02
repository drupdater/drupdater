<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

/**
 * Installs the site from its exported configuration, as the baseline the update runs against;
 * settings.php is left as it was found. See TestDatabase.
 *
 * Request: {site, data_dir}: data_dir is drupdater's, holding the databases. Result: {}.
 */
final class SiteInstallCommand extends SiteCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:site-install')
            ->setDescription('Installs a site from its exported configuration on a throwaway SQLite database.')
            ->setHidden(true);
    }

    protected function handleSite(string $site, array $request): array
    {
        $this->testDatabase($request)->install($site);
        return [];
    }
}

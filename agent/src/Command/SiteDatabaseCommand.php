<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

/**
 * Points the site at its throwaway SQLite database until drupdater:site-restore. See TestDatabase.
 *
 * Request: {site, data_dir}: data_dir is drupdater's, holding the databases. Result: {}.
 */
final class SiteDatabaseCommand extends SiteCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:site-database')
            ->setDescription('Points a site at its throwaway SQLite database.')
            ->setHidden(true);
    }

    protected function handleSite(string $site, array $request): array
    {
        $this->testDatabase($request)->configure($site);
        return [];
    }
}

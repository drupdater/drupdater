<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

/**
 * Takes the block drupdater:site-database appended off settings.php again. See TestDatabase.
 *
 * Request: {site, data_dir}: data_dir is drupdater's, holding the databases. Result: {}.
 */
final class SiteRestoreCommand extends SiteCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:site-restore')
            ->setDescription('Takes drupdater\'s database settings off a site\'s settings.php.')
            ->setHidden(true);
    }

    protected function handleSite(string $site, array $request): array
    {
        $this->testDatabase($request)->restore($site);
        return [];
    }
}

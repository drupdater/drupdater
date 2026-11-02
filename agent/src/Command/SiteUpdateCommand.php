<?php

declare(strict_types=1);

namespace Drupdater\Agent\Command;

/**
 * Runs a site's update hooks, saves its configuration again so the export carries the update's
 * new defaults, and reads its facts as they stand afterwards: an update hook may enable a module
 * or move a path the facts named before.
 *
 * Request: {site}. Result: {facts}, without asking drupal.org about unsupported modules.
 */
final class SiteUpdateCommand extends SiteCommand
{
    protected function configure(): void
    {
        $this->setName('drupdater:site-update')
            ->setDescription('Runs a site\'s update hooks and reads its facts afterwards.')
            ->setHidden(true);
    }

    protected function handleSite(string $site, array $request): array
    {
        $drush = $this->drush();
        $this->log->info('updating site', ['site' => $site]);
        $drush->updateDatabase($site);
        $drush->resaveConfig($site);
        return ['facts' => $drush->facts($site, false)];
    }
}
